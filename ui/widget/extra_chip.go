package widget

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
)

// LayoutExtraChip renders the small "+" badge appended to the key cell of
// any row whose key exists ONLY in the overlay file with no chart-defaults
// counterpart. Unlike the type tag, this chip is ALWAYS visible — the user
// always needs to know that a row has no default to fall back to (a typo
// in an extra key would silently render as a row with no effect).
//
// Cyan-teal palette: ExtraBg fill, Extra border, ExtraStrong text.
func LayoutExtraChip(gtx layout.Context, th *material.Theme) layout.Dimensions {
	padH := gtx.Dp(theme.Default.TypeTagPadH)
	padV := gtx.Dp(theme.Default.TypeTagPadV)
	radius := gtx.Dp(theme.Default.TypeTagRadius)
	hairline := gtx.Dp(theme.Default.HairlineWidth)

	lbl := material.Label(th, theme.Default.SizeXXS, "+")
	lbl.Color = theme.Default.ExtraStrong
	lbl.Font.Weight = font.SemiBold
	lbl.MaxLines = 1
	lbl.Alignment = text.Middle

	macro := op.Record(gtx.Ops)
	labelDims := LayoutLabel(gtx, lbl)
	call := macro.Stop()

	w := labelDims.Size.X + 2*padH //nolint:mnd // 2 sides of padding.
	h := labelDims.Size.Y + 2*padV //nolint:mnd // 2 sides of padding.

	bg := clip.UniformRRect(image.Rectangle{Max: image.Pt(w, h)}, radius).Push(gtx.Ops)
	paint.ColorOp{Color: theme.Default.ExtraBg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	bg.Pop()

	paintRect(gtx, image.Rect(0, 0, w, hairline), theme.Default.Extra)
	paintRect(gtx, image.Rect(0, h-hairline, w, h), theme.Default.Extra)
	paintRect(gtx, image.Rect(0, 0, hairline, h), theme.Default.Extra)
	paintRect(gtx, image.Rect(w-hairline, 0, w, h), theme.Default.Extra)

	t := op.Offset(image.Pt(padH, padV)).Push(gtx.Ops)
	call.Add(gtx.Ops)
	t.Pop()

	return layout.Dimensions{Size: image.Pt(w, h)}
}
