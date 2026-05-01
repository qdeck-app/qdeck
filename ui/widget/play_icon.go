package widget

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

const (
	defaultPlayIconSize unit.Dp = 12
	playIconTipDivisor  float32 = 2    // triangle tip is at height/2
	playIconInset       float32 = 0.15 // 15% inset on each side
)

// LayoutPlayIcon draws a Material Design play icon (right-pointing triangle) with the given color.
//
// The triangle paints inside a centered inset (playIconInset on each side)
// rather than filling the full bounding box, so its visual mass matches
// other icons that have built-in whitespace (e.g. LayoutDownloadIcon).
// The reported dimensions still equal the requested size so the icon
// composes predictably in flex layouts.
func LayoutPlayIcon(gtx layout.Context, size unit.Dp, clr color.NRGBA) layout.Dimensions {
	if size <= 0 {
		size = defaultPlayIconSize
	}

	sizePx := gtx.Dp(size)
	inset := float32(sizePx) * playIconInset
	innerSize := float32(sizePx) - 2*inset //nolint:mnd // 2 sides of inset.

	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(inset, inset))
	p.LineTo(f32.Pt(inset, inset+innerSize))
	p.LineTo(f32.Pt(inset+innerSize, inset+innerSize/playIconTipDivisor))
	p.Close()

	defer clip.Outline{Path: p.End()}.Op().Push(gtx.Ops).Pop()

	paint.ColorOp{Color: clr}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)

	return layout.Dimensions{Size: image.Pt(sizePx, sizePx)}
}
