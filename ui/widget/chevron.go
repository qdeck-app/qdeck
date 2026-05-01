package widget

import (
	"image"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/qdeck-app/qdeck/ui/theme"
)

func drawChevronTriangle(gtx layout.Context, slot image.Point, collapsed bool) {
	const half float32 = 0.5

	s := float32(gtx.Dp(overrideChevronSize))
	cx := float32(slot.X) * half
	cy := float32(slot.Y) * half
	arm := s * half

	var p clip.Path

	p.Begin(gtx.Ops)

	if collapsed {
		p.MoveTo(f32.Pt(cx-arm, cy-arm))
		p.LineTo(f32.Pt(cx+arm, cy))
		p.LineTo(f32.Pt(cx-arm, cy+arm))
	} else {
		p.MoveTo(f32.Pt(cx-arm, cy-arm))
		p.LineTo(f32.Pt(cx+arm, cy-arm))
		p.LineTo(f32.Pt(cx, cy+arm))
	}

	p.Close()

	defer clip.Outline{Path: p.End()}.Op().Push(gtx.Ops).Pop()

	paint.ColorOp{Color: theme.Default.Muted}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}
