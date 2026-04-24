package page

import (
	"image"
	"image/color"
	"io"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
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

	if p.State.ShowComments.Update(gtx) && p.OnShowCommentsChanged != nil {
		p.OnShowCommentsChanged(p.State.ShowComments.Value)
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

		// defaults + overrides + show-comments + loading + save + spacer + helm cmd + copy
		const maxRenderChildren = 8

		var (
			children [maxRenderChildren]layout.FlexChild
			n        int
		)

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutRenderButton(gtx, p.Theme, &p.State.RenderDefaultsButton,
				renderDefaultsLabelBase+" ("+defaultsHint+")")
		})
		n++

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutRenderButton(gtx, p.Theme, &p.State.RenderOverridesButton,
				renderOverridesLabelBase+" ("+overridesHint+")")
		})
		n++

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutIconTextButton(gtx, p.Theme, &p.State.SaveChartButton, "Save .tgz", 0,
				func(gtx layout.Context) layout.Dimensions {
					return customwidget.LayoutDownloadIcon(gtx, downloadIconSize, theme.ColorAccent)
				})
		})
		n++

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: valuesSpacing}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cb := material.CheckBox(p.Theme, &p.State.ShowComments, "Show comments")
				cb.Size = showCommentsSize
				cb.TextSize = unit.Sp(float32(p.Theme.TextSize) * showCommentsTextMult)

				dims := cb.Layout(gtx)

				pushPointerCursor(gtx, dims, &p.State.ShowComments)

				return dims
			})
		})
		n++

		if p.State.RenderLoading {
			children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: valuesSpacing}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(p.Theme, "Rendering...")
					lbl.Color = theme.ColorSecondary

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
				lbl.Color = theme.ColorSecondary
				lbl.MaxLines = 1

				return customwidget.LayoutLabel(gtx, lbl)
			})
			n++

			children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutTextButton(gtx, p.Theme, &p.State.CopyInstallButton, copyLabel, valuesSpacing)
			})
			n++
		}

		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children[:n]...)
	})
}

// layoutRenderButton renders a transparent button with hover background,
// a play icon on the left, and label text.
func layoutRenderButton(
	gtx layout.Context,
	th *material.Theme,
	click *widget.Clickable,
	label string,
) layout.Dimensions {
	hovered := click.Hovered()

	m := op.Record(gtx.Ops)

	dims := click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{
			Left: renderBtnPaddingH, Right: renderBtnPaddingH,
			Top: renderBtnPaddingV, Bottom: renderBtnPaddingV,
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return customwidget.LayoutPlayIcon(gtx, renderPlayIconSize, theme.ColorAccent)
				}),
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

	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops)

	event.Op(gtx.Ops, click)
	pointer.CursorPointer.Add(gtx.Ops)

	area.Pop()
	pass.Pop()

	return dims
}

// LayoutShortcutsHelp renders a single-line hotkey + color legend hint, sized
// to fit the notification bar's idle slot. Called from the app shell when the
// values page is active and no notification is showing.
func (p *ValuesPage) LayoutShortcutsHelp(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(p.Theme, helpShortcutLine)
			lbl.Color = theme.ColorSecondary
			lbl.MaxLines = 1

			return customwidget.LayoutLabel(gtx, lbl)
		}),
		layout.Rigid(layout.Spacer{Width: helpShortcutTrailGap}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.layoutLegendItem(gtx, theme.ColorScrollMarker, "override")
		}),
		layout.Rigid(layout.Spacer{Width: helpLegendItemGap}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.layoutLegendItem(gtx, theme.ColorGitAddedBar, "git added")
		}),
		layout.Rigid(layout.Spacer{Width: helpLegendItemGap}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.layoutLegendItem(gtx, theme.ColorGitModifiedBar, "git modified")
		}),
	)
}

// layoutLegendItem renders an inline colored square glyph followed by the
// label text. Both are Caption-sized, baseline-aligned — the glyph shapes and
// positions like any letter, avoiding icon-vs-text alignment hacks.
func (p *ValuesPage) layoutLegendItem(gtx layout.Context, c color.NRGBA, label string) layout.Dimensions {
	return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			sq := material.Body2(p.Theme, "\u25a0") // ■
			sq.Color = c

			return customwidget.LayoutLabel(gtx, sq)
		}),
		layout.Rigid(layout.Spacer{Width: helpGlyphTextGap}.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body2(p.Theme, label)
			lbl.Color = theme.ColorSecondary
			lbl.MaxLines = 1

			return customwidget.LayoutLabel(gtx, lbl)
		}),
	)
}
