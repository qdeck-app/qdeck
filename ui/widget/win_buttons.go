package widget

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"

	"github.com/qdeck-app/qdeck/ui/theme"
)

const (
	winBtnIconSize   = unit.Dp(16)
	winBtnIconMargin = unit.Dp(3)
	winBtnIconStroke = unit.Dp(1)
	winBtnPadding    = unit.Dp(8)
	winBtnRadius     = 4
	winBtnHalf       = 2 // divisor for centering
)

// WinButtons holds clickable state for window control buttons (minimize, maximize, close).
type WinButtons struct {
	Minimize  widget.Clickable
	Maximize  widget.Clickable
	Close     widget.Clickable
	Maximized bool
}

// Layout renders minimize, maximize/restore, and close buttons in a horizontal row.
func (wb *WinButtons) Layout(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wb.layoutButton(gtx, &wb.Minimize, theme.ColorHover, theme.ColorSecondary, drawMinimize)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if wb.Maximized {
				return wb.layoutButton(gtx, &wb.Maximize, theme.ColorHover, theme.ColorSecondary, drawRestore)
			}

			return wb.layoutButton(gtx, &wb.Maximize, theme.ColorHover, theme.ColorSecondary, drawMaximize)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return wb.layoutButton(gtx, &wb.Close, theme.ColorDanger, theme.ColorWhite, drawClose)
		}),
	)
}

func (wb *WinButtons) layoutButton(
	gtx layout.Context,
	click *widget.Clickable,
	hoverColor color.NRGBA,
	hoverIconColor color.NRGBA,
	icon func(gtx layout.Context),
) layout.Dimensions {
	hovered := click.Hovered()

	m := op.Record(gtx.Ops)

	dims := click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(winBtnPadding).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			size := gtx.Dp(winBtnIconSize)
			gtx.Constraints = layout.Exact(image.Pt(size, size))

			iconColor := theme.ColorSecondary
			if hovered {
				iconColor = hoverIconColor
			}

			paint.ColorOp{Color: iconColor}.Add(gtx.Ops)
			icon(gtx)

			return layout.Dimensions{Size: image.Pt(size, size)}
		})
	})

	c := m.Stop()

	if hovered {
		bounds := image.Rectangle{Max: dims.Size}
		radius := gtx.Dp(winBtnRadius)
		bg := clip.UniformRRect(bounds, radius).Push(gtx.Ops)

		paint.ColorOp{Color: hoverColor}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		bg.Pop()
	}

	c.Add(gtx.Ops)

	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops)

	event.Op(gtx.Ops, click)
	pointer.CursorPointer.Add(gtx.Ops)

	area.Pop()
	pass.Pop()

	return dims
}

func drawMinimize(gtx layout.Context) {
	size := gtx.Dp(winBtnIconSize)
	size32 := float32(size)
	margin := float32(gtx.Dp(winBtnIconMargin))
	width := float32(gtx.Dp(winBtnIconStroke))

	var p clip.Path

	p.Begin(gtx.Ops)
	p.MoveTo(f32.Point{X: margin, Y: size32 / winBtnHalf})
	p.LineTo(f32.Point{X: size32 - margin, Y: size32 / winBtnHalf})

	st := clip.Stroke{Path: p.End(), Width: width}.Op().Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
}

func drawMaximize(gtx layout.Context) {
	size := gtx.Dp(winBtnIconSize)
	margin := gtx.Dp(winBtnIconMargin)
	width := gtx.Dp(winBtnIconStroke)

	r := clip.RRect{Rect: image.Rect(margin, margin, size-margin, size-margin)}
	st := clip.Stroke{Path: r.Path(gtx.Ops), Width: float32(width)}.Op().Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
}

func drawRestore(gtx layout.Context) {
	size := gtx.Dp(winBtnIconSize)
	margin := gtx.Dp(winBtnIconMargin)
	width := gtx.Dp(winBtnIconStroke)

	// Front (smaller) rectangle.
	r := clip.RRect{Rect: image.Rect(margin, winBtnHalf*margin, size-winBtnHalf*margin, size-margin)}
	st := clip.Stroke{Path: r.Path(gtx.Ops), Width: float32(width)}.Op().Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()

	// Back (offset) rectangle.
	r = clip.RRect{Rect: image.Rect(winBtnHalf*margin, margin, size-margin, size-winBtnHalf*margin)}
	st = clip.Stroke{Path: r.Path(gtx.Ops), Width: float32(width)}.Op().Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
}

func drawClose(gtx layout.Context) {
	size := gtx.Dp(winBtnIconSize)
	size32 := float32(size)
	margin := float32(gtx.Dp(winBtnIconMargin))
	width := float32(gtx.Dp(winBtnIconStroke))

	var p clip.Path

	p.Begin(gtx.Ops)
	p.MoveTo(f32.Point{X: margin, Y: margin})
	p.LineTo(f32.Point{X: size32 - margin, Y: size32 - margin})
	p.MoveTo(f32.Point{X: size32 - margin, Y: margin})
	p.LineTo(f32.Point{X: margin, Y: size32 - margin})

	st := clip.Stroke{Path: p.End(), Width: width}.Op().Push(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	st.Pop()
}
