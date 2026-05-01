package page

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

// hoverOverlay is a translucent black wash used by paintHoverBg. It must
// stack visibly on top of any underlying surface (card body, hovered card
// wash, dialog bg, etc.) — using an opaque token like Default.RowHover
// would collide with the parent card's own hover paint and disappear.
// Alpha 28 ≈ 11%: light enough to read on Bg2, dark enough to remain
// visible when the card behind it is also hovered (RowHover).
//
//nolint:mnd // documented above.
var hoverOverlay = color.NRGBA{A: 28}

// paintHoverBg paints a rounded hover background behind a widget when hovered.
func paintHoverBg(gtx layout.Context, dims layout.Dimensions, hovered bool) {
	if !hovered {
		return
	}

	bounds := image.Rectangle{Max: dims.Size}
	radius := gtx.Dp(textBtnCornerRadius)
	bg := clip.UniformRRect(bounds, radius).Push(gtx.Ops)

	paint.ColorOp{Color: hoverOverlay}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	bg.Pop()
}

// paintRoundedBorder draws a rounded border by painting two concentric rounded rects:
// the outer one in the border color, the inner one in the fill color.
func paintRoundedBorder(gtx layout.Context, bounds image.Rectangle, radius, bw int, border, fill color.NRGBA) {
	// Outer (border color).
	outer := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
	paint.ColorOp{Color: border}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	outer.Pop()

	// Inner (fill color), inset by border width.
	inner := bounds
	inner.Min = inner.Min.Add(image.Pt(bw, bw))
	inner.Max = inner.Max.Sub(image.Pt(bw, bw))

	innerRadius := max(radius-bw, 0)

	innerClip := clip.UniformRRect(inner, innerRadius).Push(gtx.Ops)
	paint.ColorOp{Color: fill}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	innerClip.Pop()
}

func paintEdgeBorder(gtx layout.Context, bounds image.Rectangle, bw int, c color.NRGBA) {
	for _, edge := range customwidget.EdgeBorders(bounds, bw) {
		r := clip.Rect(edge).Push(gtx.Ops)
		paint.ColorOp{Color: c}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		r.Pop()
	}
}

func paintFocusBorder(gtx layout.Context, bounds image.Rectangle, bw int) {
	paintEdgeBorder(gtx, bounds, bw, theme.Default.Ink2)
}
