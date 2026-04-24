package page

import (
	"image"
	"image/color"
	"math"
	"path/filepath"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/image/math/fixed"

	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

const (
	editorFieldPadH        unit.Dp = 8
	editorFieldPadV        unit.Dp = 6
	editorFieldRadius      unit.Dp = 4
	editorFieldBorderWidth unit.Dp = 1

	presetChipRadius unit.Dp = 12
)

const ellipsisPrefix = "\u2026/"

func layoutEditorField(gtx layout.Context, th *material.Theme, editor *widget.Editor, hint string, bottomPad unit.Dp) layout.Dimensions {
	return layout.Inset{Bottom: bottomPad}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		ed := material.Editor(th, editor, hint)
		ed.Editor.SingleLine = true

		// Fill available width so all fields are the same length.
		gtx.Constraints.Min.X = gtx.Constraints.Max.X

		// Record editor layout to measure size for border.
		m := op.Record(gtx.Ops)
		dims := layout.Inset{
			Left: editorFieldPadH, Right: editorFieldPadH,
			Top: editorFieldPadV, Bottom: editorFieldPadV,
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return customwidget.LayoutEditor(gtx, th.Shaper, ed)
		})
		c := m.Stop()

		bounds := image.Rectangle{Max: dims.Size}
		radius := gtx.Dp(editorFieldRadius)
		bw := gtx.Dp(editorFieldBorderWidth)

		paintRoundedBorder(gtx, bounds, radius, bw, theme.ColorInputBorder, theme.ColorDropdownBg)

		c.Add(gtx.Ops)

		return dims
	})
}

// pushPointerCursor registers a pointer-hand cursor over the given area using
// a PassOp so it takes precedence over parent scroll regions.
func pushPointerCursor(gtx layout.Context, dims layout.Dimensions, tag event.Tag) {
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops)

	event.Op(gtx.Ops, tag)
	pointer.CursorPointer.Add(gtx.Ops)

	area.Pop()
	pass.Pop()
}

// paintHoverBg paints a rounded hover background behind a widget when hovered.
func paintHoverBg(gtx layout.Context, dims layout.Dimensions, hovered bool) {
	if !hovered {
		return
	}

	bounds := image.Rectangle{Max: dims.Size}
	radius := gtx.Dp(textBtnCornerRadius)
	bg := clip.UniformRRect(bounds, radius).Push(gtx.Ops)

	paint.ColorOp{Color: theme.ColorHover}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	bg.Pop()
}

func layoutPanelLabel(gtx layout.Context, th *material.Theme, text string, top, bottom unit.Dp) layout.Dimensions {
	return layout.Inset{Top: top, Bottom: bottom}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return customwidget.LayoutLabel(gtx, material.Body2(th, text))
	})
}

// layoutHelpHint renders a single-line muted Body2 caption used by per-page
// LayoutShortcutsHelp methods to fill the notification bar's idle slot.
func layoutHelpHint(gtx layout.Context, th *material.Theme, text string) layout.Dimensions {
	lbl := material.Body2(th, text)
	lbl.Color = theme.ColorSecondary
	lbl.MaxLines = 1

	return customwidget.LayoutLabel(gtx, lbl)
}

// layoutCappedHeight constrains Max.Y to the given height and lays out the widget.
func layoutCappedHeight(gtx layout.Context, maxHeight unit.Dp, w layout.Widget) layout.Dimensions {
	maxH := gtx.Dp(maxHeight)
	if gtx.Constraints.Max.Y > maxH {
		gtx.Constraints.Max.Y = maxH
	}

	return w(gtx)
}

// paintRoundedBorder draws a rounded border by painting two concentric rounded rects:
// the outer one in the border color, the inner one in the fill color.
func paintRoundedBorder(gtx layout.Context, bounds image.Rectangle, radius, bw int, border, fill color.NRGBA) {
	// Outer (border color).
	outer := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
	paint.ColorOp{Color: border}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	outer.Pop()

	// Inner (fill color), inset by border width.
	inner := bounds
	inner.Min = inner.Min.Add(image.Pt(bw, bw))
	inner.Max = inner.Max.Sub(image.Pt(bw, bw))

	innerRadius := max(radius-bw, 0)

	innerClip := clip.UniformRRect(inner, innerRadius).Push(gtx.Ops)
	paint.ColorOp{Color: fill}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	innerClip.Pop()
}

// paintEdgeBorder draws a border by painting four edge rectangles around the bounds in the given color.
func paintEdgeBorder(gtx layout.Context, bounds image.Rectangle, bw int, c color.NRGBA) {
	for _, edge := range customwidget.EdgeBorders(bounds, bw) {
		r := clip.Rect(edge).Push(gtx.Ops)
		paint.ColorOp{Color: c}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		r.Pop()
	}
}

// paintFocusBorder draws a focus border by painting four edge rectangles around the bounds.
func paintFocusBorder(gtx layout.Context, bounds image.Rectangle, bw int) {
	paintEdgeBorder(gtx, bounds, bw, theme.ColorAccent)
}

// truncatePathLeft truncates a file path from the left so it fits within maxWidthPx,
// prepending "…/" when segments are removed. If the path fits, it is returned as-is.
func truncatePathLeft(lbl *material.LabelStyle, gtx layout.Context, maxWidthPx int, path string) string {
	params := text.Parameters{
		Font:     lbl.Font,
		PxPerEm:  fixed.I(gtx.Sp(lbl.TextSize)),
		MaxWidth: math.MaxInt,
	}

	if measureTextWidth(lbl.Shaper, params, path) <= maxWidthPx {
		return path
	}

	budget := maxWidthPx - measureTextWidth(lbl.Shaper, params, ellipsisPrefix)
	remaining := path

	for {
		idx := strings.IndexByte(remaining, filepath.Separator)
		if idx < 0 {
			break
		}

		remaining = remaining[idx+1:]

		if measureTextWidth(lbl.Shaper, params, remaining) <= budget {
			return ellipsisPrefix + remaining
		}
	}

	// Even just the filename doesn't fit with ellipsis; return it anyway (label MaxLines will clip).
	return ellipsisPrefix + remaining
}

// measureTextWidth returns the pixel width of the given text when shaped with the provided font parameters.
func measureTextWidth(shaper *text.Shaper, params text.Parameters, str string) int {
	shaper.LayoutString(params, str)

	var width fixed.Int26_6

	for g, ok := shaper.NextGlyph(); ok; g, ok = shaper.NextGlyph() {
		width += g.Advance
	}

	return width.Ceil()
}
