package widget

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	giowidget "gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
)

// ButtonStyle picks the visual variant.
type ButtonStyle uint8

const (
	// ButtonDefault is the rank-1 toolbar button: warm Bg fill, 1dp Border
	// outline, Ink label, Muted leading glyph, Muted2 hint. Hover deepens
	// the outline and warms the fill; press lands on Bg3; focus-visible
	// rings the button in amber Override at 1dp offset.
	ButtonDefault ButtonStyle = iota
	// ButtonPrimary is the call-to-action: Ink fill, Bg text, no outline.
	// Reserved for the single most important action on a row (e.g. Render
	// with overrides). Hover lifts the fill toward Ink2.
	ButtonPrimary

	// ButtonSecondary is the legacy alias for ButtonDefault. Existing call
	// sites compile unchanged.
	ButtonSecondary = ButtonDefault
)

// ButtonIcon is the signature shared by every vector icon widget in this
// package (LayoutPlayIcon, LayoutDownloadIcon, LayoutVscodeIcon, …). The
// button calls it with its state-aware glyph color and the configured
// ButtonIconSize so the icon dims and disables alongside the label.
//
// Pass nil for buttons with no leading icon.
type ButtonIcon func(gtx layout.Context, size unit.Dp, c color.NRGBA) layout.Dimensions

// LayoutButton renders a styled chrome button. State lives in the supplied
// *giowidget.Clickable (caller polls via .Clicked(gtx) elsewhere).
//
// Anatomy: [ icon · label · hint ]
//
//   - Optional leadingIcon (e.g. customwidget.LayoutPlayIcon) is rendered
//     in Muted color before the label. Pass nil to omit. The icon is
//     painted at ButtonIconSize (12dp) — the vector path scales there
//     evenly, unlike a Unicode glyph which renders smaller than letters.
//   - label is the action text, weight 400, color Ink (default rank).
//   - Optional trailingHint (e.g. "F4") is rendered after the label in
//     Muted2 at SizeXS (10sp).
//
// Pass disabled = true to render the inactive state (Muted2 text + Guide
// border + no cursor). Disabled buttons still register the clickable so
// the caller can observe state, but the visual reads as inert.
func LayoutButton(
	gtx layout.Context,
	th *material.Theme,
	style ButtonStyle,
	clickable *giowidget.Clickable,
	leadingIcon ButtonIcon,
	label, trailingHint string,
	disabled bool,
) layout.Dimensions {
	padH := gtx.Dp(theme.Default.ButtonPaddingH)
	radius := gtx.Dp(theme.Default.ButtonRadius)
	height := gtx.Dp(theme.Default.ButtonHeight)
	hairline := gtx.Dp(theme.Default.HairlineWidth)
	focusOutline := gtx.Dp(theme.Default.ButtonFocusOutline)
	focusOffset := gtx.Dp(theme.Default.ButtonFocusOffset)

	hovered := !disabled && clickable.Hovered()
	pressed := !disabled && clickable.Pressed()
	focused := !disabled && gtx.Focused(clickable)
	cs := pickButtonColors(style, hovered, pressed, disabled)

	gtx.Constraints.Min.Y = height
	gtx.Constraints.Max.Y = height

	return clickable.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		// Measure content with Min.Y = 0 so labels report their natural
		// glyph-bound height (~14dp at SizeLG) instead of being clamped
		// to the button's 26dp tight constraint. Without this, label
		// dims would equal button height and the centering math below
		// becomes a no-op — glyphs would sit at the top of the button.
		measureGtx := gtx
		measureGtx.Constraints.Min = image.Point{}
		measureGtx.Constraints.Max.Y = height

		content := op.Record(gtx.Ops)
		contentDims := buttonContent(measureGtx, th, leadingIcon, label, trailingHint, cs)
		contentCall := content.Stop()

		w := contentDims.Size.X + 2*padH //nolint:mnd // 2 sides of padding.
		h := height

		// Background.
		bgStack := clip.UniformRRect(image.Rectangle{Max: image.Pt(w, h)}, radius).Push(gtx.Ops)
		paint.ColorOp{Color: cs.fill}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		bgStack.Pop()

		// Outline (everything except primary).
		if style != ButtonPrimary {
			paintRect(gtx, image.Rect(0, 0, w, hairline), cs.border)
			paintRect(gtx, image.Rect(0, h-hairline, w, h), cs.border)
			paintRect(gtx, image.Rect(0, 0, hairline, h), cs.border)
			paintRect(gtx, image.Rect(w-hairline, 0, w, h), cs.border)
		}

		// Focus-visible ring: 2dp Override outline, 1dp outside the
		// button edge. Painted as four edge rects against an outset
		// rectangle so it doesn't redraw the button background.
		if focused {
			paintFocusRing(gtx, w, h, focusOffset, focusOutline, theme.Default.Override)
		}

		// Pointer cursor — only when interactive. Disabled buttons get
		// the default cursor (Gio doesn't expose a "not-allowed" cursor
		// directly; default arrow communicates inactivity adequately).
		if !disabled {
			pointer.CursorPointer.Add(gtx.Ops)
		}

		// Center content vertically.
		yOff := (h - contentDims.Size.Y) / 2 //nolint:mnd // vertical center.
		t := op.Offset(image.Pt(padH, yOff)).Push(gtx.Ops)
		contentCall.Add(gtx.Ops)
		t.Pop()

		return layout.Dimensions{Size: image.Pt(w, h)}
	})
}

// buttonColorSet bundles every paintable color a button frame uses, so the
// style/state matrix collapses to a single switch.
type buttonColorSet struct {
	fill   color.NRGBA
	border color.NRGBA
	text   color.NRGBA
	glyph  color.NRGBA
	hint   color.NRGBA
}

func pickButtonColors(style ButtonStyle, hovered, pressed, disabled bool) buttonColorSet {
	switch style {
	case ButtonPrimary:
		cs := buttonColorSet{
			fill:   theme.Default.Ink,
			border: color.NRGBA{},
			text:   theme.Default.Bg,
			glyph:  theme.Default.Bg,
			hint:   theme.Default.Bg, // primary's reverse polarity — keep readable
		}

		switch {
		case disabled:
			cs.fill = theme.Default.Muted
		case pressed:
			cs.fill = theme.Default.Ink2
		case hovered:
			cs.fill = theme.Default.Ink2
		}

		return cs
	default: // ButtonDefault
		cs := buttonColorSet{
			fill:   theme.Default.Bg,
			border: theme.Default.Border,
			text:   theme.Default.Ink,
			glyph:  theme.Default.Muted,
			hint:   theme.Default.Muted2,
		}

		switch {
		case disabled:
			cs.text = theme.Default.Muted2
			cs.glyph = theme.Default.Muted2
			cs.hint = theme.Default.Muted2
			cs.border = theme.Default.Guide
		case pressed:
			cs.fill = theme.Default.Bg3
			// Border stays at the hover color so the press-state
			// reads as "I'm engaged with the hovered button" rather
			// than a separate visual identity.
			cs.border = theme.Default.BorderStrong
		case hovered:
			cs.fill = theme.Default.Bg2
			cs.border = theme.Default.BorderStrong
		}

		return cs
	}
}

// paintFocusRing draws a 4-sided rectangle outset by `offset` from the
// button's bounds at `outline` width, in the supplied color. Used for the
// keyboard-focus indicator — never the browser blue.
func paintFocusRing(gtx layout.Context, w, h, offset, outline int, c color.NRGBA) {
	// Outer bounds of the ring.
	x0, y0 := -offset, -offset
	x1, y1 := w+offset, h+offset

	// Top.
	paintRect(gtx, image.Rect(x0, y0, x1, y0+outline), c)
	// Bottom.
	paintRect(gtx, image.Rect(x0, y1-outline, x1, y1), c)
	// Left.
	paintRect(gtx, image.Rect(x0, y0, x0+outline, y1), c)
	// Right.
	paintRect(gtx, image.Rect(x1-outline, y0, x1, y1), c)
}

func buttonContent(
	gtx layout.Context,
	th *material.Theme,
	leadingIcon ButtonIcon,
	label, trailingHint string,
	cs buttonColorSet,
) layout.Dimensions {
	flex := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}
	children := make([]layout.FlexChild, 0, 5) //nolint:mnd // up to 5 children (icon+gap+label+gap+hint).

	if leadingIcon != nil {
		children = append(children,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return leadingIcon(gtx, theme.Default.ButtonIconSize, cs.glyph)
			}),
			layout.Rigid(buttonGap(gtx, theme.Default.ButtonContentGap)),
		)
	}

	// Label is weight 400 per the spec — emphasis is reserved for the
	// primary button's reverse polarity (Bg text on Ink fill), not for
	// bumping the default button to medium.
	children = append(children, layout.Rigid(buttonLabel(th, label, cs.text, theme.Default.SizeLG, font.Normal)))

	if trailingHint != "" {
		children = append(children,
			layout.Rigid(buttonGap(gtx, theme.Default.ButtonContentGap)),
			layout.Rigid(buttonLabel(th, trailingHint, cs.hint, theme.Default.SizeXS, font.Normal)),
		)
	}

	return flex.Layout(gtx, children...)
}

func buttonLabel(
	th *material.Theme,
	txt string,
	col color.NRGBA,
	size unit.Sp,
	weight font.Weight,
) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		lbl := material.Label(th, size, txt)
		lbl.Color = col
		lbl.Font.Weight = weight
		lbl.MaxLines = 1
		lbl.Alignment = text.Start

		return LayoutLabel(gtx, lbl)
	}
}

func buttonGap(gtx layout.Context, w unit.Dp) layout.Widget {
	px := gtx.Dp(w)

	return func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: image.Pt(px, 0)}
	}
}
