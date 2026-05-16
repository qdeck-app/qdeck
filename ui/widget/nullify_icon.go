package widget

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
)

const (
	// nullifyBtnPaddingH is the horizontal padding inside the button
	// chrome — leaves enough room around the "~" glyph that it doesn't
	// touch the rounded border.
	nullifyBtnPaddingH unit.Dp = 3

	// nullifyBtnPaddingV stays at zero so the button's outer height is
	// driven by the glyph's own line height, matching the surrounding
	// editor cells and not expanding the row.
	nullifyBtnPaddingV unit.Dp = 0

	// nullifyBtnRadius is the corner radius. Smaller than the null pill
	// (which is fully rounded) so the button reads as actionable rather
	// than as a status badge.
	nullifyBtnRadius unit.Dp = 3

	// nullifyBtnGlyphSize matches the editor-cell text size
	// (viewerEditorTextSize = 14sp), so the button height equals the
	// cell's natural line box and the row doesn't grow when the button
	// appears on hover.
	nullifyBtnGlyphSize unit.Sp = 14
)

// layoutNullifyButton renders the inline "~" nullify button.
func layoutNullifyButton(
	gtx layout.Context,
	th *material.Theme,
	click *gesture.Click,
	hover *gesture.Hover,
	hovered, active bool,
) layout.Dimensions {
	cs := pickNullifyButtonColors(active, hovered)

	padH := gtx.Dp(nullifyBtnPaddingH)
	padV := gtx.Dp(nullifyBtnPaddingV)
	radius := gtx.Dp(nullifyBtnRadius)

	hairline := gtx.Dp(theme.Default.HairlineWidth)
	if hairline < 1 {
		hairline = 1
	}

	// Render the "~" label into a recorded macro first so we know the
	// button's natural size before we paint the chrome.
	measureGtx := gtx
	measureGtx.Constraints.Min = image.Point{}

	contentRec := op.Record(gtx.Ops)
	contentDims := nullifyButtonGlyph(measureGtx, th, cs.text)
	contentCall := contentRec.Stop()

	w := contentDims.Size.X + 2*padH //nolint:mnd // 2 sides of horizontal padding.
	h := contentDims.Size.Y + 2*padV //nolint:mnd // 2 sides of vertical padding.

	// Background fill.
	bgStack := clip.UniformRRect(image.Rectangle{Max: image.Pt(w, h)}, radius).Push(gtx.Ops)
	paint.ColorOp{Color: cs.fill}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	bgStack.Pop()

	paintHairlineBorder(gtx, w, h, hairline, cs.border)

	// Place the recorded label inside the padded interior.
	tr := op.Offset(image.Pt(padH, padV)).Push(gtx.Ops)
	contentCall.Add(gtx.Ops)
	tr.Pop()

	// Hit area: hover (cursor + color), click (action), CursorPointer
	// last so it wins over the row's CursorText on the same point.
	area := clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops)
	hover.Add(gtx.Ops)
	click.Add(gtx.Ops)
	pointer.CursorPointer.Add(gtx.Ops)
	area.Pop()

	return layout.Dimensions{Size: image.Pt(w, h)}
}

// nullifyButtonGlyph renders the "~" glyph at the configured glyph size.
func nullifyButtonGlyph(gtx layout.Context, th *material.Theme, c color.NRGBA) layout.Dimensions {
	lbl := material.Body2(th, "~")
	lbl.Color = c
	lbl.Font.Weight = font.Medium
	lbl.MaxLines = 1
	lbl.Alignment = text.Middle
	lbl.TextSize = nullifyBtnGlyphSize

	return LayoutLabel(gtx, lbl)
}

// layoutNullifyButtonPlaceholder returns dimensions matching what the
// real nullify button would occupy, without painting any chrome or
// registering the hover / click gestures. Reserving this footprint
// keeps the surrounding editor text and anchor badge stable when the
// button toggles between hidden and visible on row hover.
func layoutNullifyButtonPlaceholder(gtx layout.Context, th *material.Theme) layout.Dimensions {
	padH := gtx.Dp(nullifyBtnPaddingH)
	padV := gtx.Dp(nullifyBtnPaddingV)

	// Measure the glyph's natural size by recording it into a macro
	// and discarding the recording — the simplest way to match the
	// real button's pixel dimensions without duplicating shaper work.
	measureGtx := gtx
	measureGtx.Constraints.Min = image.Point{}

	rec := op.Record(gtx.Ops)
	contentDims := nullifyButtonGlyph(measureGtx, th, theme.Default.Transparent)
	_ = rec.Stop()

	w := contentDims.Size.X + 2*padH //nolint:mnd // 2 sides of horizontal padding.
	h := contentDims.Size.Y + 2*padV //nolint:mnd // 2 sides of vertical padding.

	return layout.Dimensions{Size: image.Pt(w, h)}
}

// nullifyButtonColors bundles the three colors the button frame uses so
// the (active, hovered) state matrix collapses to a single switch.
type nullifyButtonColors struct {
	fill   color.NRGBA
	border color.NRGBA
	text   color.NRGBA
}

func pickNullifyButtonColors(active, hovered bool) nullifyButtonColors {
	switch {
	case active && hovered:
		// Engaged + hovered: stronger border to invite the un-nullify
		// click without changing the bg (which already reads as "this
		// cell is in null state").
		return nullifyButtonColors{
			fill:   theme.Default.OverrideBg,
			border: theme.Default.Override,
			text:   theme.Default.Override,
		}
	case active:
		return nullifyButtonColors{
			fill:   theme.Default.OverrideBg,
			border: theme.Default.Override,
			text:   theme.Default.Override,
		}
	case hovered:
		return nullifyButtonColors{
			fill:   theme.Default.Bg2,
			border: theme.Default.BorderStrong,
			text:   theme.Default.Override,
		}
	default:
		return nullifyButtonColors{
			fill:   theme.Default.Bg,
			border: theme.Default.Border,
			text:   theme.Default.Muted2,
		}
	}
}
