package widget

// vscode logo in svg:
// <svg fill="#000000" width="800px" height="800px" viewBox="0 0 32 32" xmlns="http://www.w3.org/2000/svg">
//  <path d="M30.865 3.448l-6.583-3.167c-0.766-0.37-1.677-0.214-2.276 0.385l-12.609 11.505-5.495-4.167c-0.51-0.391-1.229-0.359-1.703 0.073l-1.76 1.604c-0.583 0.526-0.583 1.443-0.005 1.969l4.766 4.349-4.766 4.349c-0.578 0.526-0.578 1.443 0.005 1.969l1.76 1.604c0.479 0.432 1.193 0.464 1.703 0.073l5.495-4.172 12.615 11.51c0.594 0.599 1.505 0.755 2.271 0.385l6.589-3.172c0.693-0.333 1.13-1.031 1.13-1.802v-21.495c0-0.766-0.443-1.469-1.135-1.802zM24.005 23.266l-9.573-7.266 9.573-7.266z"/>
// </svg>

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

const (
	defaultVSCodeIconSize unit.Dp = 12
	vscodeViewBox         float32 = 32 // SVG viewBox is 0 0 32 32
)

// pathOp identifies a path drawing command.
type pathOp byte

const (
	opMove  pathOp = 'M'
	opLine  pathOp = 'L'
	opCubic pathOp = 'C'
	opClose pathOp = 'Z'
)

// pathCmd is a single path command with up to 3 control points.
type pathCmd struct {
	op  pathOp
	pts [3]f32.Point // Move/Line use pts[0]; Cubic uses all three.
}

// vscodeIconPath contains the VS Code logo converted from SVG to absolute coordinates.
// Original viewBox: 0 0 32 32. Two sub-paths: outer shape + inner triangle cutout.
//
//nolint:gochecknoglobals // icon path data is package-level constant data
var vscodeIconPath = [...]pathCmd{
	// Outer shape.
	{op: opMove, pts: [3]f32.Point{{X: 30.865, Y: 3.448}}},
	{op: opLine, pts: [3]f32.Point{{X: 24.282, Y: 0.281}}},
	{op: opCubic, pts: [3]f32.Point{{X: 23.516, Y: -0.089}, {X: 22.605, Y: 0.067}, {X: 22.006, Y: 0.666}}},
	{op: opLine, pts: [3]f32.Point{{X: 9.397, Y: 12.171}}},
	{op: opLine, pts: [3]f32.Point{{X: 3.902, Y: 8.004}}},
	{op: opCubic, pts: [3]f32.Point{{X: 3.392, Y: 7.613}, {X: 2.673, Y: 7.645}, {X: 2.199, Y: 8.077}}},
	{op: opLine, pts: [3]f32.Point{{X: 0.439, Y: 9.681}}},
	{op: opCubic, pts: [3]f32.Point{{X: -0.144, Y: 10.207}, {X: -0.144, Y: 11.124}, {X: 0.434, Y: 11.650}}},
	{op: opLine, pts: [3]f32.Point{{X: 5.200, Y: 15.999}}},
	{op: opLine, pts: [3]f32.Point{{X: 0.434, Y: 20.348}}},
	{op: opCubic, pts: [3]f32.Point{{X: -0.144, Y: 20.874}, {X: -0.144, Y: 21.791}, {X: 0.439, Y: 22.317}}},
	{op: opLine, pts: [3]f32.Point{{X: 2.199, Y: 23.921}}},
	{op: opCubic, pts: [3]f32.Point{{X: 2.678, Y: 24.353}, {X: 3.392, Y: 24.385}, {X: 3.902, Y: 23.994}}},
	{op: opLine, pts: [3]f32.Point{{X: 9.397, Y: 19.822}}},
	{op: opLine, pts: [3]f32.Point{{X: 22.012, Y: 31.332}}},
	{op: opCubic, pts: [3]f32.Point{{X: 22.606, Y: 31.931}, {X: 23.517, Y: 32.087}, {X: 24.283, Y: 31.717}}},
	{op: opLine, pts: [3]f32.Point{{X: 30.872, Y: 28.545}}},
	{op: opCubic, pts: [3]f32.Point{{X: 31.565, Y: 28.212}, {X: 32.002, Y: 27.514}, {X: 32.002, Y: 26.743}}},
	{op: opLine, pts: [3]f32.Point{{X: 32.002, Y: 5.248}}},
	{op: opCubic, pts: [3]f32.Point{{X: 32.002, Y: 4.482}, {X: 31.559, Y: 3.779}, {X: 30.867, Y: 3.446}}},
	{op: opClose},

	// Inner triangle (cutout — opposite winding creates hole with nonzero fill rule).
	{op: opMove, pts: [3]f32.Point{{X: 24.005, Y: 23.266}}},
	{op: opLine, pts: [3]f32.Point{{X: 14.432, Y: 16.000}}},
	{op: opLine, pts: [3]f32.Point{{X: 24.005, Y: 8.734}}},
	{op: opClose},
}

// LayoutVSCodeIcon draws the VS Code logo at the given size and color.
func LayoutVSCodeIcon(gtx layout.Context, size unit.Dp, clr color.NRGBA) layout.Dimensions {
	if size <= 0 {
		size = defaultVSCodeIconSize
	}

	sizePx := float32(gtx.Dp(size))
	s := sizePx / vscodeViewBox

	spec := buildVSCodePath(gtx.Ops, s)

	defer clip.Outline{Path: spec}.Op().Push(gtx.Ops).Pop()

	paint.ColorOp{Color: clr}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)

	return layout.Dimensions{Size: image.Pt(int(sizePx), int(sizePx))}
}

func buildVSCodePath(ops *op.Ops, s float32) clip.PathSpec {
	var p clip.Path

	p.Begin(ops)

	for i := range vscodeIconPath {
		cmd := &vscodeIconPath[i]

		switch cmd.op {
		case opMove:
			p.MoveTo(scalePoint(cmd.pts[0], s))
		case opLine:
			p.LineTo(scalePoint(cmd.pts[0], s))
		case opCubic:
			p.CubeTo(scalePoint(cmd.pts[0], s), scalePoint(cmd.pts[1], s), scalePoint(cmd.pts[2], s))
		case opClose:
			p.Close()
		}
	}

	return p.End()
}

func scalePoint(pt f32.Point, s float32) f32.Point {
	return f32.Pt(pt.X*s, pt.Y*s)
}
