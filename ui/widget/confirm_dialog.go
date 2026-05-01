package widget

import (
	"image"
	"image/color"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
)

// ConfirmAction represents the user's choice in the confirm dialog.
type ConfirmAction uint8

const (
	ConfirmNone ConfirmAction = iota
	ConfirmYes
	ConfirmNo
)

const (
	dialogOverlayAlpha          = 128
	dialogMaxWidth      unit.Dp = 400
	dialogMinWidth      unit.Dp = 300
	dialogCornerRadius  unit.Dp = 8
	dialogPadding       unit.Dp = 20
	dialogButtonSpacing unit.Dp = 16
	dialogButtonGap     unit.Dp = 8
)

// ConfirmDialog renders an inline confirmation prompt.
type ConfirmDialog struct {
	YesButton *widget.Clickable
	NoButton  *widget.Clickable
}

func (d *ConfirmDialog) Update(gtx layout.Context) ConfirmAction {
	if d.YesButton.Clicked(gtx) {
		return ConfirmYes
	}

	if d.NoButton.Clicked(gtx) {
		return ConfirmNo
	}

	return ConfirmNone
}

// dialogBackground paints a rounded filled rect sized to the Stack's current
// Min constraint, used as the Expanded child of both ConfirmDialog and
// AnchorDialog. Factored out so the two modal chromes don't duplicate the
// same ~10 lines of paint setup.
func dialogBackground(radius unit.Dp) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		rect := clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, gtx.Dp(radius)).Push(gtx.Ops)
		paint.ColorOp{Color: theme.Default.Bg}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		rect.Pop()

		return layout.Dimensions{Size: gtx.Constraints.Min}
	}
}

func (d *ConfirmDialog) Layout(gtx layout.Context, th *material.Theme, message string) layout.Dimensions {
	// Semi-transparent overlay that blocks pointer events to content underneath.
	overlay := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	paint.ColorOp{Color: color.NRGBA{A: dialogOverlayAlpha}}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	event.Op(gtx.Ops, d)
	overlay.Pop()

	// Consume all pointer events on the overlay so clicks don't pass through.
	for {
		_, ok := gtx.Event(pointer.Filter{
			Target: d,
			Kinds:  pointer.Press | pointer.Release | pointer.Drag | pointer.Scroll,
		})
		if !ok {
			break
		}
	}

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = gtx.Dp(dialogMaxWidth)
		gtx.Constraints.Min.X = gtx.Dp(dialogMinWidth)

		return layout.Stack{}.Layout(gtx,
			layout.Expanded(dialogBackground(dialogCornerRadius)),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(dialogPadding).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return LayoutLabel(gtx, material.Body1(th, message))
						}),
						layout.Rigid(layout.Spacer{Height: dialogButtonSpacing}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Spacing: layout.SpaceBetween}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									btn := material.Button(th, d.NoButton, "Cancel")
									btn.Background = theme.Default.Muted
									btn.Color = theme.Default.Ink

									return btn.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Width: dialogButtonGap}.Layout),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									btn := material.Button(th, d.YesButton, "Confirm")
									btn.Background = theme.Default.Danger

									return btn.Layout(gtx)
								}),
							)
						}),
					)
				})
			}),
		)
	})
}
