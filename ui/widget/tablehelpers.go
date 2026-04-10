package widget

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// lastSegment returns the portion after the last dot in a key path.
func lastSegment(key string) string {
	idx := strings.LastIndexByte(key, '.')

	if idx < 0 {
		return key
	}

	return key[idx+1:]
}

// parentPath returns the portion before the last dot, or "" for root-level keys.
func parentPath(key string) string {
	idx := strings.LastIndexByte(key, '.')

	if idx < 0 {
		return ""
	}

	return key[:idx]
}

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
