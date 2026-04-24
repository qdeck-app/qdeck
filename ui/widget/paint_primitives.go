package widget

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// EdgeBorders returns four edge rectangles forming a border inside the given bounds.
func EdgeBorders(bounds image.Rectangle, w int) [4]image.Rectangle {
	return [4]image.Rectangle{
		// Top
		{Min: bounds.Min, Max: image.Pt(bounds.Max.X, bounds.Min.Y+w)},
		// Bottom
		{Min: image.Pt(bounds.Min.X, bounds.Max.Y-w), Max: bounds.Max},
		// Left
		{Min: bounds.Min, Max: image.Pt(bounds.Min.X+w, bounds.Max.Y)},
		// Right
		{Min: image.Pt(bounds.Max.X-w, bounds.Min.Y), Max: bounds.Max},
	}
}

// paintRowBg fills a full-width rectangle of the given height with the specified color.
func paintRowBg(gtx layout.Context, height int, c color.NRGBA) {
	rect := clip.Rect{Max: image.Pt(gtx.Constraints.Max.X, height)}.Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	rect.Pop()
}

const gitIndicatorWidth = 4

// paintGitIndicator draws a narrow vertical bar at the given x position.
func paintGitIndicator(gtx layout.Context, x, height int, c color.NRGBA) {
	w := gtx.Dp(gitIndicatorWidth)

	rect := clip.Rect{Min: image.Pt(x, 0), Max: image.Pt(x+w, height)}.Push(gtx.Ops)
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	rect.Pop()
}
