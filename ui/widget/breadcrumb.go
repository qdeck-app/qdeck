package widget

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
)

const (
	breadcrumbMaxSegments = 4
	breadcrumbMaxChildren = breadcrumbMaxSegments*2 - 1 // segments + separators

	breadcrumbPaddingH     unit.Dp = 16
	breadcrumbPaddingV     unit.Dp = 10
	breadcrumbSeparatorPad unit.Dp = 6
	breadcrumbBorderHeight unit.Dp = 1
	breadcrumbBoldWeight           = 700
)

// BreadcrumbSegment holds the label and clickable for one breadcrumb level.
type BreadcrumbSegment struct {
	Label string
	Click widget.Clickable
}

// Breadcrumb renders a horizontal breadcrumb navigation bar.
// Fixed-size array of segments; Count determines how many are active.
type Breadcrumb struct {
	Segments [breadcrumbMaxSegments]BreadcrumbSegment
	Count    int
}

func (b *Breadcrumb) Clicked(gtx layout.Context) int {
	for i := range b.Count - 1 {
		if b.Segments[i].Click.Clicked(gtx) {
			return i
		}
	}

	return -1
}

func (b *Breadcrumb) Layout(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return b.LayoutWithAction(gtx, th, nil)
}

// LayoutWithAction renders the breadcrumb bar with an optional action widget on the right.
func (b *Breadcrumb) LayoutWithAction(gtx layout.Context, th *material.Theme, action layout.Widget) layout.Dimensions {
	if b.Count == 0 {
		return layout.Dimensions{}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{
				Left: breadcrumbPaddingH, Right: breadcrumbPaddingH,
				Top: breadcrumbPaddingV, Bottom: breadcrumbPaddingV,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var children [breadcrumbMaxChildren]layout.FlexChild

				n := 0

				for i := range b.Count {
					idx := i

					children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return b.layoutSegment(gtx, th, idx)
					})
					n++

					if i < b.Count-1 {
						children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return b.layoutSeparator(gtx, th)
						})
						n++
					}
				}

				if action == nil {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children[:n]...)
				}

				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children[:n]...)
					}),
					layout.Rigid(action),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return b.layoutBorder(gtx)
		}),
	)
}

func (b *Breadcrumb) layoutSegment(gtx layout.Context, th *material.Theme, idx int) layout.Dimensions {
	seg := &b.Segments[idx]
	isLast := idx == b.Count-1

	if isLast {
		lbl := material.Body1(th, seg.Label)
		lbl.Font.Weight = breadcrumbBoldWeight

		return lbl.Layout(gtx)
	}

	// Clickable ancestor: record, paint hover bg, replay, register pointer.
	hovered := seg.Click.Hovered()

	m := op.Record(gtx.Ops)
	dims := seg.Click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Body1(th, seg.Label)
		lbl.Color = theme.ColorAccent

		return lbl.Layout(gtx)
	})
	c := m.Stop()

	if hovered {
		rect := clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops)
		paint.ColorOp{Color: theme.ColorHover}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		rect.Pop()
	}

	c.Add(gtx.Ops)

	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops)

	event.Op(gtx.Ops, &seg.Click)
	pointer.CursorPointer.Add(gtx.Ops)

	area.Pop()
	pass.Pop()

	return dims
}

func (b *Breadcrumb) layoutSeparator(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Inset{
		Left: breadcrumbSeparatorPad, Right: breadcrumbSeparatorPad,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		lbl := material.Body1(th, "/")
		lbl.Color = theme.ColorMuted

		return lbl.Layout(gtx)
	})
}

func (b *Breadcrumb) layoutBorder(gtx layout.Context) layout.Dimensions {
	height := gtx.Dp(breadcrumbBorderHeight)
	size := image.Pt(gtx.Constraints.Max.X, height)
	rect := clip.Rect{Max: size}.Push(gtx.Ops)

	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)

	rect.Pop()

	return layout.Dimensions{Size: size}
}
