package page

import (
	"image"
	"path/filepath"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/domain"
	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/state"
	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

const (
	repoPaddingBottom unit.Dp = 8
	repoPaddingSmall  unit.Dp = 4

	compactDropZoneHeight        unit.Dp = 48
	sectionHeaderPaddingTop      unit.Dp = 12
	sectionHeaderPaddingBottom   unit.Dp = 8
	recentChartsMaxHeight        unit.Dp = 130
	recentValuesEntriesMaxHeight unit.Dp = 130
	repoListMaxHeight            unit.Dp = 170

	homeDropZoneTitle   = "Drop chart directory, Chart.yaml, or .tar.gz here"
	valuesDropZoneTitle = "Drop values file (.yaml / .yml)"
	browseButtonLabel   = "Browse"
	directLinkHint      = "repo/chart:version or oci://registry/chart:version"
	addRepoLabel        = "+ Add Repository"
	cancelRepoLabel     = "- Cancel"

	presetChipPadH unit.Dp = 8
	presetChipPadV unit.Dp = 4
	presetChipGap  unit.Dp = 6
)

// presetRepo holds a predefined Helm repository name and URL.
type presetRepo struct {
	Name string
	URL  string
}

var presetRepos = []presetRepo{
	{"bitnami", "https://charts.bitnami.com/bitnami"},
	{"ingress-nginx", "https://kubernetes.github.io/ingress-nginx"},
	{"jetstack", "https://charts.jetstack.io"},
	{"prometheus", "https://prometheus-community.github.io/helm-charts"},
	{"grafana", "https://grafana.github.io/helm-charts"},
	{"elastic", "https://helm.elastic.co"},
	{"hashicorp", "https://helm.releases.hashicorp.com"},
	{"gitlab", "https://charts.gitlab.io"},
	{"traefik", "https://traefik.github.io/charts"},
	{"argo", "https://argoproj.github.io/argo-helm"},
	{"datadog", "https://helm.datadoghq.com"},
	{"apache", "https://charts.apache.org"},
	{"nvidia", "https://helm.ngc.nvidia.com/nvidia"},
	{"cilium", "https://helm.cilium.io"},
	{"istio", "https://istio-release.storage.googleapis.com/charts"},
	{"harbor", "https://helm.goharbor.io"},
}

// ReposPage renders the repository management view.
type ReposPage struct {
	Theme *material.Theme
	State *state.RepoPageState

	OnAddRepo    func(req service.AddRepoRequest)
	OnRemoveRepo func(name string)
	OnRenameRepo func(oldName, newName string)
	OnUpdateRepo func(name string)
	OnSelectRepo func(name string)

	OnOpenLocalChart          func(path string)
	OnSelectRecentChart       func(entry domain.RecentChart)
	OnRemoveRecentChart       func(idx int)
	OnOpenChartFilePicker     func()
	OnDirectLinkSubmit        func(text string)
	OnValuesFileDropped       func(path string)
	OnOpenValuesFilePicker    func()
	OnSelectRecentValuesEntry func(entry domain.RecentValuesEntry)
	OnRemoveRecentValuesEntry func(idx int)

	// confirmDialog lives on the page (not State) to avoid State importing the widget
	// package. Button pointers (ConfirmYes/No) live on State and are re-assigned on each
	// delete click; ConfirmActive and ConfirmDeleteName track dialog lifecycle in State.
	confirmDialog  customwidget.ConfirmDialog
	homeDropZone   customwidget.FileDropZone
	valuesDropZone customwidget.FileDropZone
}

//nolint:dupl // same Gio flex pattern as ChartsPage but entirely different children
func (p *ReposPage) Layout(gtx layout.Context) layout.Dimensions {
	p.State.EnsureClickables(len(p.State.Repos))
	// Guarantees RecentClicks and RecentRemoveClicks are large enough for list callbacks below.
	p.State.EnsureRecentClickables(len(p.State.RecentCharts))
	p.State.EnsurePresetClickables(len(presetRepos))
	p.State.EnsureRecentValuesClickables(len(p.State.RecentValuesEntries))

	// Clamp focus index after data changes (e.g. repo deleted while focused on last item).
	if maxIdx := p.sectionItemCount(p.State.FocusedSection) - 1; maxIdx < 0 {
		p.State.FocusedIndex = 0
	} else if p.State.FocusedIndex > maxIdx {
		p.State.FocusedIndex = maxIdx
	}

	if !p.State.ConfirmActive {
		p.handleKeyEvents(gtx)
	}

	// Check for chart drop — only the first file is used because the app opens
	// one chart at a time. Additional dropped files are intentionally ignored.
	if len(p.homeDropZone.FilePaths) > 0 {
		if p.OnOpenLocalChart != nil {
			p.OnOpenLocalChart(p.homeDropZone.FilePaths[0])
		}

		p.homeDropZone.FilePaths = nil
	}

	// Check for values drop — only the first file is used.
	if len(p.valuesDropZone.FilePaths) > 0 {
		if p.OnValuesFileDropped != nil {
			p.OnValuesFileDropped(p.valuesDropZone.FilePaths[0])
		}

		p.valuesDropZone.FilePaths = nil
	}

	// Check for chart picker button click
	if p.State.ChartPickerButton.Clicked(gtx) && p.OnOpenChartFilePicker != nil {
		p.OnOpenChartFilePicker()
	}

	// Check for values picker button click
	if p.State.ValuesPickerButton.Clicked(gtx) && p.OnOpenValuesFilePicker != nil {
		p.OnOpenValuesFilePicker()
	}

	// Handle confirm dialog
	if p.State.ConfirmActive {
		action := p.confirmDialog.Update(gtx)

		switch action {
		case customwidget.ConfirmYes:
			if p.State.ConfirmDeleteName != "" && p.OnRemoveRepo != nil {
				p.OnRemoveRepo(p.State.ConfirmDeleteName)
			}

			p.State.ConfirmActive = false
		case customwidget.ConfirmNo:
			p.State.ConfirmActive = false
		}
	}

	p.State.PageList.Axis = layout.Vertical

	sections := [...]layout.Widget{
		p.layoutChartsSection,
		p.layoutRepositoriesSection,
		p.layoutValuesSection,
	}

	// Reset each frame; the list-item callback repopulates when the Values
	// section is actually visible. Stale values would misroute native drops.
	p.State.ValuesSectionMinY = 0

	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return material.List(p.Theme, &p.State.PageList).Layout(gtx, len(sections),
				func(gtx layout.Context, index int) layout.Dimensions {
					if index == valuesSectionIdx {
						p.recordValuesSectionMinY()
					}

					dims := sections[index](gtx)
					p.State.SectionHeights[index] = dims.Size.Y

					return dims
				})
		}),
		layout.Expanded(p.layoutOverlay),
	)
}

const valuesSectionIdx = 2

// recordValuesSectionMinY computes the y-origin of the Values card in window
// coordinates from the page list's scroll position plus the heights of any
// prior sections already measured this frame. Leaves ValuesSectionMinY at 0
// (drops disabled) if the card's top is scrolled above the page content area.
func (p *ReposPage) recordValuesSectionMinY() {
	pos := p.State.PageList.Position

	y := p.State.PageContentTop - pos.Offset
	for i := pos.First; i < valuesSectionIdx; i++ {
		y += p.State.SectionHeights[i]
	}

	if y >= p.State.PageContentTop {
		p.State.ValuesSectionMinY = y
	}
}

// layoutChartsSection renders the "Charts" header, compact drop zone, direct link input, and recent chart items.
//
//nolint:dupl // same Gio flex pattern as layoutRepositoriesSection but entirely different children
func (p *ReposPage) layoutChartsSection(gtx layout.Context) layout.Dimensions {
	return layoutSectionCard(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutSectionHeaderWithHint(gtx, p.Theme, "Recent Charts", []string{"Tab"}, "to focus",
					sectionHeaderPaddingTop, sectionHeaderPaddingBottom)
			}),
			layout.Rigid(p.layoutCompactDropZone),
			layout.Rigid(p.layoutDirectLinkInput),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutCappedHeight(gtx, recentChartsMaxHeight, p.layoutRecentChartItems)
			}),
		)
	})
}

func (p *ReposPage) layoutCompactDropZone(gtx layout.Context) layout.Dimensions {
	return p.layoutDropZone(gtx, &p.homeDropZone, homeDropZoneTitle, &p.State.ChartPickerButton)
}

func (p *ReposPage) layoutValuesDropZone(gtx layout.Context) layout.Dimensions {
	return p.layoutDropZone(gtx, &p.valuesDropZone, valuesDropZoneTitle, &p.State.ValuesPickerButton)
}

func (p *ReposPage) layoutDropZone(
	gtx layout.Context,
	zone *customwidget.FileDropZone,
	title string,
	pickBtn *widget.Clickable,
) layout.Dimensions {
	return layout.Inset{Bottom: cardItemSpacing}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.Y = gtx.Dp(compactDropZoneHeight)
		gtx.Constraints.Min.Y = gtx.Constraints.Max.Y

		zone.Active = p.State.FileDropActive
		zone.HideTitle = !p.State.DropSupported
		zone.Title = title
		zone.Hint = ""
		zone.PickButton = pickBtn
		zone.ButtonLabel = browseButtonLabel

		return zone.Layout(gtx, p.Theme)
	})
}

// layoutValuesSection renders the "Values" header, values drop zone, and recent values+chart entries.
func (p *ReposPage) layoutValuesSection(gtx layout.Context) layout.Dimensions {
	return layoutSectionCard(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutSectionHeaderWithHint(gtx, p.Theme, "Values", []string{"Tab", "Tab"}, "to focus",
					sectionHeaderPaddingTop, sectionHeaderPaddingBottom)
			}),
			layout.Rigid(p.layoutValuesDropZone),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutCappedHeight(gtx, recentValuesEntriesMaxHeight, p.layoutRecentValuesEntries)
			}),
		)
	})
}

func (p *ReposPage) layoutRecentValuesEntries(gtx layout.Context) layout.Dimensions {
	if len(p.State.RecentValuesEntries) == 0 {
		return layout.Dimensions{}
	}

	p.State.RecentValuesList.Axis = layout.Vertical

	return material.List(p.Theme, &p.State.RecentValuesList).Layout(gtx, len(p.State.RecentValuesEntries),
		func(gtx layout.Context, index int) layout.Dimensions {
			entry := p.State.RecentValuesEntries[index]

			removed := p.State.RecentValuesRemoveClicks[index].Clicked(gtx)
			if removed && p.OnRemoveRecentValuesEntry != nil {
				p.OnRemoveRecentValuesEntry(index)
			}

			if p.State.RecentValuesClicks[index].Clicked(gtx) && !removed && p.OnSelectRecentValuesEntry != nil {
				p.OnSelectRecentValuesEntry(entry)
			}

			focused := p.State.FocusedSection == state.RepoSectionValues && p.State.FocusedIndex == index

			return layoutCardFocusable(gtx, &p.State.RecentValuesClicks[index], focused,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(customwidget.LabelWidget(material.Body2(p.Theme, filepath.Base(entry.ValuesPath)))),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(p.Theme, entry.ChartDisplayName)
									lbl.Color = theme.ColorSecondary

									return customwidget.LayoutLabel(gtx, lbl)
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutActionButton(gtx, p.Theme, &p.State.RecentValuesRemoveClicks[index],
								"x", theme.ColorDanger, repoPaddingSmall)
						}),
					)
				})
		})
}

func (p *ReposPage) layoutDirectLinkInput(gtx layout.Context) layout.Dimensions {
	p.State.DirectLinkEditor.Submit = true

	for {
		ev, ok := p.State.DirectLinkEditor.Update(gtx)
		if !ok {
			break
		}

		if _, isSubmit := ev.(widget.SubmitEvent); isSubmit {
			text := p.State.DirectLinkEditor.Text()
			if text != "" && p.OnDirectLinkSubmit != nil {
				p.OnDirectLinkSubmit(text)
				p.State.DirectLinkEditor.SetText("")
			}
		}
	}

	return layout.Inset{Bottom: cardItemSpacing}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layoutEditorField(gtx, p.Theme, &p.State.DirectLinkEditor, directLinkHint, 0)
	})
}

func (p *ReposPage) layoutRecentChartItems(gtx layout.Context) layout.Dimensions {
	if len(p.State.RecentCharts) == 0 {
		return layout.Dimensions{}
	}

	p.State.RecentList.Axis = layout.Vertical

	return material.List(p.Theme, &p.State.RecentList).Layout(gtx, len(p.State.RecentCharts),
		func(gtx layout.Context, index int) layout.Dimensions {
			entry := p.State.RecentCharts[index]

			// Remove is checked first so its Clicked() is consumed before the row click.
			// Safe under Gio's single-pointer model: at most one fires per frame.
			removed := p.State.RecentRemoveClicks[index].Clicked(gtx)
			if removed && p.OnRemoveRecentChart != nil {
				p.OnRemoveRecentChart(index)
			}

			if p.State.RecentClicks[index].Clicked(gtx) && !removed && p.OnSelectRecentChart != nil {
				p.OnSelectRecentChart(entry)
			}

			focused := p.State.FocusedSection == state.RepoSectionRecent && p.State.FocusedIndex == index

			return layoutCardFocusable(gtx, &p.State.RecentClicks[index], focused,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(customwidget.LabelWidget(material.Body2(p.Theme, entry.DisplayName))),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									subtitle := recentSubtitle(entry)
									lbl := material.Caption(p.Theme, subtitle)
									lbl.Color = theme.ColorSecondary

									return customwidget.LayoutLabel(gtx, lbl)
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutActionButton(gtx, p.Theme, &p.State.RecentRemoveClicks[index],
								"x", theme.ColorDanger, repoPaddingSmall)
						}),
					)
				})
		})
}

// layoutRepositoriesSection renders the "Repositories" header, add row, form, and repo list.
//
//nolint:dupl // same Gio flex pattern as layoutChartsSection but entirely different children
func (p *ReposPage) layoutRepositoriesSection(gtx layout.Context) layout.Dimensions {
	if p.State.Loading && len(p.State.Repos) == 0 {
		return layoutCenteredLoading(gtx, p.Theme)
	}

	return layoutSectionCard(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutSectionHeaderWithHint(gtx, p.Theme, "Repositories", []string{"Tab"}, "to focus",
					sectionHeaderPaddingTop, sectionHeaderPaddingBottom)
			}),
			layout.Rigid(p.layoutAddRepoRow),
			layout.Rigid(p.layoutAddForm),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutCappedHeight(gtx, repoListMaxHeight, p.layoutRepoList)
			}),
		)
	})
}

func (p *ReposPage) layoutAddRepoRow(gtx layout.Context) layout.Dimensions {
	if p.State.AddButton.Clicked(gtx) {
		p.State.AddFormVisible = !p.State.AddFormVisible
	}

	label := addRepoLabel
	if p.State.AddFormVisible {
		label = cancelRepoLabel
	}

	return layoutCardFocusable(gtx, &p.State.AddButton, false,
		func(gtx layout.Context) layout.Dimensions {
			lbl := material.Body1(p.Theme, label)
			lbl.Color = theme.ColorAccent

			return customwidget.LayoutLabel(gtx, lbl)
		})
}

func (p *ReposPage) layoutAddForm(gtx layout.Context) layout.Dimensions {
	if !p.State.AddFormVisible {
		return layout.Dimensions{}
	}

	if p.State.SubmitButton.Clicked(gtx) {
		name := strings.TrimSpace(p.State.AddNameEditor.Text())

		url := strings.TrimSpace(p.State.AddURLEditor.Text())
		if name != "" && url != "" && p.OnAddRepo != nil {
			p.OnAddRepo(service.AddRepoRequest{Name: name, URL: url})
			p.State.AddNameEditor.SetText("")
			p.State.AddURLEditor.SetText("")
			p.State.AddFormVisible = false
		}
	}

	// Check preset button clicks.
	for i := range presetRepos {
		if p.State.PresetClicks[i].Clicked(gtx) {
			p.State.AddNameEditor.SetText(presetRepos[i].Name)
			p.State.AddURLEditor.SetText(presetRepos[i].URL)
		}
	}

	return layoutStaticCard(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutEditorField(gtx, p.Theme, &p.State.AddNameEditor, "Repository Name", repoPaddingSmall)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				hint := "Repository URL (e.g. https://charts.bitnami.com/bitnami)"

				return layoutEditorField(gtx, p.Theme, &p.State.AddURLEditor, hint, repoPaddingSmall)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return p.layoutPresetButtons(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return LayoutTextButton(gtx, p.Theme, &p.State.SubmitButton, "Add", 0)
			}),
		)
	})
}

func (p *ReposPage) layoutPresetButtons(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Bottom: repoPaddingSmall}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return p.layoutFlowWrap(gtx)
	})
}

// layoutFlowWrap arranges preset chips in a horizontal flow that wraps to the next line.
func (p *ReposPage) layoutFlowWrap(gtx layout.Context) layout.Dimensions {
	maxWidth := gtx.Constraints.Max.X
	gap := gtx.Dp(presetChipGap)

	var x, y, rowHeight int

	for i := range presetRepos {
		// Measure chip size.
		m := op.Record(gtx.Ops)
		dims := layoutPresetChip(gtx, p.Theme, &p.State.PresetClicks[i], presetRepos[i].Name)
		c := m.Stop()

		// Wrap to next row if this chip exceeds the line.
		if x > 0 && x+gap+dims.Size.X > maxWidth {
			x = 0
			y += rowHeight + gap
			rowHeight = 0
		}

		if x > 0 {
			x += gap
		}

		// Position the recorded chip.
		stack := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
		c.Add(gtx.Ops)
		stack.Pop()

		x += dims.Size.X

		if dims.Size.Y > rowHeight {
			rowHeight = dims.Size.Y
		}
	}

	return layout.Dimensions{Size: image.Pt(maxWidth, y+rowHeight)}
}

func layoutPresetChip(gtx layout.Context, th *material.Theme, click *widget.Clickable, label string) layout.Dimensions {
	hovered := click.Hovered()

	lbl := material.Caption(th, label)
	lbl.Color = theme.ColorAccent

	m := op.Record(gtx.Ops)
	dims := click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{
			Left: presetChipPadH, Right: presetChipPadH,
			Top: presetChipPadV, Bottom: presetChipPadV,
		}.Layout(gtx, lbl.Layout)
	})
	c := m.Stop()

	bounds := image.Rectangle{Max: dims.Size}
	radius := gtx.Dp(presetChipRadius)
	bw := gtx.Dp(editorFieldBorderWidth)

	borderColor := theme.ColorInputBorder
	if hovered {
		borderColor = theme.ColorAccent
	}

	paintRoundedBorder(gtx, bounds, radius, bw, borderColor, theme.ColorCardBg)

	c.Add(gtx.Ops)

	pushPointerCursor(gtx, dims, click)

	return dims
}

func (p *ReposPage) layoutRepoList(gtx layout.Context) layout.Dimensions {
	p.State.RepoList.Axis = layout.Vertical

	return material.List(p.Theme, &p.State.RepoList).Layout(gtx, len(p.State.Repos),
		func(gtx layout.Context, index int) layout.Dimensions {
			repo := p.State.Repos[index]

			actionClicked := false

			if !p.State.ConfirmActive && p.State.DeleteClicks[index].Clicked(gtx) {
				actionClicked = true
				p.State.ConfirmDeleteName = repo.Name
				p.State.ConfirmActive = true
				p.confirmDialog.YesButton = &p.State.ConfirmYes
				p.confirmDialog.NoButton = &p.State.ConfirmNo
			}

			if p.State.UpdateClicks[index].Clicked(gtx) {
				actionClicked = true

				if p.OnUpdateRepo != nil {
					p.OnUpdateRepo(repo.Name)
				}
			}

			if p.State.RepoClicks[index].Clicked(gtx) && !actionClicked && p.OnSelectRepo != nil {
				p.OnSelectRepo(repo.Name)
			}

			focused := p.State.FocusedSection == state.RepoSectionRepos && p.State.FocusedIndex == index

			return layoutCardFocusable(gtx, &p.State.RepoClicks[index], focused,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						// Body1 for repos (primary items) vs Body2 for recent charts (secondary).
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(customwidget.LabelWidget(material.Body1(p.Theme, repo.Name))),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Caption(p.Theme, repo.URL)
									lbl.Color = theme.ColorSecondary

									return customwidget.LayoutLabel(gtx, lbl)
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutActionButton(gtx, p.Theme,
								&p.State.UpdateClicks[index], "Update", theme.ColorAccent, repoPaddingBottom)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutActionButton(gtx, p.Theme,
								&p.State.DeleteClicks[index], "Delete", theme.ColorDanger, repoPaddingSmall)
						}),
					)
				})
		})
}

func (p *ReposPage) layoutOverlay(gtx layout.Context) layout.Dimensions {
	if !p.State.ConfirmActive {
		return layout.Dimensions{}
	}

	return p.confirmDialog.Layout(gtx, p.Theme, "Delete repository "+p.State.ConfirmDeleteName+"?")
}

func (p *ReposPage) handleKeyEvents(gtx layout.Context) {
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, p)
	area.Pop()

	editorFocused := gtx.Focused(&p.State.DirectLinkEditor)

	filters := []event.Filter{
		key.Filter{Name: key.NameTab},
		key.Filter{Name: key.NameTab, Required: key.ModShift},
		key.Filter{Name: key.NameUpArrow},
		key.Filter{Name: key.NameDownArrow},
	}

	if !editorFocused {
		filters = append(filters,
			key.Filter{Name: key.NameReturn},
			key.Filter{Name: key.NameEnter},
		)
	}

	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}

		e, isKey := ev.(key.Event)
		if !isKey || e.State != key.Press {
			continue
		}

		switch e.Name {
		case key.NameTab:
			p.handleTab(e.Modifiers.Contain(key.ModShift))
		case key.NameUpArrow:
			p.moveFocus(-1)
		case key.NameDownArrow:
			p.moveFocus(1)
		case key.NameReturn, key.NameEnter:
			p.activateFocused()
		}
	}
}

func (p *ReposPage) handleTab(reverse bool) {
	delta := 1
	if reverse {
		delta = -1
	}

	p.cycleSection(delta)
	p.State.FocusedIndex = 0

	// Skip empty sections so Tab never lands on nothing.
	if p.sectionItemCount(p.State.FocusedSection) == 0 {
		p.skipToNonEmptySection(delta)
	}

	p.scrollFocusedIntoView()
}

// cycleSection advances FocusedSection by one step in the given direction.
func (p *ReposPage) cycleSection(delta int) {
	if delta < 0 {
		if p.State.FocusedSection == 0 {
			p.State.FocusedSection = state.RepoSectionCount - 1
		} else {
			p.State.FocusedSection--
		}
	} else {
		p.State.FocusedSection = (p.State.FocusedSection + 1) % state.RepoSectionCount
	}
}

func (p *ReposPage) moveFocus(delta int) {
	maxIdx := p.sectionItemCount(p.State.FocusedSection) - 1
	if maxIdx < 0 {
		// Current section is empty — advance to the next non-empty section.
		p.skipToNonEmptySection(delta)

		return
	}

	p.State.FocusedIndex += delta
	p.State.FocusedIndex = max(min(p.State.FocusedIndex, maxIdx), 0)

	p.scrollFocusedIntoView()
}

// scrollFocusedIntoView first makes sure the focused section's card is in the
// page viewport, then adjusts that section's inner list so the focused row is
// visible. Only scrolls when the target lies outside the currently visible
// window.
func (p *ReposPage) scrollFocusedIntoView() {
	p.scrollFocusedSectionIntoPageView()

	var list *widget.List

	switch p.State.FocusedSection {
	case state.RepoSectionRecent:
		list = &p.State.RecentList
	case state.RepoSectionRepos, state.RepoSectionAddForm:
		list = &p.State.RepoList
	case state.RepoSectionValues:
		list = &p.State.RecentValuesList
	default:
		return
	}

	idx := p.State.FocusedIndex
	pos := &list.Position
	first := pos.First
	count := pos.Count

	// Last fully visible index: Position.Count includes a partially-clipped last
	// item when OffsetLast < 0, so subtract one in that case.
	lastFull := first + count - 1
	if pos.OffsetLast < 0 && count > 0 {
		lastFull = first + count - 2 //nolint:mnd // exclude the clipped last item
	}

	// First fully visible index: Position.Offset > 0 means the first item is
	// clipped at the top, so treat first+1 as the first fully visible.
	firstFull := first
	if pos.Offset > 0 {
		firstFull = first + 1
	}

	switch {
	case idx < firstFull:
		pos.First = idx
		pos.Offset = 0
	case count > 0 && idx > lastFull:
		// Place focused item as the last fully visible row.
		pos.First = max(0, idx-(count-1)+1)
		pos.Offset = 0
	}
}

// scrollFocusedSectionIntoPageView ensures the focused section's top-level
// card is visible in the page-level scroll list. Sub-focus on the add-repo
// form maps to the Repositories card.
func (p *ReposPage) scrollFocusedSectionIntoPageView() {
	var target int

	switch p.State.FocusedSection {
	case state.RepoSectionRecent:
		target = 0
	case state.RepoSectionRepos, state.RepoSectionAddForm:
		target = 1
	case state.RepoSectionValues:
		target = valuesSectionIdx
	default:
		return
	}

	pos := &p.State.PageList.Position

	switch {
	case target < pos.First:
		pos.First = target
		pos.Offset = 0
	case target == pos.First && pos.Offset > 0:
		pos.Offset = 0
	case target >= pos.First+pos.Count:
		pos.First = target
		pos.Offset = 0
	case pos.Count > 0 && target == pos.First+pos.Count-1 && pos.OffsetLast < 0:
		// Partly clipped at the bottom — bring the card to the top.
		pos.First = target
		pos.Offset = 0
	}
}

// skipToNonEmptySection cycles through sections in the given direction
// until one with items is found, or all sections have been tried.
func (p *ReposPage) skipToNonEmptySection(delta int) {
	for range state.RepoSectionCount - 1 {
		p.cycleSection(delta)

		if p.sectionItemCount(p.State.FocusedSection) > 0 {
			p.State.FocusedIndex = 0

			return
		}
	}
}

func (p *ReposPage) sectionItemCount(section state.RepoSection) int {
	switch section {
	case state.RepoSectionRecent:
		return len(p.State.RecentCharts)
	case state.RepoSectionValues:
		return len(p.State.RecentValuesEntries)
	case state.RepoSectionRepos:
		return len(p.State.Repos)
	case state.RepoSectionAddForm:
		if p.State.AddFormVisible {
			return 1
		}

		return 0
	default:
		return 0
	}
}

func (p *ReposPage) activateFocused() {
	switch p.State.FocusedSection {
	case state.RepoSectionRecent:
		if p.State.FocusedIndex < len(p.State.RecentCharts) && p.OnSelectRecentChart != nil {
			p.OnSelectRecentChart(p.State.RecentCharts[p.State.FocusedIndex])
		}
	case state.RepoSectionValues:
		if p.State.FocusedIndex < len(p.State.RecentValuesEntries) && p.OnSelectRecentValuesEntry != nil {
			p.OnSelectRecentValuesEntry(p.State.RecentValuesEntries[p.State.FocusedIndex])
		}
	case state.RepoSectionRepos:
		if p.State.FocusedIndex < len(p.State.Repos) && p.OnSelectRepo != nil {
			p.OnSelectRepo(p.State.Repos[p.State.FocusedIndex].Name)
		}
	case state.RepoSectionAddForm:
		// No bounds check needed — this only toggles form visibility.
		p.State.AddFormVisible = !p.State.AddFormVisible
	}
}

func recentSubtitle(entry domain.RecentChart) string {
	switch {
	case entry.IsLocal():
		return "Local: " + filepath.Base(entry.LocalPath)
	case entry.IsOCI():
		return "OCI: " + entry.OciURL
	default:
		return entry.RepoName
	}
}
