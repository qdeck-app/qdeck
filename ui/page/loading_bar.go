package page

import (
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

const (
	loadingBarWidth      unit.Dp = 200
	loadingBarHeight     unit.Dp = 3
	loadingBarSpacing    unit.Dp = 12
	loadingCycleDuration         = 1500 * time.Millisecond
)

const loadingIndicatorFraction float32 = 0.3

const (
	pingPongMidpoint float32 = 0.5
	pingPongScale    float32 = 2
)

func layoutCenteredLoading(gtx layout.Context, th *material.Theme) layout.Dimensions {
	gtx.Execute(op.InvalidateCmd{})

	gtx.Constraints.Min = gtx.Constraints.Max

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return customwidget.LayoutLabel(gtx, material.Body1(th, "Loading..."))
			}),
			layout.Rigid(layout.Spacer{Height: loadingBarSpacing}.Layout),
			layout.Rigid(layoutLoadingBar),
		)
	})
}

func layoutLoadingBar(gtx layout.Context) layout.Dimensions {
	barW := gtx.Dp(loadingBarWidth)
	barH := gtx.Dp(loadingBarHeight)
	size := image.Pt(barW, barH)

	// Ping-pong animation: 0→1→0 over loadingCycleDuration.
	cycleMs := loadingCycleDuration.Milliseconds()
	t := float32(gtx.Now.UnixMilli()%cycleMs) / float32(cycleMs)

	if t > pingPongMidpoint {
		t = 1.0 - t
	}

	t *= pingPongScale // Scale to 0..1.

	// Smooth ease-in-out (cubic Hermite).
	t = t * t * (3 - 2*t) //nolint:mnd // cubic Hermite coefficients

	// Track background.
	trackRect := clip.Rect{Max: size}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.Default.Border}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	trackRect.Pop()

	// Sliding indicator.
	indicatorW := int(float32(barW) * loadingIndicatorFraction)
	maxOffset := barW - indicatorW
	offset := int(float32(maxOffset) * t)

	indRect := clip.Rect{
		Min: image.Pt(offset, 0),
		Max: image.Pt(offset+indicatorW, barH),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.Default.Ink2}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	indRect.Pop()

	return layout.Dimensions{Size: size}
}
