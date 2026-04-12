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
	defaultDownloadIconSize unit.Dp = 12
	downloadCenterDivisor   float32 = 2    // divide by 2 to center horizontally
	downloadArrowWidth      float32 = 0.3  // arrow shaft width as fraction of size
	downloadArrowHeadWidth  float32 = 0.7  // arrowhead width as fraction of size
	downloadArrowHeadHeight float32 = 0.35 // arrowhead height as fraction of size
	downloadArrowShaftTop   float32 = 0.05 // shaft top as fraction of size
	downloadArrowShaftEnd   float32 = 0.6  // shaft bottom (where head starts) as fraction of size
	downloadTrayTop         float32 = 0.8  // tray top as fraction of size
	downloadTrayHeight      float32 = 0.15 // tray height as fraction of size
	downloadTrayInset       float32 = 0.05 // tray horizontal inset as fraction of size
)

// LayoutDownloadIcon draws a download icon (downward arrow with tray) with the given color.
func LayoutDownloadIcon(gtx layout.Context, size unit.Dp, clr color.NRGBA) layout.Dimensions {
	if size <= 0 {
		size = defaultDownloadIconSize
	}

	sz := float32(gtx.Dp(size))

	// Arrow shaft + head as one polygon.
	shaftL := sz * (1 - downloadArrowWidth) / downloadCenterDivisor
	shaftR := sz * (1 + downloadArrowWidth) / downloadCenterDivisor
	headL := sz * (1 - downloadArrowHeadWidth) / downloadCenterDivisor
	headR := sz * (1 + downloadArrowHeadWidth) / downloadCenterDivisor
	shaftTop := sz * downloadArrowShaftTop
	shaftEnd := sz * downloadArrowShaftEnd
	tipY := shaftEnd + sz*downloadArrowHeadHeight

	var arrow clip.Path

	arrow.Begin(gtx.Ops)
	arrow.MoveTo(f32.Pt(shaftL, shaftTop))
	arrow.LineTo(f32.Pt(shaftR, shaftTop))
	arrow.LineTo(f32.Pt(shaftR, shaftEnd))
	arrow.LineTo(f32.Pt(headR, shaftEnd))
	arrow.LineTo(f32.Pt(sz/downloadCenterDivisor, tipY))
	arrow.LineTo(f32.Pt(headL, shaftEnd))
	arrow.LineTo(f32.Pt(shaftL, shaftEnd))
	arrow.Close()

	defer clip.Outline{Path: arrow.End()}.Op().Push(gtx.Ops).Pop()

	paint.ColorOp{Color: clr}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)

	// Tray (horizontal bar at bottom).
	trayY := sz * downloadTrayTop
	trayH := sz * downloadTrayHeight
	inset := sz * downloadTrayInset
	tray := clip.Rect{
		Min: image.Pt(int(inset), int(trayY)),
		Max: image.Pt(int(sz-inset), int(trayY+trayH)),
	}.Push(gtx.Ops)

	paint.ColorOp{Color: clr}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tray.Pop()

	return layout.Dimensions{Size: image.Pt(int(sz), int(sz))}
}
