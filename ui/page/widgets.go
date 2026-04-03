package page

import (
	"image"
	"image/color"
	"time"

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
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

const separatorHeight unit.Dp = 1

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

const (
	cardCornerRadius unit.Dp = 6
	cardItemSpacing  unit.Dp = 4
	cardPaddingH     unit.Dp = 12
	cardPaddingV     unit.Dp = 8
)

const (
	textBtnPaddingH     unit.Dp = 6
	textBtnPaddingV             = cardPaddingV // match card row height
	textBtnCornerRadius unit.Dp = 4
)

const (
	focusBorderWidth unit.Dp = 2
)

func layoutCenteredLoading(gtx layout.Context, th *material.Theme) layout.Dimensions {
	gtx.Execute(op.InvalidateCmd{})

	gtx.Constraints.Min = gtx.Constraints.Max

	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Body1(th, "Loading...").Layout(gtx)
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
	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
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
	paint.ColorOp{Color: theme.ColorAccent}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	indRect.Pop()

	return layout.Dimensions{Size: size}
}

func layoutEditorField(gtx layout.Context, th *material.Theme, editor *widget.Editor, hint string, bottomPad unit.Dp) layout.Dimensions {
	return layout.Inset{Bottom: bottomPad}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		ed := material.Editor(th, editor, hint)
		ed.Editor.SingleLine = true

		return ed.Layout(gtx)
	})
}

// pushPointerCursor registers a pointer-hand cursor over the given area using
// a PassOp so it takes precedence over parent scroll regions.
func pushPointerCursor(gtx layout.Context, dims layout.Dimensions, tag event.Tag) {
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops)

	event.Op(gtx.Ops, tag)
	pointer.CursorPointer.Add(gtx.Ops)

	area.Pop()
	pass.Pop()
}

// paintHoverBg paints a rounded hover background behind a widget when hovered.
func paintHoverBg(gtx layout.Context, dims layout.Dimensions, hovered bool) {
	if !hovered {
		return
	}

	bounds := image.Rectangle{Max: dims.Size}
	radius := gtx.Dp(textBtnCornerRadius)
	bg := clip.UniformRRect(bounds, radius).Push(gtx.Ops)

	paint.ColorOp{Color: theme.ColorHover}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	bg.Pop()
}

// LayoutTextButton renders a clickable text link with accent color, hover background, and pointer cursor.
func LayoutTextButton(gtx layout.Context, th *material.Theme, click *widget.Clickable, label string, left unit.Dp) layout.Dimensions {
	return layoutActionButton(gtx, th, click, label, theme.ColorAccent, left)
}

func layoutActionButton(
	gtx layout.Context, th *material.Theme, click *widget.Clickable,
	label string, textColor color.NRGBA, left unit.Dp,
) layout.Dimensions {
	return layout.Inset{Left: left}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		hovered := click.Hovered()

		lbl := material.Body2(th, label)
		lbl.Color = textColor

		// Lay out with padding so the hover background has breathing room.
		m := op.Record(gtx.Ops)
		dims := click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{
				Left: textBtnPaddingH, Right: textBtnPaddingH,
				Top: textBtnPaddingV, Bottom: textBtnPaddingV,
			}.Layout(gtx, lbl.Layout)
		})
		c := m.Stop()

		// Hover background.
		paintHoverBg(gtx, dims, hovered)

		c.Add(gtx.Ops)

		pushPointerCursor(gtx, dims, click)

		return dims
	})
}

// layoutClickablePointer wraps a Clickable layout with a pointer hand cursor
// and a hover background highlight.
// Uses a PassOp overlay so the cursor takes precedence over the parent list's
// scroll input area while still passing pointer events through.
func layoutClickablePointer(gtx layout.Context, click *widget.Clickable, w layout.Widget) layout.Dimensions {
	hovered := click.Hovered()

	m := op.Record(gtx.Ops)
	dims := click.Layout(gtx, w)
	c := m.Stop()

	paintHoverBg(gtx, dims, hovered)

	c.Add(gtx.Ops)

	pushPointerCursor(gtx, dims, click)

	return dims
}

func layoutHorizontalSeparator(gtx layout.Context, left, right unit.Dp) layout.Dimensions {
	return layout.Inset{Left: left, Right: right}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		h := gtx.Dp(separatorHeight)
		w := gtx.Constraints.Max.X
		size := image.Pt(w, h)

		rect := clip.Rect{Max: size}.Push(gtx.Ops)
		paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		rect.Pop()

		return layout.Dimensions{Size: size}
	})
}

func layoutPanelLabel(gtx layout.Context, th *material.Theme, text string, left, top, bottom unit.Dp) layout.Dimensions {
	return layout.Inset{Left: left, Top: top, Bottom: bottom}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return material.Body2(th, text).Layout(gtx)
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
		paint.ColorOp{Color: theme.ColorCardBg}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		bgRect.Pop()

		c.Add(gtx.Ops)

		return dims
	})
}

// layoutCappedHeight constrains Max.Y to the given height and lays out the widget.
func layoutCappedHeight(gtx layout.Context, maxHeight unit.Dp, w layout.Widget) layout.Dimensions {
	maxH := gtx.Dp(maxHeight)
	if gtx.Constraints.Max.Y > maxH {
		gtx.Constraints.Max.Y = maxH
	}

	return w(gtx)
}

// layoutCardFocusable wraps a Clickable in a card with keyboard focus support.
// When focused is true, a blue focus border is drawn around the card.
func layoutCardFocusable(gtx layout.Context, click *widget.Clickable, focused bool, w layout.Widget) layout.Dimensions {
	return layout.Inset{Bottom: cardItemSpacing}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		hovered := click.Hovered()

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

		// Card background.
		bgRect := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
		paint.ColorOp{Color: theme.ColorCardBg}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		bgRect.Pop()

		// Hover or focus overlay.
		switch {
		case focused:
			focusRect := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
			paint.ColorOp{Color: theme.ColorFocus}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			focusRect.Pop()

			// Focus border.
			bw := gtx.Dp(focusBorderWidth)
			paintFocusBorder(gtx, bounds, bw)
		case hovered:
			hoverRect := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
			paint.ColorOp{Color: theme.ColorHover}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			hoverRect.Pop()
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

// paintEdgeBorder draws a border by painting four edge rectangles around the bounds in the given color.
func paintEdgeBorder(gtx layout.Context, bounds image.Rectangle, bw int, c color.NRGBA) {
	for _, edge := range customwidget.EdgeBorders(bounds, bw) {
		r := clip.Rect(edge).Push(gtx.Ops)
		paint.ColorOp{Color: c}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		r.Pop()
	}
}

// paintFocusBorder draws a focus border by painting four edge rectangles around the bounds.
func paintFocusBorder(gtx layout.Context, bounds image.Rectangle, bw int) {
	paintEdgeBorder(gtx, bounds, bw, theme.ColorAccent)
}
