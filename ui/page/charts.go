package page

import (
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/state"
	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

const (
	chartPaddingContent unit.Dp = 16
	chartPaddingSmall   unit.Dp = 4
	chartSearchPaddingV unit.Dp = 10
	chartSpacerSmall    unit.Dp = 8
	chartVersionFlex            = 0.25
	chartDateFormat             = "2006-01-02"
	chartScrollContext          = 2 // rows visible above focused item when scrolling
)

// ChartsPage renders the chart browser for a selected repository.
type ChartsPage struct {
	Theme *material.Theme
	State *state.ChartPageState

	OnSelectChart   func(chartName string)
	OnSelectVersion func(chartName, version string)
	OnSaveChart     func(chartName, version string)

	// filteredBuf is a reusable buffer for filtered indices to avoid per-frame allocation.
	filteredBuf []int

	// lastFiltered caches filtered source indices from the last layout for key nav activation.
	lastFiltered []int

	// cachedQuery avoids per-frame strings.ToLower allocation.
	cachedQueryRaw   string
	cachedQueryLower string
}

//nolint:dupl // same Gio flex pattern as ReposPage but entirely different children
func (p *ChartsPage) Layout(gtx layout.Context) layout.Dimensions {
	p.handleKeyEvents(gtx)

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.layoutSearch),
		layout.Flexed(1, p.layoutContent),
	)
}

func (p *ChartsPage) layoutSearch(gtx layout.Context) layout.Dimensions {
	if p.State.FocusSearch {
		p.State.FocusSearch = false
		gtx.Execute(key.FocusCmd{Tag: &p.State.SearchEditor})
	}

	searchHint := customwidget.ShortcutLabel("\u2318+F", "Ctrl+F")

	hint := "Search charts... (" + searchHint + ")"
	if p.State.SelectedChart != "" {
		hint = "Search versions... (" + searchHint + ")"
	}

	return layout.Inset{
		Left: chartPaddingContent, Right: chartPaddingContent,
		Top: chartSearchPaddingV, Bottom: chartSearchPaddingV,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		ed := material.Editor(p.Theme, &p.State.SearchEditor, hint)
		ed.Editor.SingleLine = true

		return customwidget.LayoutEditor(gtx, p.Theme.Shaper, ed)
	})
}

func (p *ChartsPage) layoutContent(gtx layout.Context) layout.Dimensions {
	if p.State.SelectedChart != "" {
		if p.State.Loading && len(p.State.Versions) == 0 {
			return layoutCenteredLoading(gtx, p.Theme)
		}

		return p.layoutVersions(gtx)
	}

	if p.State.Loading && len(p.State.Charts) == 0 {
		return layoutCenteredLoading(gtx, p.Theme)
	}

	return p.layoutChartList(gtx)
}

func (p *ChartsPage) layoutChartList(gtx layout.Context) layout.Dimensions {
	p.State.EnsureChartClickables(len(p.State.Charts))
	query := p.searchQuery()

	// Build filtered list
	filtered := p.filteredBuf[:0]

	for i := range p.State.Charts {
		if query == "" || strings.Contains(p.State.ChartSearchNames[i], query) ||
			strings.Contains(p.State.ChartSearchDescs[i], query) {
			filtered = append(filtered, i)
		}
	}

	p.filteredBuf = filtered
	p.lastFiltered = append(p.lastFiltered[:0], filtered...)
	p.State.ChartList.Axis = layout.Vertical

	if len(filtered) > 0 && p.State.FocusedIndex >= len(filtered) {
		p.State.FocusedIndex = len(filtered) - 1
	}

	return material.List(p.Theme, &p.State.ChartList).Layout(gtx, len(filtered),
		func(gtx layout.Context, idx int) layout.Dimensions {
			chartIdx := filtered[idx]
			chart := p.State.Charts[chartIdx]

			if p.State.ChartClicks[chartIdx].Clicked(gtx) {
				p.State.SelectedChart = chart.Name
				p.State.FocusedIndex = 0

				if p.OnSelectChart != nil {
					p.OnSelectChart(chart.Name)
				}
			}

			focused := p.State.FocusedIndex == idx

			return layoutCardFocusable(gtx, &p.State.ChartClicks[chartIdx], focused,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(customwidget.LabelWidget(material.Body1(p.Theme, chart.Name))),
								layout.Rigid(layout.Spacer{Width: chartSpacerSmall}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(p.Theme, chart.LatestVersion())
									lbl.Color = theme.ColorAccent

									return customwidget.LayoutLabel(gtx, lbl)
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Caption(p.Theme, chart.Description)
							lbl.Color = theme.ColorSecondary
							lbl.MaxLines = 2

							return customwidget.LayoutLabel(gtx, lbl)
						}),
					)
				})
		})
}

func (p *ChartsPage) layoutVersions(gtx layout.Context) layout.Dimensions {
	p.State.EnsureVersionClickables(len(p.State.Versions))
	query := p.searchQuery()

	filtered := p.filteredBuf[:0]

	for i := range p.State.Versions {
		if query == "" || strings.Contains(p.State.VersionSearchVersions[i], query) ||
			strings.Contains(p.State.VersionSearchAppVersions[i], query) {
			filtered = append(filtered, i)
		}
	}

	p.filteredBuf = filtered
	p.lastFiltered = append(p.lastFiltered[:0], filtered...)

	if len(filtered) > 0 && p.State.FocusedIndex >= len(filtered) {
		p.State.FocusedIndex = len(filtered) - 1
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			p.State.VersionList.Axis = layout.Vertical

			return material.List(p.Theme, &p.State.VersionList).Layout(gtx, len(filtered),
				func(gtx layout.Context, idx int) layout.Dimensions {
					versionIdx := filtered[idx]
					ver := p.State.Versions[versionIdx]

					if p.State.SaveClicks[versionIdx].Clicked(gtx) {
						if p.OnSaveChart != nil {
							p.OnSaveChart(p.State.SelectedChart, ver.Version)
						}
					} else if p.State.VersionClicks[versionIdx].Clicked(gtx) && p.OnSelectVersion != nil {
						p.OnSelectVersion(p.State.SelectedChart, ver.Version)
					}

					focused := p.State.FocusedIndex == idx

					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layoutCardFocusable(gtx, &p.State.VersionClicks[versionIdx], focused,
								func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
										layout.Flexed(chartVersionFlex, func(gtx layout.Context) layout.Dimensions {
											return customwidget.LayoutLabel(gtx, material.Body1(p.Theme, ver.Version))
										}),
										layout.Flexed(chartVersionFlex, func(gtx layout.Context) layout.Dimensions {
											return customwidget.LayoutLabel(gtx, material.Body2(p.Theme, "App: "+ver.AppVersion))
										}),
										layout.Flexed(chartVersionFlex, func(gtx layout.Context) layout.Dimensions {
											lbl := material.Caption(p.Theme, ver.Created.Format(chartDateFormat))
											lbl.Color = theme.ColorSecondary

											return customwidget.LayoutLabel(gtx, lbl)
										}),
									)
								})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return LayoutTextButton(gtx, p.Theme,
								&p.State.SaveClicks[versionIdx], "Save .tgz", chartPaddingSmall)
						}),
					)
				})
		}),
	)
}

func (p *ChartsPage) handleKeyEvents(gtx layout.Context) {
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, p)
	area.Pop()

	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameUpArrow},
			key.Filter{Name: key.NameDownArrow},
			key.Filter{Name: key.NameReturn},
			key.Filter{Name: key.NameEnter},
			key.Filter{Name: key.NameTab},
		)
		if !ok {
			break
		}

		e, isKey := ev.(key.Event)
		if !isKey || e.State != key.Press {
			continue
		}

		switch e.Name {
		case key.NameUpArrow:
			if p.State.FocusedIndex > 0 {
				p.State.FocusedIndex--
				p.scrollToFocused()
			}
		case key.NameDownArrow:
			maxIdx := len(p.lastFiltered) - 1
			if p.State.FocusedIndex < maxIdx {
				p.State.FocusedIndex++
				p.scrollToFocused()
			}
		case key.NameReturn, key.NameEnter:
			p.activateFocused()
		case key.NameTab:
			p.State.FocusSearch = true
		}
	}
}

// scrollToFocused adjusts the active list's scroll position so the focused item is visible.
func (p *ChartsPage) scrollToFocused() {
	target := max(0, p.State.FocusedIndex-chartScrollContext)

	if p.State.SelectedChart != "" {
		p.State.VersionList.Position.First = target
	} else {
		p.State.ChartList.Position.First = target
	}
}

// searchQuery returns the lowercased search query, caching to avoid per-frame allocation.
func (p *ChartsPage) searchQuery() string {
	raw := p.State.SearchEditor.Text()
	if raw != p.cachedQueryRaw {
		p.cachedQueryRaw = raw
		p.cachedQueryLower = strings.ToLower(raw)
	}

	return p.cachedQueryLower
}

func (p *ChartsPage) activateFocused() {
	if p.State.FocusedIndex >= len(p.lastFiltered) {
		return
	}

	srcIdx := p.lastFiltered[p.State.FocusedIndex]

	if p.State.SelectedChart != "" {
		// Version list mode.
		if srcIdx < len(p.State.Versions) && p.OnSelectVersion != nil {
			p.OnSelectVersion(p.State.SelectedChart, p.State.Versions[srcIdx].Version)
		}
	} else {
		// Chart list mode.
		if srcIdx < len(p.State.Charts) {
			p.State.SelectedChart = p.State.Charts[srcIdx].Name
			p.State.FocusedIndex = 0

			if p.OnSelectChart != nil {
				p.OnSelectChart(p.State.Charts[srcIdx].Name)
			}
		}
	}
}
