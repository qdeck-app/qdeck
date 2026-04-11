package widget

import (
	"image"

	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
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
	breadcrumbMaxChildren = breadcrumbMaxSegments*2 + 1 // logo + spacing + segments + separators

	breadcrumbPaddingH     unit.Dp = 16
	breadcrumbPaddingV     unit.Dp = 10
	breadcrumbSeparatorPad unit.Dp = 6
	breadcrumbBorderHeight unit.Dp = 1
	breadcrumbLogoSize     unit.Dp = 18
	breadcrumbLogoGap      unit.Dp = 6
	breadcrumbBoldWeight           = 700
	breadcrumbDoubleClick          = 2
)

// BreadcrumbSegment holds the label and clickable for one breadcrumb level.
type BreadcrumbSegment struct {
	Label string
	Click widget.Clickable
}

// Breadcrumb renders a horizontal breadcrumb navigation bar.
// Fixed-size array of segments; Count determines how many are active.
// When MoveArea is true the bar registers as a window drag handle.
type Breadcrumb struct {
	Segments  [breadcrumbMaxSegments]BreadcrumbSegment
	Count     int
	MoveArea  bool
	moveClick gesture.Click
}

func (b *Breadcrumb) Clicked(gtx layout.Context) int {
	for i := range b.Count - 1 {
		if b.Segments[i].Click.Clicked(gtx) {
			return i
		}
	}

	return -1
}

// MoveDoubleClicked reports whether the user double-clicked the drag area.
func (b *Breadcrumb) MoveDoubleClicked(src input.Source) bool {
	for {
		ev, ok := b.moveClick.Update(src)
		if !ok {
			return false
		}

		if ev.Kind == gesture.KindClick && ev.NumClicks == breadcrumbDoubleClick {
			return true
		}
	}
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
			padV := gtx.Dp(breadcrumbPaddingV)

			var actionWidth int

			dims := layout.Inset{
				Left: breadcrumbPaddingH, Right: breadcrumbPaddingH,
				Top: breadcrumbPaddingV, Bottom: breadcrumbPaddingV,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				var children [breadcrumbMaxChildren]layout.FlexChild

				n := 0

				children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Right: breadcrumbLogoGap}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return LayoutLogo(gtx, breadcrumbLogoSize, theme.ColorAccent)
					})
				})
				n++

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
					return b.layoutSegmentsWithMove(gtx, children[:n])
				}

				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return b.layoutSegmentsWithMove(gtx, children[:n])
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						d := action(gtx)
						actionWidth = d.Size.X

						return d
					}),
				)
			})

			b.registerMoveStrips(gtx, dims, padV, actionWidth)

			return dims
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

// registerMoveStrips registers window-drag handles on the padding strips of
// the breadcrumb bar. actionWidth is the pixel width of the right-side action
// widget (e.g. window buttons); that region is excluded so drag areas don't
// overlap interactive controls.
func (b *Breadcrumb) registerMoveStrips(gtx layout.Context, dims layout.Dimensions, padV, actionWidth int) {
	if !b.MoveArea {
		return
	}

	w := dims.Size.X
	h := dims.Size.Y
	padH := gtx.Dp(breadcrumbPaddingH)
	midH := h - padV - padV         // content height between top/bottom strips
	dragW := w - padH - actionWidth // width excluding right padding + action

	// Top strip (excludes the action widget area on the right).
	top := clip.Rect{Max: image.Pt(dragW, padV)}.Push(gtx.Ops)
	system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
	b.moveClick.Add(gtx.Ops)
	top.Pop()

	// Bottom strip (excludes the action widget area on the right).
	offB := op.Offset(image.Pt(0, h-padV)).Push(gtx.Ops)
	bot := clip.Rect{Max: image.Pt(dragW, padV)}.Push(gtx.Ops)

	system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
	b.moveClick.Add(gtx.Ops)

	bot.Pop()
	offB.Pop()

	// Left padding column (between top and bottom strips).
	offL := op.Offset(image.Pt(0, padV)).Push(gtx.Ops)
	left := clip.Rect{Max: image.Pt(padH, midH)}.Push(gtx.Ops)

	system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
	b.moveClick.Add(gtx.Ops)

	left.Pop()
	offL.Pop()
}

// layoutSegmentsWithMove lays out breadcrumb segments and, when MoveArea is
// enabled, registers a window-drag handle on the empty space to the right of
// the segments. ActionInputOp(ActionMove) captures all pointer events in its
// area, so it must not overlap with clickable elements.
func (b *Breadcrumb) layoutSegmentsWithMove(gtx layout.Context, children []layout.FlexChild) layout.Dimensions {
	maxX := gtx.Constraints.Max.X

	// Remove min-width so Flex returns actual content width, not the
	// tight constraint imposed by the Flexed parent.
	gtx.Constraints.Min.X = 0
	dims := layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)

	if b.MoveArea {
		contentX := dims.Size.X
		if maxX > contentX {
			off := op.Offset(image.Pt(contentX, 0)).Push(gtx.Ops)
			area := clip.Rect{Max: image.Pt(maxX-contentX, dims.Size.Y)}.Push(gtx.Ops)

			system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
			b.moveClick.Add(gtx.Ops)

			area.Pop()
			off.Pop()
		}
	}

	// Restore full width to satisfy the Flexed parent constraint.
	dims.Size.X = maxX

	return dims
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
