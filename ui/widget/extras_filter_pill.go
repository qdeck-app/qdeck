package widget

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	giowidget "gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
)

// LayoutExtrasFilterPill renders the small "✚ extras-only" toggle pill
// that lives at the right edge of the search bar. State lives in the
// supplied *giowidget.Clickable; the caller polls .Clicked elsewhere
// and tracks the active flag (passed in here for color resolution).
//
// Inactive: Bg fill, Border outline, Muted text — reads as available.
// Active:   ExtraBg fill, Extra border, ExtraStrong text — same cyan
// palette as the in-grid extras visuals so the user can tell at a
// glance which filter is engaged.
func LayoutExtrasFilterPill(
	gtx layout.Context,
	th *material.Theme,
	clickable *giowidget.Clickable,
	active bool,
) layout.Dimensions {
	padH := gtx.Dp(theme.Default.ButtonPaddingH)
	hairline := gtx.Dp(theme.Default.HairlineWidth)
	height := gtx.Dp(theme.Default.ButtonHeight)
	radius := height / 2 //nolint:mnd // half-height = full radius for pill shape.

	fill, borderColor, textColor := extrasPillColors(active)

	gtx.Constraints.Min.Y = height
	gtx.Constraints.Max.Y = height

	return clickable.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Measure content first so we know the pill's width.
		measureGtx := gtx
		measureGtx.Constraints.Min = image.Point{}
		measureGtx.Constraints.Max.Y = height

		content := op.Record(gtx.Ops)
		contentDims := extrasPillContent(measureGtx, th, textColor)
		contentCall := content.Stop()

		w := contentDims.Size.X + 2*padH //nolint:mnd // 2 sides of padding.
		h := height

		bgStack := clip.UniformRRect(image.Rectangle{Max: image.Pt(w, h)}, radius).Push(gtx.Ops)
		paint.ColorOp{Color: fill}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		bgStack.Pop()

		paintRect(gtx, image.Rect(0, 0, w, hairline), borderColor)
		paintRect(gtx, image.Rect(0, h-hairline, w, h), borderColor)
		paintRect(gtx, image.Rect(0, 0, hairline, h), borderColor)
		paintRect(gtx, image.Rect(w-hairline, 0, w, h), borderColor)

		pointer.CursorPointer.Add(gtx.Ops)

		yOff := (h - contentDims.Size.Y) / 2 //nolint:mnd // vertical center.
		t := op.Offset(image.Pt(padH, yOff)).Push(gtx.Ops)
		contentCall.Add(gtx.Ops)
		t.Pop()

		return layout.Dimensions{Size: image.Pt(w, h)}
	})
}

func extrasPillColors(active bool) (fill, borderCol, textCol color.NRGBA) {
	if active {
		return theme.Default.ExtraBg, theme.Default.Extra, theme.Default.ExtraStrong
	}

	return theme.Default.Bg, theme.Default.Border, theme.Default.Muted
}

func extrasPillContent(gtx layout.Context, th *material.Theme, textCol color.NRGBA) layout.Dimensions {
	lbl := material.Label(th, theme.Default.SizeSM, "✚ extras-only")
	lbl.Color = textCol
	lbl.Font.Weight = font.Normal
	lbl.MaxLines = 1
	lbl.Alignment = text.Middle

	return LayoutLabel(gtx, lbl)
}
