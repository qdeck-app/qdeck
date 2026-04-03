package widget

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

// logoAspectRatio is width/height from the SVG viewBox (1247.03 / 1448.37).
const logoAspectRatio float32 = 0.8610

type logoCmd uint8

const (
	logoMove logoCmd = iota
	logoLine
	logoCube
)

type logoOp struct {
	cmd    logoCmd
	x1, y1 float32
	x2, y2 float32
	x3, y3 float32
}

// logoPath holds the pre-computed path data from assets/qdeck.svg.
//
//nolint:gochecknoglobals,mnd // static SVG path data, coordinates pre-normalized to [0,1] by viewBox height
var logoPath = [...]logoOp{
	// Path 1: top-right geometric shape.
	{cmd: logoMove, x1: 0.7639, y1: 0.2222},
	{cmd: logoLine, x1: 0.7639, y1: 0},
	{cmd: logoLine, x1: 0.4305, y1: 0.2222},
	{cmd: logoCube, x1: 0.4305, y1: 0.3143, x2: 0.3932, y2: 0.3975, x3: 0.3329, y3: 0.4579},
	{cmd: logoCube, x1: 0.2725, y1: 0.5183, x2: 0.1893, y2: 0.5556, x3: 0.0972, y3: 0.5556},
	{cmd: logoLine, x1: 0.4305, y1: 0.5556},
	{cmd: logoCube, x1: 0.6146, y1: 0.5556, x2: 0.7639, y2: 0.4063, x3: 0.7639, y3: 0.2222},
	// Path 2: bottom arc.
	{cmd: logoMove, x1: 0.4305, y1: 1.0},
	{cmd: logoCube, x1: 0.6376, y1: 1.0, x2: 0.8116, y2: 0.8583, x3: 0.8610, y3: 0.6667},
	{cmd: logoLine, x1: 0, y1: 0.6667},
	{cmd: logoCube, x1: 0.0494, y1: 0.8583, x2: 0.2235, y2: 1.0, x3: 0.4305, y3: 1.0},
}

// LayoutLogo draws the QDeck logo at the given size (height in Dp).
// The logo is rendered as vector paths matching assets/qdeck.svg.
func LayoutLogo(gtx layout.Context, size unit.Dp, clr color.NRGBA) layout.Dimensions {
	heightPx := float32(gtx.Dp(size))
	widthPx := heightPx * logoAspectRatio

	var p clip.Path

	p.Begin(gtx.Ops)

	for i := range logoPath {
		op := &logoPath[i]

		switch op.cmd {
		case logoMove:
			p.MoveTo(scalePt(op.x1, op.y1, heightPx))
		case logoLine:
			p.LineTo(scalePt(op.x1, op.y1, heightPx))
		case logoCube:
			p.CubeTo(
				scalePt(op.x1, op.y1, heightPx),
				scalePt(op.x2, op.y2, heightPx),
				scalePt(op.x3, op.y3, heightPx),
			)
		}
	}

	defer clip.Outline{Path: p.End()}.Op().Push(gtx.Ops).Pop()

	paint.ColorOp{Color: clr}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)

	w := int(math.Round(float64(widthPx)))
	h := int(math.Round(float64(heightPx)))

	return layout.Dimensions{Size: image.Pt(w, h)}
}

func scalePt(x, y, scale float32) f32.Point {
	return f32.Pt(x*scale, y*scale)
}
