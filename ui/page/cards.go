package page

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
	"gioui.org/widget"

	"github.com/qdeck-app/qdeck/ui/theme"
)

// Card drop-shadow layers — pure alpha overlays. Theme-agnostic; they
// composite as a soft darken on whatever surface lies beneath. Inlined
// here (the only consumer) instead of routed through theme.Default,
// since the design tokens are explicitly OKLCH colors with hue and
// these have no hue.
//
//nolint:mnd // 28/20 alpha values are inherent to the shadow design.
var (
	cardShadowOuter = color.NRGBA{A: 28}
	cardShadowInner = color.NRGBA{A: 20}
)

const (
	cardCornerRadius  unit.Dp = 6
	cardItemSpacing   unit.Dp = 4
	cardPaddingH      unit.Dp = 12
	cardPaddingV      unit.Dp = 8
	cardShadowOffsetY unit.Dp = 2
	cardShadowSpread  unit.Dp = 1
)

const (
	sectionCardRadius   unit.Dp = 10
	sectionCardPaddingH unit.Dp = 14
	sectionCardPaddingV unit.Dp = 8
	sectionCardMarginH  unit.Dp = 8
	sectionCardSpacing  unit.Dp = 10
)

const focusBorderWidth unit.Dp = 2

// layoutSectionCard wraps a full page section (e.g. Charts, Repositories, Values)
// in a rounded card with background, shadow, and inner padding.
func layoutSectionCard(gtx layout.Context, w layout.Widget) layout.Dimensions {
	return layout.Inset{
		Left: sectionCardMarginH, Right: sectionCardMarginH,
		Bottom: sectionCardSpacing,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		m := op.Record(gtx.Ops)
		dims := layout.Inset{
			Left: sectionCardPaddingH, Right: sectionCardPaddingH,
			Top: sectionCardPaddingV, Bottom: sectionCardPaddingV,
		}.Layout(gtx, w)
		c := m.Stop()

		bounds := image.Rectangle{Max: dims.Size}
		radius := gtx.Dp(sectionCardRadius)

		paintCardShadow(gtx, bounds, radius)

		bgRect := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
		paint.ColorOp{Color: theme.Default.Bg2}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		bgRect.Pop()

		c.Add(gtx.Ops)

		return dims
	})
}

// layoutStaticCard renders content in a non-clickable card with rounded corners and background.
func layoutStaticCard(gtx layout.Context, w layout.Widget) layout.Dimensions {
	return layout.Inset{Bottom: cardItemSpacing}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		m := op.Record(gtx.Ops)
		dims := layout.Inset{
			Left: cardPaddingH, Right: cardPaddingH,
			Top: cardPaddingV, Bottom: cardPaddingV,
		}.Layout(gtx, w)
		c := m.Stop()

		bounds := image.Rectangle{Max: dims.Size}
		radius := gtx.Dp(cardCornerRadius)

		bgRect := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
		paint.ColorOp{Color: theme.Default.Bg2}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		bgRect.Pop()

		c.Add(gtx.Ops)

		return dims
	})
}

// layoutCardFocusable wraps a Clickable in a card with keyboard focus support.
// When focused is true, a blue focus border is drawn around the card.
func layoutCardFocusable(gtx layout.Context, click *widget.Clickable, focused bool, w layout.Widget) layout.Dimensions {
	return layout.Inset{Bottom: cardItemSpacing}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		hovered := click.Hovered()
		gtx.Constraints.Min.X = gtx.Constraints.Max.X

		m := op.Record(gtx.Ops)
		dims := click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{
				Left: cardPaddingH, Right: cardPaddingH,
				Top: cardPaddingV, Bottom: cardPaddingV,
			}.Layout(gtx, w)
		})
		c := m.Stop()

		bounds := image.Rectangle{Max: dims.Size}
		radius := gtx.Dp(cardCornerRadius)

		paintCardShadow(gtx, bounds, radius)

		// Fill lighter than the Bg2 section card so each row reads as a raised
		// tile instead of blending into the panel behind it.
		bgRect := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
		paint.ColorOp{Color: theme.Default.Bg}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		bgRect.Pop()

		// Active-state wash painted over the tile fill.
		var wash color.NRGBA

		switch {
		case focused:
			wash = theme.Default.RowSelected
		case hovered:
			wash = theme.Default.RowHover
		}

		if wash != (color.NRGBA{}) {
			washRect := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
			paint.ColorOp{Color: wash}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			washRect.Pop()
		}

		// Edge: a focused row gets the strong focus ring; every other row gets a
		// hairline border so its boundary stays crisp against the section card.
		if focused {
			paintFocusBorder(gtx, bounds, gtx.Dp(focusBorderWidth))
		} else {
			paintEdgeBorder(gtx, bounds, gtx.Dp(theme.Default.HairlineWidth), theme.Default.Border)
		}

		c.Add(gtx.Ops)

		pass := pointer.PassOp{}.Push(gtx.Ops)
		area := clip.UniformRRect(bounds, radius).Push(gtx.Ops)

		event.Op(gtx.Ops, click)
		pointer.CursorPointer.Add(gtx.Ops)

		area.Pop()
		pass.Pop()

		return dims
	})
}

// paintCardShadow draws a two-layer drop shadow behind a card to give subtle depth.
// Layer 1 (outer): offset down, expanded by spread. Layer 2 (inner): offset down half.
func paintCardShadow(gtx layout.Context, bounds image.Rectangle, radius int) {
	offsetY := gtx.Dp(cardShadowOffsetY)
	spread := gtx.Dp(cardShadowSpread)

	outer := bounds
	outer.Min.X -= spread
	outer.Min.Y += offsetY - spread
	outer.Max.X += spread
	outer.Max.Y += offsetY + spread

	outerClip := clip.UniformRRect(outer, radius+spread).Push(gtx.Ops)
	paint.ColorOp{Color: cardShadowOuter}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	outerClip.Pop()

	inner := bounds
	inner.Min.Y += offsetY / 2 //nolint:mnd // half of shadow offset
	inner.Max.Y += offsetY / 2 //nolint:mnd // half of shadow offset

	innerClip := clip.UniformRRect(inner, radius).Push(gtx.Ops)
	paint.ColorOp{Color: cardShadowInner}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	innerClip.Pop()
}
