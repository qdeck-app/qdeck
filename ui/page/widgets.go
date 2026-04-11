package page

import (
	"image"
	"image/color"
	"math"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"golang.org/x/image/math/fixed"

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
	textBtnPaddingH unit.Dp = 6

	textBtnPaddingV = cardPaddingV // match card row height

	textBtnCornerRadius unit.Dp = 4
)

const (
	focusBorderWidth unit.Dp = 2
)

const (
	editorFieldPadH        unit.Dp = 8
	editorFieldPadV        unit.Dp = 6
	editorFieldRadius      unit.Dp = 4
	editorFieldBorderWidth unit.Dp = 1

	presetChipRadius unit.Dp = 12
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

		// Fill available width so all fields are the same length.
		gtx.Constraints.Min.X = gtx.Constraints.Max.X

		// Record editor layout to measure size for border.
		m := op.Record(gtx.Ops)
		dims := layout.Inset{
			Left: editorFieldPadH, Right: editorFieldPadH,
			Top: editorFieldPadV, Bottom: editorFieldPadV,
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return customwidget.LayoutEditor(gtx, th.Shaper, ed)
		})
		c := m.Stop()

		bounds := image.Rectangle{Max: dims.Size}
		radius := gtx.Dp(editorFieldRadius)
		bw := gtx.Dp(editorFieldBorderWidth)

		paintRoundedBorder(gtx, bounds, radius, bw, theme.ColorInputBorder, theme.ColorDropdownBg)

		c.Add(gtx.Ops)

		return dims
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

// LayoutCompactTextButton renders a clickable text link with minimal vertical padding,
// suitable for embedding in rows that already provide their own vertical spacing (e.g. breadcrumb).
func LayoutCompactTextButton(gtx layout.Context, th *material.Theme, click *widget.Clickable, label string) layout.Dimensions {
	hovered := click.Hovered()

	lbl := material.Body2(th, label)
	lbl.Color = theme.ColorAccent

	m := op.Record(gtx.Ops)
	dims := click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: textBtnPaddingH, Right: textBtnPaddingH}.Layout(gtx, lbl.Layout)
	})
	c := m.Stop()

	paintHoverBg(gtx, dims, hovered)

	c.Add(gtx.Ops)

	pushPointerCursor(gtx, dims, click)

	return dims
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

// layoutIconButton renders an icon widget inside a clickable area whose hover zone
func layoutIconButton(
	gtx layout.Context, th *material.Theme, click *widget.Clickable, icon layout.Widget,
) layout.Dimensions {
	hovered := click.Hovered()

	m := op.Record(gtx.Ops)
	dims := click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{
			Left: textBtnPaddingH, Right: textBtnPaddingH,
			Top: textBtnPaddingV, Bottom: textBtnPaddingV,
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = gtx.Sp(material.Body2(th, "X").TextSize)

			return layout.Center.Layout(gtx, icon)
		})
	})
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

func layoutPanelLabel(gtx layout.Context, th *material.Theme, text string, top, bottom unit.Dp) layout.Dimensions {
	return layout.Inset{Top: top, Bottom: bottom}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return customwidget.LayoutLabel(gtx, material.Body2(th, text))
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

const ellipsisPrefix = "\u2026/"

// truncatePathLeft truncates a file path from the left so it fits within maxWidthPx,
// prepending "…/" when segments are removed. If the path fits, it is returned as-is.
func truncatePathLeft(lbl *material.LabelStyle, gtx layout.Context, maxWidthPx int, path string) string {
	params := text.Parameters{
		Font:     lbl.Font,
		PxPerEm:  fixed.I(gtx.Sp(lbl.TextSize)),
		MaxWidth: math.MaxInt,
	}

	if measureTextWidth(lbl.Shaper, params, path) <= maxWidthPx {
		return path
	}

	budget := maxWidthPx - measureTextWidth(lbl.Shaper, params, ellipsisPrefix)
	remaining := path

	for {
		idx := strings.IndexByte(remaining, filepath.Separator)
		if idx < 0 {
			break
		}

		remaining = remaining[idx+1:]

		if measureTextWidth(lbl.Shaper, params, remaining) <= budget {
			return ellipsisPrefix + remaining
		}
	}

	// Even just the filename doesn't fit with ellipsis; return it anyway (label MaxLines will clip).
	return ellipsisPrefix + remaining
}

// measureTextWidth returns the pixel width of the given text when shaped with the provided font parameters.
func measureTextWidth(shaper *text.Shaper, params text.Parameters, str string) int {
	shaper.LayoutString(params, str)

	var width fixed.Int26_6

	for g, ok := shaper.NextGlyph(); ok; g, ok = shaper.NextGlyph() {
		width += g.Advance
	}

	return width.Ceil()
}
