package page

import (
	"image"
	"image/color"

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

const (
	textBtnPaddingH unit.Dp = 6

	textBtnPaddingV = cardPaddingV // match card row height

	textBtnCornerRadius unit.Dp = 4
)

const (
	hotkeyHintPadH   unit.Dp = 6
	hotkeyHintPadV   unit.Dp = 2
	hotkeyHintRadius unit.Dp = 4
	hotkeyHintGap    unit.Dp = 2
)

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

// layoutIconTextButton renders a clickable button with a leading icon widget, spacing, and label text.
func layoutIconTextButton(
	gtx layout.Context, th *material.Theme, click *widget.Clickable,
	label string, left unit.Dp, icon layout.Widget,
) layout.Dimensions {
	return layout.Inset{Left: left}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		hovered := click.Hovered()

		m := op.Record(gtx.Ops)

		dims := click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{
				Left: textBtnPaddingH, Right: textBtnPaddingH,
				Top: textBtnPaddingV, Bottom: textBtnPaddingV,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(icon),
					layout.Rigid(layout.Spacer{Width: renderIconSpacing}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(th, label)
						lbl.Color = theme.ColorAccent

						return customwidget.LayoutLabel(gtx, lbl)
					}),
				)
			})
		})

		c := m.Stop()

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

// layoutSectionHeaderWithHint renders a section title on the left and, on the right,
// one pill per key (e.g. ["Tab","Tab"]) followed by a plain-text suffix (e.g. "to focus").
func layoutSectionHeaderWithHint(
	gtx layout.Context, th *material.Theme, title string, keys []string, suffix string, top, bottom unit.Dp,
) layout.Dimensions {
	return layout.Inset{Top: top, Bottom: bottom}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		children := make([]layout.FlexChild, 0, len(keys)*2+3) //nolint:mnd // title + per-key (pill+gap) + trailing gap + suffix
		children = append(children, layout.Flexed(1, customwidget.LabelWidget(material.Body2(th, title))))

		for _, k := range keys {
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutHotkeyHint(gtx, th, k)
				}),
				layout.Rigid(layout.Spacer{Width: hotkeyHintGap}.Layout),
			)
		}

		children = append(children,
			layout.Rigid(layout.Spacer{Width: hotkeyHintGap}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Caption(th, suffix)
				lbl.Color = theme.ColorSecondary

				return customwidget.LayoutLabel(gtx, lbl)
			}),
		)

		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

// layoutHotkeyHint renders text inside a small rounded pill with muted colors.
func layoutHotkeyHint(gtx layout.Context, th *material.Theme, text string) layout.Dimensions {
	lbl := material.Caption(th, text)
	lbl.Color = theme.ColorSecondary

	m := op.Record(gtx.Ops)
	dims := layout.Inset{
		Left: hotkeyHintPadH, Right: hotkeyHintPadH,
		Top: hotkeyHintPadV, Bottom: hotkeyHintPadV,
	}.Layout(gtx, customwidget.LabelWidget(lbl))
	c := m.Stop()

	bounds := image.Rectangle{Max: dims.Size}
	radius := gtx.Dp(hotkeyHintRadius)

	bgClip := clip.UniformRRect(bounds, radius).Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorCardBg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	bgClip.Pop()

	paintEdgeBorder(gtx, bounds, gtx.Dp(1), theme.ColorSeparator)

	c.Add(gtx.Ops)

	return dims
}
