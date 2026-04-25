package widget

// This file provides a label layout function with a larger glyph buffer than
// Gio's default (32).  Gio renders each buffer-full of glyphs as a separate
// GPU stencil path; when a monospace font line exceeds 32 characters the two
// adjacent paths meet at the same X pixel on every row, producing a visible
// vertical stripe of lighter anti-aliasing on Linux GPU drivers.
//
// Increasing the buffer to 256 glyphs keeps typical UI text in a single
// batch, eliminating the seam.
//
// Mirrors gioui.org/widget.Label.LayoutDetailed – keep in sync with Gio v0.9.0.

import (
	"image"
	"image/color"
	"math"
	"strings"
	"unicode/utf8"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"
	"golang.org/x/image/math/fixed"
)

// glyphBatchSize is the number of glyphs buffered before flushing to the GPU.
// Gio defaults to 32; we use 256 to avoid mid-line batch seams.
const glyphBatchSize = 256

// SearchHighlightColor is the yellow wash painted behind a matching substring
// inside a label. Saturated and near-opaque so a single character stays
// legible — the viewer's full-line highlight intentionally uses a softer
// wash because it covers much more area.
var SearchHighlightColor = color.NRGBA{R: 255, G: 215, B: 0, A: 200} //nolint:mnd // saturated amber highlight

// LayoutEditor lays out a material.EditorStyle, rendering the hint text with
// the larger glyph batch so it doesn't exhibit the vertical-stripe artefact.
// The shaper must be the same *text.Shaper used by the theme (th.Shaper) since
// EditorStyle.shaper is unexported.
func LayoutEditor(gtx layout.Context, lt *text.Shaper, e material.EditorStyle) layout.Dimensions {
	textColorMacro := op.Record(gtx.Ops)
	paint.ColorOp{Color: e.Color}.Add(gtx.Ops)

	textColor := textColorMacro.Stop()

	hintColorMacro := op.Record(gtx.Ops)
	paint.ColorOp{Color: e.HintColor}.Add(gtx.Ops)

	hintColor := hintColorMacro.Stop()

	selectionColorMacro := op.Record(gtx.Ops)
	paint.ColorOp{Color: e.SelectionColor}.Add(gtx.Ops)

	selectionColor := selectionColorMacro.Stop()

	var maxlines int
	if e.Editor.SingleLine {
		maxlines = 1
	}

	// Render the hint with our larger glyph batch.
	macro := op.Record(gtx.Ops)
	dims := layoutLabel(gtx, lt, e.Font, e.TextSize, e.Hint, hintColor, labelParams{
		alignment:       e.Editor.Alignment,
		maxLines:        maxlines,
		lineHeight:      e.LineHeight,
		lineHeightScale: e.LineHeightScale,
	})
	call := macro.Stop()

	if w := dims.Size.X; gtx.Constraints.Min.X < w {
		gtx.Constraints.Min.X = w
	}

	if h := dims.Size.Y; gtx.Constraints.Min.Y < h {
		gtx.Constraints.Min.Y = h
	}

	e.Editor.LineHeight = e.LineHeight
	e.Editor.LineHeightScale = e.LineHeightScale
	dims = e.Editor.Layout(gtx, lt, e.Font, e.TextSize, textColor, selectionColor)

	if e.Editor.Len() == 0 {
		call.Add(gtx.Ops)
	}

	return dims
}

// LabelWidget returns a layout.Widget that calls LayoutLabel.
// Use where layout.Rigid previously accepted a direct method value (e.g. lbl.Layout).
func LabelWidget(l material.LabelStyle) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return LayoutLabel(gtx, l)
	}
}

// LayoutHighlightedLabel renders lbl with a yellow rectangle painted behind
// the first case-insensitive occurrence of query inside lbl.Text. When query
// is empty or does not match, it is equivalent to LayoutLabel.
//
// Forces lbl.MaxLines to 1 so the highlight rect — painted as a single
// (x0, x1, fullHeight) rectangle — can never smear across wrapped lines.
// Callers that need multi-line rendering should not use this widget.
func LayoutHighlightedLabel(gtx layout.Context, lbl material.LabelStyle, query string) layout.Dimensions {
	lbl.MaxLines = 1

	if query == "" {
		return LayoutLabel(gtx, lbl)
	}

	matchStart := strings.Index(strings.ToLower(lbl.Text), strings.ToLower(query))
	if matchStart < 0 {
		return LayoutLabel(gtx, lbl)
	}

	matchEnd := matchStart + len(query)

	x0, x1 := measureMatchRange(gtx, lbl, matchStart, matchEnd)

	m := op.Record(gtx.Ops)
	dims := LayoutLabel(gtx, lbl)
	call := m.Stop()

	if x1 > x0 {
		rect := clip.Rect{Min: image.Pt(x0, 0), Max: image.Pt(x1, dims.Size.Y)}.Push(gtx.Ops)
		paint.ColorOp{Color: SearchHighlightColor}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		rect.Pop()
	}

	call.Add(gtx.Ops)

	return dims
}

// measureMatchRange returns the pixel x-range of bytes [byteStart, byteEnd)
// inside lbl.Text by shaping the label's text with the same parameters
// LayoutLabel uses, then walking glyphs once. Position within the shaped text
// is tracked as a rune count — Gio v0.9.0's text.Glyph exposes per-cluster
// rune counts via Runes on FlagClusterBreak rather than a direct byte offset,
// so we convert the byte-granular match range to rune indices first.
func measureMatchRange(gtx layout.Context, lbl material.LabelStyle, byteStart, byteEnd int) (int, int) {
	cs := gtx.Constraints
	textSize := fixed.I(gtx.Sp(lbl.TextSize))
	lineHeight := fixed.I(gtx.Sp(lbl.LineHeight))

	maxLines := lbl.MaxLines
	if maxLines == 0 {
		maxLines = 1
	}

	lbl.Shaper.LayoutString(text.Parameters{
		Font:            lbl.Font,
		PxPerEm:         textSize,
		MaxLines:        maxLines,
		Truncator:       lbl.Truncator,
		Alignment:       lbl.Alignment,
		WrapPolicy:      lbl.WrapPolicy,
		MaxWidth:        cs.Max.X,
		MinWidth:        cs.Min.X,
		Locale:          gtx.Locale,
		LineHeight:      lineHeight,
		LineHeightScale: lbl.LineHeightScale,
	}, lbl.Text)

	startRune := utf8.RuneCountInString(lbl.Text[:byteStart])
	endRune := startRune + utf8.RuneCountInString(lbl.Text[byteStart:byteEnd])

	var (
		runesBefore int
		x0, x1      int
		haveX0      bool
	)

	for g, ok := lbl.Shaper.NextGlyph(); ok; g, ok = lbl.Shaper.NextGlyph() {
		if !haveX0 && runesBefore >= startRune {
			x0 = g.X.Floor()
			haveX0 = true
		}

		if runesBefore >= startRune && runesBefore < endRune {
			if right := (g.X + g.Advance).Ceil(); right > x1 {
				x1 = right
			}
		}

		if g.Flags&text.FlagClusterBreak != 0 {
			runesBefore += int(g.Runes)
		}
	}

	if !haveX0 {
		return 0, 0
	}

	return x0, x1
}

// LayoutLabel lays out a material.LabelStyle using a larger glyph batch than
// Gio's built-in label renderer, preventing the vertical-stripe artefact that
// appears with monospace fonts on Linux.
func LayoutLabel(gtx layout.Context, l material.LabelStyle) layout.Dimensions {
	textColorMacro := op.Record(gtx.Ops)
	paint.ColorOp{Color: l.Color}.Add(gtx.Ops)

	textColor := textColorMacro.Stop()

	if l.State != nil {
		if l.State.Text() != l.Text {
			l.State.SetText(l.Text)
		}

		l.State.Alignment = l.Alignment
		l.State.MaxLines = l.MaxLines
		l.State.Truncator = l.Truncator
		l.State.WrapPolicy = l.WrapPolicy
		l.State.LineHeight = l.LineHeight
		l.State.LineHeightScale = l.LineHeightScale

		selectColorMacro := op.Record(gtx.Ops)
		paint.ColorOp{Color: l.SelectionColor}.Add(gtx.Ops)

		selectColor := selectColorMacro.Stop()

		return l.State.Layout(gtx, l.Shaper, l.Font, l.TextSize, textColor, selectColor)
	}

	return layoutLabel(gtx, l.Shaper, l.Font, l.TextSize, l.Text, textColor, labelParams{
		alignment:       l.Alignment,
		maxLines:        l.MaxLines,
		truncator:       l.Truncator,
		wrapPolicy:      l.WrapPolicy,
		lineHeight:      l.LineHeight,
		lineHeightScale: l.LineHeightScale,
	})
}

type labelParams struct {
	alignment       text.Alignment
	maxLines        int
	truncator       string
	wrapPolicy      text.WrapPolicy
	lineHeight      unit.Sp
	lineHeightScale float32
}

func layoutLabel(
	gtx layout.Context,
	lt *text.Shaper,
	f font.Font,
	size unit.Sp,
	txt string,
	textMaterial op.CallOp,
	p labelParams,
) layout.Dimensions {
	cs := gtx.Constraints
	textSize := fixed.I(gtx.Sp(size))
	lineHeight := fixed.I(gtx.Sp(p.lineHeight))

	lt.LayoutString(text.Parameters{
		Font:            f,
		PxPerEm:         textSize,
		MaxLines:        p.maxLines,
		Truncator:       p.truncator,
		Alignment:       p.alignment,
		WrapPolicy:      p.wrapPolicy,
		MaxWidth:        cs.Max.X,
		MinWidth:        cs.Min.X,
		Locale:          gtx.Locale,
		LineHeight:      lineHeight,
		LineHeightScale: p.lineHeightScale,
	}, txt)

	m := op.Record(gtx.Ops)
	viewport := image.Rectangle{Max: cs.Max}

	it := labelIterator{
		viewport: viewport,
		maxLines: p.maxLines,
		material: textMaterial,
	}

	semantic.LabelOp(txt).Add(gtx.Ops)

	var glyphs [glyphBatchSize]text.Glyph

	line := glyphs[:0]

	for g, ok := lt.NextGlyph(); ok; g, ok = lt.NextGlyph() {
		var ok bool
		if line, ok = it.paintGlyph(gtx, lt, g, line); !ok {
			break
		}
	}

	call := m.Stop()
	viewport.Min = viewport.Min.Add(it.padding.Min)
	viewport.Max = viewport.Max.Add(it.padding.Max)
	clipStack := clip.Rect(viewport).Push(gtx.Ops)
	call.Add(gtx.Ops)

	dims := layout.Dimensions{Size: it.bounds.Size()}
	dims.Size = cs.Constrain(dims.Size)
	dims.Baseline = dims.Size.Y - it.baseline

	clipStack.Pop()

	return dims
}

// labelIterator mirrors gioui.org/widget.textIterator.
type labelIterator struct {
	viewport  image.Rectangle
	maxLines  int
	material  op.CallOp
	truncated int
	linesSeen int
	lineOff   f32.Point
	padding   image.Rectangle
	bounds    image.Rectangle
	visible   bool
	first     bool
	baseline  int
}

func (it *labelIterator) processGlyph(g text.Glyph, ok bool) (visibleOrBefore bool) {
	if it.maxLines > 0 {
		if g.Flags&text.FlagTruncator != 0 && g.Flags&text.FlagClusterBreak != 0 {
			it.truncated = int(g.Runes)
		}

		if g.Flags&text.FlagLineBreak != 0 {
			it.linesSeen++
		}

		if it.linesSeen == it.maxLines && g.Flags&text.FlagParagraphBreak != 0 {
			return false
		}
	}

	if d := g.Bounds.Min.X.Floor(); d < it.padding.Min.X {
		it.padding.Min.X = d
	}

	if d := (g.Bounds.Max.X - g.Advance).Ceil(); d > it.padding.Max.X {
		it.padding.Max.X = d
	}

	if d := (g.Bounds.Min.Y + g.Ascent).Floor(); d < it.padding.Min.Y {
		it.padding.Min.Y = d
	}

	if d := (g.Bounds.Max.Y - g.Descent).Ceil(); d > it.padding.Max.Y {
		it.padding.Max.Y = d
	}

	logicalBounds := image.Rectangle{
		Min: image.Pt(g.X.Floor(), int(g.Y)-g.Ascent.Ceil()),
		Max: image.Pt((g.X + g.Advance).Ceil(), int(g.Y)+g.Descent.Ceil()),
	}

	if !it.first {
		it.first = true
		it.baseline = int(g.Y)
		it.bounds = logicalBounds
	}

	above := logicalBounds.Max.Y < it.viewport.Min.Y
	below := logicalBounds.Min.Y > it.viewport.Max.Y
	left := logicalBounds.Max.X < it.viewport.Min.X
	right := logicalBounds.Min.X > it.viewport.Max.X
	it.visible = !above && !below && !left && !right

	if it.visible {
		it.bounds.Min.X = min(it.bounds.Min.X, logicalBounds.Min.X)
		it.bounds.Min.Y = min(it.bounds.Min.Y, logicalBounds.Min.Y)
		it.bounds.Max.X = max(it.bounds.Max.X, logicalBounds.Max.X)
		it.bounds.Max.Y = max(it.bounds.Max.Y, logicalBounds.Max.Y)
	}

	return ok && !below
}

func labelFixedToFloat(i fixed.Int26_6) float32 {
	return float32(i) / 64.0 //nolint:mnd // fixed-point 26.6 conversion factor
}

func (it *labelIterator) paintGlyph(
	gtx layout.Context,
	shaper *text.Shaper,
	glyph text.Glyph,
	line []text.Glyph,
) ([]text.Glyph, bool) {
	visibleOrBefore := it.processGlyph(glyph, true)

	if it.visible {
		if len(line) == 0 {
			it.lineOff = f32.Point{
				X: labelFixedToFloat(glyph.X),
				Y: float32(glyph.Y),
			}.Sub(layout.FPt(it.viewport.Min))
			// Snap to integer pixels so glyph outlines align with the pixel
			// grid.  Sub-pixel offsets (common with right/center alignment)
			// cause grey anti-aliasing artefacts on Linux GPU drivers.
			it.lineOff.X = float32(math.Round(float64(it.lineOff.X)))
		}

		line = append(line, glyph)
	}

	if glyph.Flags&text.FlagLineBreak != 0 || cap(line)-len(line) == 0 || !visibleOrBefore {
		t := op.Affine(f32.AffineId().Offset(it.lineOff)).Push(gtx.Ops)
		path := shaper.Shape(line)
		outline := clip.Outline{Path: path}.Op().Push(gtx.Ops)
		it.material.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		outline.Pop()

		if call := shaper.Bitmaps(line); call != (op.CallOp{}) {
			call.Add(gtx.Ops)
		}

		t.Pop()

		line = line[:0]
	}

	return line, visibleOrBefore
}
