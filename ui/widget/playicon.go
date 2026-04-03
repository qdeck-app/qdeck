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
	playIconTipDivisor  float32 = 2 // triangle tip is at height/2
)

// LayoutPlayIcon draws a Material Design play icon (right-pointing triangle) with the given color.
func LayoutPlayIcon(gtx layout.Context, size unit.Dp, clr color.NRGBA) layout.Dimensions {
	if size <= 0 {
		size = defaultPlayIconSize
	}

	sizePx := gtx.Dp(size)

	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(0, 0))
	p.LineTo(f32.Pt(0, float32(sizePx)))
	p.LineTo(f32.Pt(float32(sizePx), float32(sizePx)/playIconTipDivisor))
	p.Close()

	defer clip.Outline{Path: p.End()}.Op().Push(gtx.Ops).Pop()

	paint.ColorOp{Color: clr}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)

	return layout.Dimensions{Size: image.Pt(sizePx, sizePx)}
}
