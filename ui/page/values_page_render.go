package page

import (
	"image"
	"image/color"
	"io"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/platform"
	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

// layoutRenderButtons renders the template-rendering action row (defaults
// button, overrides button, save-as-tgz, show-comments toggle, optional
// loading caption, and — when populated — the helm install command with a
// copy button).
func (p *ValuesPage) layoutRenderButtons(gtx layout.Context) layout.Dimensions {
	if p.State.DefaultValues == nil || p.State.ChartPath == "" {
		return layout.Dimensions{}
	}

	if p.State.RenderDefaultsButton.Clicked(gtx) && p.OnRenderDefaults != nil {
		p.OnRenderDefaults()
	}

	if p.State.RenderOverridesButton.Clicked(gtx) && p.OnRenderOverrides != nil {
		p.OnRenderOverrides()
	}

	if p.State.ShowDocs.Update(gtx) && p.OnShowDocsChanged != nil {
		p.OnShowDocsChanged(p.State.ShowDocs.Value)
	}

	if p.State.SaveChartButton.Clicked(gtx) && p.OnSaveChart != nil {
		p.OnSaveChart()
	}

	if p.State.CopyInstallButton.Clicked(gtx) && p.State.HelmInstallCmd != "" {
		gtx.Execute(clipboard.WriteCmd{
			Type: "text/plain",
			Data: io.NopCloser(strings.NewReader(p.State.HelmInstallCmd)),
		})
	}

	return layout.Inset{
		Left: valuesSpacing, Right: valuesSpacing,
		Bottom: valuesPaddingSmall,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		defaultsHint := platform.ShortcutLabel("\u2318+1", "F3")
		overridesHint := platform.ShortcutLabel("\u2318+2", "F4")

		// defaults + gap + overrides + gap + save + show-comments + loading
		// + flex spacer + helm cmd + gap + copy.
		const maxRenderChildren = 12

		var (
			children [maxRenderChildren]layout.FlexChild
			n        int
		)

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return customwidget.LayoutButton(gtx, p.Theme, customwidget.ButtonDefault,
				&p.State.RenderDefaultsButton, customwidget.LayoutPlayIcon,
				renderDefaultsLabelBase, defaultsHint, false)
		})
		n++

		children[n] = layout.Rigid(layout.Spacer{Width: toolbarBtnGap}.Layout)
		n++

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return customwidget.LayoutButton(gtx, p.Theme, customwidget.ButtonDefault,
				&p.State.RenderOverridesButton, customwidget.LayoutPlayIcon,
				renderOverridesLabelBase, overridesHint, false)
		})
		n++

		children[n] = layout.Rigid(layout.Spacer{Width: toolbarBtnGap}.Layout)
		n++

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return customwidget.LayoutButton(gtx, p.Theme, customwidget.ButtonDefault,
				&p.State.SaveChartButton, customwidget.LayoutDownloadIcon,
				"Save .tgz", "", false)
		})
		n++

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: valuesSpacing}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cb := material.CheckBox(p.Theme, &p.State.ShowDocs, "Show docs")
				cb.Size = showDocsSize
				cb.TextSize = unit.Sp(float32(p.Theme.TextSize) * showDocsTextMult)

				dims := cb.Layout(gtx)

				pushPointerCursor(gtx, dims, &p.State.ShowDocs)

				return dims
			})
		})
		n++

		if p.State.RenderLoading {
			children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: valuesSpacing}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(p.Theme, "Rendering...")
					lbl.Color = theme.Default.Muted

					return customwidget.LayoutLabel(gtx, lbl)
				})
			})
			n++
		}

		if p.State.HelmInstallCmd != "" {
			cmd := p.State.HelmInstallCmd

			children[n] = layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(gtx.Constraints.Min.X, 0)}
			})
			n++

			children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				maxW := int(helmCmdMaxWidthRatio * float32(gtx.Constraints.Max.X))
				gtx.Constraints.Max.X = maxW

				lbl := material.Body2(p.Theme, cmd)
				lbl.Color = theme.Default.Ink2
				lbl.MaxLines = 1

				return customwidget.LayoutLabel(gtx, lbl)
			})
			n++

			children[n] = layout.Rigid(layout.Spacer{Width: toolbarBtnGap}.Layout)
			n++

			children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return customwidget.LayoutButton(gtx, p.Theme, customwidget.ButtonDefault,
					&p.State.CopyInstallButton, nil, copyLabel, "", false)
			})
			n++
		}

		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children[:n]...)
	})
}

// LayoutShortcutsHelp renders a single-line hotkey + color legend hint, sized
// to fit the notification bar's idle slot. Called from the app shell when the
// values page is active and no notification is showing.
func (p *ValuesPage) LayoutShortcutsHelp(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(p.helpMutedLabel(helpShortcutLine)),
		// Flex spacer pushes the color-swatch legend to the right edge so
		// kbd hints sit left, swatches sit right. When the bar is too
		// narrow to fit both, the swatches simply collapse against the
		// hints — Rigid children take precedence over the Flexed gap.
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: gtx.Constraints.Min}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// "extra" — keys defined only in the overlay with no chart
			// default. The cyan-teal swatch matches the in-grid wash and
			// the "+" chip on the key cell.
			return p.layoutLegendItem(gtx, theme.Default.Extra, "extra (override-only)")
		}),
		layout.Rigid(layout.Spacer{Width: helpLegendItemGap}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.layoutLegendItem(gtx, theme.Default.Override, "override")
		}),
		layout.Rigid(layout.Spacer{Width: helpLegendItemGap}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.layoutLegendItem(gtx, theme.Default.Added, "git added")
		}),
		layout.Rigid(layout.Spacer{Width: helpLegendItemGap}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.layoutLegendItem(gtx, theme.Default.Modified, "git modified")
		}),
	)
}

// layoutLegendItem renders an inline colored square glyph followed by the
// label text. Both are Caption-sized, baseline-aligned — the glyph shapes and
// positions like any letter, avoiding icon-vs-text alignment hacks.
func (p *ValuesPage) layoutLegendItem(gtx layout.Context, c color.NRGBA, label string) layout.Dimensions {
	return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			sq := material.Label(p.Theme, theme.Default.SizeSM, "\u25a0") // ■
			sq.Color = c

			return customwidget.LayoutLabel(gtx, sq)
		}),
		layout.Rigid(layout.Spacer{Width: helpGlyphTextGap}.Layout),
		layout.Rigid(p.helpMutedLabel(label)),
	)
}

// helpMutedLabel is the shared rendering used for both the kbd-hint line
// and each legend item's text label. Centralizes the size/color/maxLines
// trio so a future restyle changes one place.
func (p *ValuesPage) helpMutedLabel(txt string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		lbl := material.Label(p.Theme, theme.Default.SizeSM, txt)
		lbl.Color = theme.Default.Muted
		lbl.MaxLines = 1

		return customwidget.LayoutLabel(gtx, lbl)
	}
}
