package widget

import (
	"image"
	"image/color"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

const defaultBarWidth unit.Dp = 10

const (
	splitDivisor  = 2   // divides bar width and proportion formula
	splitMaxRatio = 0.9 // prevents collapsing a panel completely
)

// SplitView implements a resizable horizontal split panel.
type SplitView struct {
	// Ratio is the split position: 0 = center, -1 = left edge, 1 = right edge.
	Ratio float32
	// Bar is the width of the draggable divider.
	Bar unit.Dp

	drag   bool
	dragID pointer.ID
	dragX  float32
}

//nolint:mnd // Color component values are design tokens.
var splitBarColor = color.NRGBA{R: 220, G: 220, B: 220, A: 255}

// Layout renders the left and right children separated by a draggable vertical bar.
func (s *SplitView) Layout(gtx layout.Context, left, right layout.Widget) layout.Dimensions {
	bar := gtx.Dp(s.Bar)
	if bar <= 1 {
		bar = gtx.Dp(defaultBarWidth)
	}

	proportion := (s.Ratio + 1) / splitDivisor

	leftSize := max(int(proportion*float32(gtx.Constraints.Max.X))-bar/splitDivisor, 0)

	rightOffset := leftSize + bar

	rightSize := max(gtx.Constraints.Max.X-rightOffset, 0)

	{
		barRect := image.Rect(leftSize, 0, rightOffset, gtx.Constraints.Max.Y)
		area := clip.Rect(barRect).Push(gtx.Ops)
		event.Op(gtx.Ops, s)
		pointer.CursorColResize.Add(gtx.Ops)
		area.Pop()

		barArea := clip.Rect(barRect).Push(gtx.Ops)
		paint.ColorOp{Color: splitBarColor}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		barArea.Pop()
	}

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: s,
			Kinds:  pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel,
		})
		if !ok {
			break
		}

		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}

		switch e.Kind {
		case pointer.Press:
			if s.drag {
				break
			}

			s.dragID = e.PointerID
			s.dragX = e.Position.X
			s.drag = true
		case pointer.Drag:
			if s.dragID != e.PointerID {
				break
			}

			if gtx.Constraints.Max.X > 0 {
				deltaX := e.Position.X - s.dragX
				deltaRatio := deltaX * splitDivisor / float32(gtx.Constraints.Max.X)

				s.Ratio += deltaRatio
				s.Ratio = max(min(s.Ratio, splitMaxRatio), -splitMaxRatio)
			}

			s.dragX = e.Position.X
		case pointer.Release, pointer.Cancel:
			s.drag = false
		}
	}

	// Layout left child
	{
		gtx := gtx
		gtx.Constraints = layout.Exact(image.Pt(leftSize, gtx.Constraints.Max.Y))
		left(gtx)
	}

	// Layout right child
	{
		off := op.Offset(image.Pt(rightOffset, 0)).Push(gtx.Ops)
		gtx := gtx
		gtx.Constraints = layout.Exact(image.Pt(rightSize, gtx.Constraints.Max.Y))
		right(gtx)
		off.Pop()
	}

	return layout.Dimensions{Size: gtx.Constraints.Max}
}
