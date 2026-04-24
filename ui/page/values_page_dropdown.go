package page

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

// processDropdownEvents consumes button clicks for the recent-values dropdown
// and the +Values column button.
//
// This runs before layout so op.Record'd width-measurement layouts (which
// replay and would otherwise consume the click event in the discarded pass)
// don't swallow user input.
func (p *ValuesPage) processDropdownEvents(gtx layout.Context) {
	if p.State.RecentDropdownToggle.Clicked(gtx) {
		p.State.RecentDropdownOpen = !p.State.RecentDropdownOpen
	}

	if p.State.RecentDropdownDismiss.Clicked(gtx) {
		p.State.RecentDropdownOpen = false
	}

	if p.State.AddColumnButton.Clicked(gtx) && p.OnAddColumn != nil {
		p.OnAddColumn()
	}

	for i := range p.State.RecentValuesFiles {
		if i >= len(p.State.RecentValuesClicks) {
			break
		}

		if p.State.RecentValuesClicks[i].Clicked(gtx) {
			if p.OnSelectRecentValues != nil {
				p.OnSelectRecentValues(p.State.RecentValuesFiles[i].Path)
			}

			p.State.RecentDropdownOpen = false
		}

		if i < len(p.State.RecentValuesRemoveClicks) && p.State.RecentValuesRemoveClicks[i].Clicked(gtx) {
			if p.OnRemoveRecentValues != nil {
				p.OnRemoveRecentValues(i)
			}
		}
	}
}

// layoutRecentDropdownOverlay renders the recent-values dropdown when open.
func (p *ValuesPage) layoutRecentDropdownOverlay(gtx layout.Context) layout.Dimensions {
	if !p.State.RecentDropdownOpen || len(p.State.RecentValuesFiles) == 0 {
		return layout.Dimensions{}
	}

	bounds := gtx.Constraints.Max

	// Semi-transparent overlay to catch clicks outside the dropdown.
	overlayRect := clip.Rect{Max: bounds}.Push(gtx.Ops)
	paint.ColorOp{Color: color.NRGBA{A: dropdownOverlayAlpha}}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	overlayRect.Pop()

	// Register dismiss click area covering the full viewport.
	p.State.RecentDropdownDismiss.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: bounds}
	})

	// Position the dropdown card in the right column, below headers.
	leftPx, rightPx := p.overrideColumnOffset(gtx)
	off := op.Offset(image.Pt(leftPx, p.dropdownTopOffset)).Push(gtx.Ops)

	ddGtx := gtx
	ddGtx.Constraints.Max.X = rightPx
	ddGtx.Constraints.Min.X = 0

	maxH := gtx.Dp(recentDropdownMaxH)
	ddGtx.Constraints.Max.Y = maxH

	p.layoutRecentDropdownCard(ddGtx)
	off.Pop()

	return layout.Dimensions{Size: bounds}
}

// layoutRecentDropdownCard renders the white dropdown card with the list of
// recent files.
func (p *ValuesPage) layoutRecentDropdownCard(gtx layout.Context) layout.Dimensions {
	// Lay out content first to measure its size.
	m := op.Record(gtx.Ops)

	dims := layout.Inset{
		Left: recentDropdownPadH, Right: recentDropdownPadH,
		Top: recentDropdownPadV, Bottom: recentDropdownPadV,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutPanelLabel(gtx, p.Theme, "Recent Values Files", 0, valuesPaddingSmall)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					p.recentDropdownItems()...,
				)
			}),
		)
	})

	c := m.Stop()

	// Card background with rounded corners.
	cardBounds := image.Rectangle{Max: dims.Size}
	radius := gtx.Dp(recentDropdownRadius)

	bgRect := clip.UniformRRect(cardBounds, radius).Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorDropdownBg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	bgRect.Pop()

	// Subtle border.
	borderW := gtx.Dp(recentDropdownBorder)

	paintEdgeBorder(gtx, cardBounds, borderW, theme.ColorSeparator)

	// Replay content on top.
	c.Add(gtx.Ops)

	return dims
}

// recentDropdownItems builds layout children for the recent files list.
// Click handling is done in processDropdownEvents; this method is layout-only.
func (p *ValuesPage) recentDropdownItems() []layout.FlexChild {
	n := 0
	recentCount := min(len(p.State.RecentValuesFiles), maxRecentValues)

	for idx := range recentCount {
		p.recentDropdownChildren[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			entry := p.State.RecentValuesFiles[idx]

			return layoutClickablePointer(gtx, &p.State.RecentValuesClicks[idx],
				func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: recentItemPadV, Bottom: recentItemPadV}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(p.Theme, "")
									lbl.Text = truncatePathLeft(&lbl, gtx, gtx.Constraints.Max.X, entry.Path)
									lbl.Color = theme.ColorAccent
									lbl.MaxLines = 1

									return customwidget.LayoutLabel(gtx, lbl)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layoutActionButton(gtx, p.Theme,
										&p.State.RecentValuesRemoveClicks[idx],
										"x", theme.ColorDanger, valuesPaddingSmall)
								}),
							)
						})
				})
		})
		n++
	}

	return p.recentDropdownChildren[:n]
}
