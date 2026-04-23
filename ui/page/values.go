package page

import (
	"image"
	"image/color"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/state"
	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

const (
	valuesSpacing         unit.Dp = 8
	valuesPaddingSmall    unit.Dp = 4
	valuesHeaderWeight            = 700
	valuesHeaderPadV      unit.Dp = 6
	valuesHeaderMinH      unit.Dp = 36 // must accommodate LayoutTextButton height (Body2 + 2×textBtnPaddingV)
	valuesDividerWidth    unit.Dp = 2
	valuesDefaultRatio    float32 = 0.6
	valuesScrollbarWidth  unit.Dp = 10 // must match overrideScrollbarWidth in overridetable.go
	valuesSeparatorHeight unit.Dp = 1

	browseLabel    = "Browse"
	addColumnLabel = "+Values"

	renderDefaultsLabelBase          = "Render with default values" //nolint:goconst // render label base
	renderOverridesLabelBase         = "Render with all overrides"  //nolint:goconst // render label base
	renderPlayIconSize       unit.Dp = 10
	downloadIconSize         unit.Dp = 12
	editorIconSize           unit.Dp = 12
	renderIconSpacing        unit.Dp = 4
	renderBtnPaddingH        unit.Dp = 10
	renderBtnPaddingV        unit.Dp = 10

	recentDropdownMaxH       unit.Dp = 250
	recentDropdownPadH       unit.Dp = 12
	recentDropdownPadV       unit.Dp = 8
	recentDropdownRadius     unit.Dp = 6
	recentDropdownBorder     unit.Dp = 1
	dropdownOverlayAlpha             = 64
	recentBtnLabel                   = "Recent \u25be"
	recentBtnLabelCompact            = "\u25be"
	dropZoneCompactThreshold unit.Dp = 250
	headerSubDividerW        unit.Dp = 1 // matches overrideSubDividerW in overridetable.go

	recentItemPadV       unit.Dp = 2
	showCommentsSize     unit.Dp = 18
	showCommentsTextMult float32 = 0.85

	maxRecentValues = 10 // must match service/recent_service.go

	copyLabel                    = "Copy"
	helmCmdMaxWidthRatio float32 = 0.35 // fraction of row width reserved for helm command
)

// cellNavMod is the modifier for arrow-key cell navigation. On macOS we use
// Ctrl+Shift+Arrow to avoid stealing Cmd+Arrow (line/doc nav) and Option+Arrow
// (word nav) from the focused editor. On Windows/Linux we use Alt+Arrow,
// which is free inside text editors on those platforms.
//
//nolint:gochecknoglobals // platform-specific modifier resolved once at init
var cellNavMod = func() key.Modifiers {
	if customwidget.IsMac {
		return key.ModCtrl | key.ModShift
	}

	return key.ModAlt
}()

// ValuesPageCallbacks bundles every callback the ValuesPage needs from its controller.
type ValuesPageCallbacks struct {
	OnColumnFilesSelected   func(colIdx int, paths []string)
	OnOpenColumnFile        func(colIdx int)
	OnRevealFile            func(colIdx int)
	OnOpenInEditor          func(colIdx int)
	OnSaveChart             func()
	OnColumnOverrideChanged func(colIdx int, yamlText string, err error)
	OnSaveColumnValues      func(colIdx int)
	OnAddColumn             func()
	OnClearColumn           func(colIdx int)
	OnRemoveColumn          func(colIdx int)
	OnSelectRecentValues    func(path string)
	OnRemoveRecentValues    func(idx int)
	OnRenderDefaults        func()
	OnRenderOverrides       func()
	OnKeyCopied             func(key string)
	OnShowCommentsChanged   func(show bool)
}

// ValuesPage renders the unified override editor: default values on the left,
// editable override columns on the right, in a single synchronized table.
type ValuesPage struct {
	Theme *material.Theme
	State *state.ValuesPageState

	Table  customwidget.OverrideTable
	Search customwidget.SearchBar

	// Per-column drop zones (one per possible column).
	DropZones [state.MaxCustomColumns]customwidget.FileDropZone

	ValuesPageCallbacks

	// dropdownTopOffset is the Y pixel offset where the dropdown card starts.
	// Computed during layout of the stacked content.
	dropdownTopOffset int

	// trailingBtnW is the pixel width of the overlaid trailing buttons (Recent, +Values).
	// Set during header layout so drop zones on the last column can account for it.
	trailingBtnW int

	// columnEditorSlices avoids per-frame allocation when building the filter input.
	columnEditorSlices [state.MaxCustomColumns][]widget.Editor

	// recentDropdownChildren avoids per-frame allocation in recentDropdownItems.
	recentDropdownChildren [maxRecentValues]layout.FlexChild
}

func NewValuesPage(th *material.Theme, st *state.ValuesPageState, cb ValuesPageCallbacks) *ValuesPage {
	st.SearchEditor.SingleLine = true

	return &ValuesPage{
		Theme: th,
		State: st,
		Table: customwidget.OverrideTable{
			Theme:      th,
			List:       &st.OverrideList,
			HoveredRow: -1,
		},
		Search:              customwidget.SearchBar{Editor: &st.SearchEditor},
		ValuesPageCallbacks: cb,
	}
}

func (p *ValuesPage) Layout(gtx layout.Context) layout.Dimensions {
	// Check if any column drop zone received files.
	for c := range p.State.ColumnCount {
		if len(p.DropZones[c].FilePaths) > 0 {
			if p.OnColumnFilesSelected != nil {
				p.OnColumnFilesSelected(c, p.DropZones[c].FilePaths)
			}

			p.DropZones[c].FilePaths = nil
		}
	}

	// Handle Ctrl+F / Cmd+F search focus.
	if p.State.FocusSearch {
		p.State.FocusSearch = false
		gtx.Execute(key.FocusCmd{Tag: p.Search.Editor})
	}

	p.handleKeyEvents(gtx)

	if p.State.DefaultValues == nil {
		if p.State.Loading {
			return layoutCenteredLoading(gtx, p.Theme)
		}

		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return customwidget.LayoutLabel(gtx, material.Body1(p.Theme, "No chart selected"))
		})
	}

	entryCount := len(p.State.DefaultValues.Entries)

	// Ensure editors are allocated for all active columns.
	p.State.EnsureDefaultEditors(entryCount)

	for c := range p.State.ColumnCount {
		p.State.EnsureColumnEditors(c, entryCount)
	}

	// Wire the table editors from column state.
	p.Table.DefaultValueEditors = p.State.DefaultValueEditors
	p.Table.ColumnCount = p.State.ColumnCount
	p.Table.ShowComments = p.State.ShowComments.Value

	for c := range p.State.ColumnCount {
		p.Table.ColumnEditors[c] = p.State.Columns[c].OverrideEditors
		p.Table.ColumnStates[c] = &p.State.Columns[c]
	}

	// Drain pending SetText ChangeEvents from file loads before wiring OnChanged,
	// so drained events don't trigger onColumnOverrideChanged.
	for c := range p.State.ColumnCount {
		col := &p.State.Columns[c]
		if !col.DrainPendingChanges {
			continue
		}

		for i := range col.OverrideEditors {
			for {
				_, ok := col.OverrideEditors[i].Update(gtx)
				if !ok {
					break
				}
			}
		}

		col.DrainPendingChanges = false
		col.ValuesModified = false
	}

	p.Table.OnChanged = p.OnColumnOverrideChanged
	p.Table.OnKeyCopied = p.OnKeyCopied
	p.Table.OnCellFocused = func(row, col int) {
		p.State.FocusedRow = row
		p.State.FocusedCol = col
	}

	// Build columnEditors slice for search filter.
	for c := range p.State.ColumnCount {
		p.columnEditorSlices[c] = p.State.Columns[c].OverrideEditors
	}

	// Recompute filtered indices.
	p.State.FilteredIndices = p.Search.FilterEntriesWithMultiOverrides(
		p.State.DefaultValues.Entries,
		p.columnEditorSlices[:p.State.ColumnCount],
		p.State.FilteredIndices,
	)

	// Clamp focused row to stay within filtered bounds.
	if p.State.FocusedRow >= len(p.State.FilteredIndices) {
		p.State.FocusedRow = max(0, len(p.State.FilteredIndices)-1)
	}

	// Ensure recent values clickables are allocated.
	p.State.EnsureRecentValuesClickables(len(p.State.RecentValuesFiles))

	// Process dropdown events (toggle, dismiss, item selection).
	p.processDropdownEvents(gtx)

	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			var totalRigidH int

			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					dims := p.layoutParseErrors(gtx)
					totalRigidH += dims.Size.Y

					return dims
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					dims := p.layoutRenderButtons(gtx)
					totalRigidH += dims.Size.Y

					return dims
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					searchHint := customwidget.ShortcutLabel("\u2318+F", "Ctrl+F")
					dims := p.Search.Layout(gtx, p.Theme, "Search values... ("+searchHint+")")
					totalRigidH += dims.Size.Y

					return dims
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					dims := p.layoutColumnHeaders(gtx)
					totalRigidH += dims.Size.Y
					p.dropdownTopOffset = totalRigidH

					return dims
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return p.Table.Layout(gtx,
						p.State.DefaultValues.Entries,
						p.State.FilteredIndices,
					)
				}),
			)
		}),
		layout.Expanded(p.layoutRecentDropdownOverlay),
	)
}

// processDropdownEvents handles all recent dropdown click events and the
// +Values button in one place to avoid double-consuming Clicked() events.
// In particular, AddColumnButton.Clicked must be consumed here (not during
// layout) because layoutTrailingButtons uses op.Record to probe button width,
// which would consume the click event in the discarded measurement pass.
func (p *ValuesPage) processDropdownEvents(gtx layout.Context) {
	// Toggle dropdown open/closed.
	if p.State.RecentDropdownToggle.Clicked(gtx) {
		p.State.RecentDropdownOpen = !p.State.RecentDropdownOpen
	}

	// Dismiss on overlay click.
	if p.State.RecentDropdownDismiss.Clicked(gtx) {
		p.State.RecentDropdownOpen = false
	}

	// +Values column button.
	if p.State.AddColumnButton.Clicked(gtx) && p.OnAddColumn != nil {
		p.OnAddColumn()
	}

	// Recent item clicks.
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

// layoutParseErrors renders parse errors from all active columns.
func (p *ValuesPage) layoutParseErrors(gtx layout.Context) layout.Dimensions {
	var children [state.MaxCustomColumns]layout.FlexChild

	n := 0

	for c := range p.State.ColumnCount {
		errMsg := p.State.Columns[c].EditorParseError
		if errMsg == "" {
			continue
		}

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: valuesSpacing, Bottom: valuesPaddingSmall}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					lbl := material.Caption(p.Theme, errMsg)
					lbl.Color = theme.ColorError

					return customwidget.LayoutLabel(gtx, lbl)
				})
		})
		n++
	}

	if n == 0 {
		return layout.Dimensions{}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children[:n]...)
}

func (p *ValuesPage) layoutColumnHeaders(gtx layout.Context) layout.Dimensions {
	ratio := p.Table.ColumnRatio
	if ratio <= 0 {
		ratio = valuesDefaultRatio
	}

	// material.List subtracts the scrollbar width from child constraints,
	// so the table's totalW = viewport - scrollbar. Match that here.
	viewportW := gtx.Constraints.Max.X
	scrollW := gtx.Dp(valuesScrollbarWidth)
	totalW := viewportW - scrollW
	divW := gtx.Dp(valuesDividerWidth)
	leftW := int(ratio * float32(totalW))
	rightW := max(totalW-leftW-divW, 0)

	// 1. Measure trailing buttons (Recent ▾, +Values) via op.Record so we know their width.
	// perColW approximates the last override column's width; used to decide compact labels.
	perColW := rightW / max(p.State.ColumnCount, 1)
	btnMacro := op.Record(gtx.Ops)
	btnDims := p.layoutTrailingButtons(gtx, perColW)
	btnCall := btnMacro.Stop()
	p.trailingBtnW = btnDims.Size.X

	// 2. Record the main header row to measure its height.
	m := op.Record(gtx.Ops)

	dims := layout.Inset{Top: valuesHeaderPadV, Bottom: valuesHeaderPadV}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			// Cap Max.Y so the header height is stable across empty/loaded states.
			contentH := gtx.Dp(valuesHeaderMinH)
			gtx.Constraints.Max.Y = contentH

			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				// Zero-width spacer that sets the flex cross-axis height to contentH.
				// Children keep crossMin=0, so they return natural heights and
				// get properly centered by layout.Middle.
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(0, contentH)}
				}),
				// Left: Key + Default Value with explicit pixel width matching the table.
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = leftW
					gtx.Constraints.Max.X = leftW

					return layout.Inset{Left: valuesSpacing, Right: valuesSpacing}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions { //nolint:mnd // key proportion
									lbl := material.Body2(p.Theme, "Key")
									lbl.Font.Weight = valuesHeaderWeight

									return customwidget.LayoutLabel(gtx, lbl)
								}),
								layout.Flexed(0.5, func(gtx layout.Context) layout.Dimensions { //nolint:mnd // value proportion
									lbl := material.Body2(p.Theme, "Default Value")
									lbl.Font.Weight = valuesHeaderWeight
									lbl.Alignment = text.End

									return customwidget.LayoutLabel(gtx, lbl)
								}),
							)
						})
				}),
				// Divider spacer (matches table's rigid divider width).
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(divW, 0)}
				}),
				// Right: per-column file statuses only (no buttons — they are overlaid).
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return p.layoutColumnFileStatuses(gtx)
				}),
			)
		})

	c := m.Stop()
	c.Add(gtx.Ops)

	// 3. Overlay trailing buttons at the right edge, aligned to content area.
	if btnDims.Size.X > 0 {
		padV := gtx.Dp(valuesHeaderPadV)
		contentH := dims.Size.Y - 2*padV //nolint:mnd // top + bottom padding
		btnX := viewportW - btnDims.Size.X
		btnY := padV + (contentH-btnDims.Size.Y)/2 //nolint:mnd // center in content area

		btnOff := op.Offset(image.Pt(btnX, btnY)).Push(gtx.Ops)
		btnCall.Add(gtx.Ops)
		btnOff.Pop()
	}

	// 4. Draw divider lines at table-aligned positions.
	sepH := gtx.Dp(valuesSeparatorHeight)

	// Vertical divider between left and right panels.
	vDiv := clip.Rect{
		Min: image.Pt(leftW, 0),
		Max: image.Pt(leftW+divW, dims.Size.Y),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	vDiv.Pop()

	// Sub-column dividers between override columns in the header.
	colCount := p.State.ColumnCount
	if colCount > 1 {
		subDivW := gtx.Dp(headerSubDividerW)
		subTotalDivW := subDivW * (colCount - 1)
		subColW := (rightW - subTotalDivW) / colCount
		rightStart := leftW + divW

		for i := 1; i < colCount; i++ {
			x := rightStart + i*subColW + (i-1)*subDivW

			subDiv := clip.Rect{
				Min: image.Pt(x, 0),
				Max: image.Pt(x+subDivW, dims.Size.Y),
			}.Push(gtx.Ops)
			paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			subDiv.Pop()
		}
	}

	// Horizontal line above header.
	topLine := clip.Rect{
		Max: image.Pt(viewportW, sepH),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	topLine.Pop()

	// Horizontal line below header.
	botLine := clip.Rect{
		Min: image.Pt(0, dims.Size.Y-sepH),
		Max: image.Pt(viewportW, dims.Size.Y),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	botLine.Pop()

	return dims
}

// layoutColumnFileStatuses renders per-column file statuses (no trailing buttons).
// Each column gets equal Flexed(1) weight with sub-divider spacers between them.
func (p *ValuesPage) layoutColumnFileStatuses(gtx layout.Context) layout.Dimensions {
	colCount := p.State.ColumnCount

	var children [state.MaxCustomColumns*2 - 1]layout.FlexChild //nolint:mnd // cols + dividers

	n := 0

	for c := range colCount {
		col := c

		children[n] = layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return p.layoutSingleColumnStatus(gtx, col)
		})
		n++

		if c < colCount-1 {
			subDivW := gtx.Dp(headerSubDividerW)

			children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(subDivW, 0)}
			})
			n++
		}
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children[:n]...)
}

// layoutTrailingButtons renders the "Recent ▾" and "+Values" buttons.
// Called via op.Record to measure width, then overlaid at the right edge.
// perColW is the pixel width of the last override column; when the buttons
// would consume more than half of it, "Recent ▾" collapses to just "▾".
func (p *ValuesPage) layoutTrailingButtons(gtx layout.Context, perColW int) layout.Dimensions {
	showRecent := len(p.State.RecentValuesFiles) > 0
	showAddCol := p.State.CanAddColumn()

	if !showRecent && !showAddCol {
		return layout.Dimensions{}
	}

	recentLabel := recentBtnLabel

	// Measure with full label first; if it exceeds half the last column, use compact.
	if showRecent && perColW > 0 {
		probe := op.Record(gtx.Ops)
		fullW := p.layoutTrailingButtonsInner(gtx, recentLabel, showRecent, showAddCol)

		probe.Stop()

		if fullW.Size.X > perColW/3 { //nolint:mnd // leave room for Browse button
			recentLabel = recentBtnLabelCompact
		}
	}

	return p.layoutTrailingButtonsInner(gtx, recentLabel, showRecent, showAddCol)
}

func (p *ValuesPage) layoutTrailingButtonsInner(
	gtx layout.Context, recentLabel string, showRecent, showAddCol bool,
) layout.Dimensions {
	var children [2]layout.FlexChild //nolint:mnd // recent + addcol

	n := 0

	if showRecent {
		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutTextButton(gtx, p.Theme, &p.State.RecentDropdownToggle, recentLabel, valuesPaddingSmall)
		})
		n++
	}

	if showAddCol {
		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutTextButton(gtx, p.Theme, &p.State.AddColumnButton, addColumnLabel, valuesPaddingSmall)
		})
		n++
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children[:n]...)
}

// layoutSingleColumnStatus renders the file status for one column:
// empty → drop zone / browse, loaded → filename + save + x.
func (p *ValuesPage) layoutSingleColumnStatus(gtx layout.Context, colIdx int) layout.Dimensions {
	col := &p.State.Columns[colIdx]
	hasFile := len(col.CustomFilePaths) > 0
	hasOverrides := hasFile || col.HasOverrides()

	var inner func(layout.Context) layout.Dimensions
	if !hasOverrides {
		inner = func(gtx layout.Context) layout.Dimensions {
			return p.layoutColumnDropZone(gtx, colIdx)
		}
	} else {
		inner = func(gtx layout.Context) layout.Dimensions {
			return p.layoutColumnFileStatus(gtx, colIdx)
		}
	}

	// Columns after the first get an extra remove-column button.
	if colIdx == 0 {
		return inner(gtx)
	}

	idx := colIdx

	if col.RemoveColumnButton.Clicked(gtx) && p.OnRemoveColumn != nil {
		p.OnRemoveColumn(idx)
		gtx.Execute(op.InvalidateCmd{})
	}

	// Reserve space: trailing buttons (Recent, +Values) on the last column,
	// or a small gap before the sub-divider on middle columns.
	var trailingPad int
	if colIdx == p.State.ColumnCount-1 {
		trailingPad = p.trailingBtnW
	} else {
		trailingPad = gtx.Dp(valuesSpacing)
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, inner),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutTextButton(gtx, p.Theme, &col.RemoveColumnButton, "x", valuesPaddingSmall)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Dimensions{Size: image.Pt(trailingPad, 0)}
		}),
	)
}

// layoutColumnFileStatus renders the filename, close, and save buttons for a loaded column.
func (p *ValuesPage) layoutColumnFileStatus(gtx layout.Context, colIdx int) layout.Dimensions {
	col := &p.State.Columns[colIdx]
	hasFile := len(col.CustomFilePaths) > 0

	if col.SaveValuesButton.Clicked(gtx) && p.OnSaveColumnValues != nil {
		p.OnSaveColumnValues(colIdx)
	}

	label := "New Values File"
	if hasFile {
		label = filepath.Base(col.CustomFilePaths[0])

		if len(col.CustomFilePaths) > 1 {
			label += " (+" + strconv.Itoa(len(col.CustomFilePaths)-1) + " merged)"
		}
	}

	if col.ValuesModified {
		label += "*"
	}

	idx := colIdx

	// Handle filename click → reveal in Finder.
	if col.FileNameButton.Clicked(gtx) && hasFile && p.OnRevealFile != nil {
		p.OnRevealFile(idx)
	}

	// Handle open-in-editor click.
	if col.OpenInEditorButton.Clicked(gtx) && hasFile && p.OnOpenInEditor != nil {
		p.OnOpenInEditor(idx)
	}

	const maxChildren = 4

	var (
		children [maxChildren]layout.FlexChild
		n        int
	)

	children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: valuesSpacing}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				if !hasFile {
					lbl := material.Body2(p.Theme, label)
					lbl.Font.Weight = valuesHeaderWeight
					lbl.MaxLines = 1

					return customwidget.LayoutLabel(gtx, lbl)
				}

				return layoutClickablePointer(gtx, &col.FileNameButton,
					func(gtx layout.Context) layout.Dimensions {
						lbl := material.Body2(p.Theme, label)
						lbl.Font.Weight = valuesHeaderWeight
						lbl.MaxLines = 1

						return customwidget.LayoutLabel(gtx, lbl)
					})
			})
	})
	n++

	// Open in VS Code button (only when a file is loaded).
	if hasFile {
		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: valuesPaddingSmall}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					iconColor := theme.ColorAccent
					if col.OpenInEditorButton.Hovered() {
						iconColor = theme.ColorAccentHover
					}

					return layoutIconButton(gtx, p.Theme, &col.OpenInEditorButton,
						func(gtx layout.Context) layout.Dimensions {
							return customwidget.LayoutVSCodeIcon(gtx, editorIconSize, iconColor)
						})
				})
		})
		n++
	}

	// Close button right next to filename: always clears the file/overrides.
	children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
		if col.CloseButton.Clicked(gtx) && p.OnClearColumn != nil {
			p.OnClearColumn(idx)

			// Force re-render: column state changed mid-frame after Table was wired.
			gtx.Execute(op.InvalidateCmd{})
		}

		return LayoutTextButton(gtx, p.Theme, &col.CloseButton, "x", valuesPaddingSmall)
	})
	n++

	if col.ValuesModified {
		saveHint := customwidget.ShortcutLabel("\u2318+S", "Ctrl+S")

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return LayoutTextButton(gtx, p.Theme, &col.SaveValuesButton, "Save ("+saveHint+")", valuesPaddingSmall)
		})
		n++
	}

	return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children[:n]...)
}

// layoutColumnDropZone renders a compact drop zone / browse button for a single column.
func (p *ValuesPage) layoutColumnDropZone(gtx layout.Context, colIdx int) layout.Dimensions {
	col := &p.State.Columns[colIdx]

	if col.PickFileButton.Clicked(gtx) && p.OnOpenColumnFile != nil {
		p.OnOpenColumnFile(colIdx)
	}

	dz := &p.DropZones[colIdx]
	dz.Active = col.FileDropActive

	// Hide the "Drop values file" title when the column is too narrow,
	// accounting for trailing buttons (Recent, +Values) overlaid on the last column.
	effectiveW := gtx.Constraints.Max.X
	if colIdx == p.State.ColumnCount-1 {
		effectiveW -= p.trailingBtnW
	}

	dz.HideTitle = !p.State.DropSupported || effectiveW < gtx.Dp(dropZoneCompactThreshold)
	dz.PickButton = &col.PickFileButton
	dz.ButtonLabel = browseLabel
	dz.AlignLeft = true
	dz.Compact = true

	return dz.Layout(gtx, p.Theme)
}

// overrideColumnOffset returns the pixel offset and width of the override (right) column.
// Subtracts scrollbar width to match layoutColumnHeaders alignment.
func (p *ValuesPage) overrideColumnOffset(gtx layout.Context) (leftPx int, rightPx int) {
	ratio := p.Table.ColumnRatio
	if ratio <= 0 {
		ratio = valuesDefaultRatio
	}

	totalW := gtx.Constraints.Max.X - gtx.Dp(valuesScrollbarWidth)
	divW := gtx.Dp(valuesDividerWidth)
	left := int(ratio * float32(totalW))
	right := max(totalW-left-divW, 0)

	return left + divW, right
}

// Recent Values Dropdown Overlay

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

// layoutRecentDropdownCard renders the white dropdown card with the list of recent files.
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

	for i := range recentCount {
		idx := i

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

// layoutRenderButtons renders the template rendering action buttons.
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
		defaultsHint := customwidget.ShortcutLabel("\u2318+1", "F3")
		overridesHint := customwidget.ShortcutLabel("\u2318+2", "F4")

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

func (p *ValuesPage) handleKeyEvents(gtx layout.Context) {
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, p)
	area.Pop()

	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameUpArrow, Required: cellNavMod},
			key.Filter{Name: key.NameDownArrow, Required: cellNavMod},
			key.Filter{Name: key.NameLeftArrow, Required: cellNavMod},
			key.Filter{Name: key.NameRightArrow, Required: cellNavMod},
			key.Filter{Name: key.NameTab},
			key.Filter{Name: key.NameTab, Required: key.ModShift},
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
			p.moveCellFocusRow(gtx, -1)
		case key.NameDownArrow:
			p.moveCellFocusRow(gtx, 1)
		case key.NameLeftArrow:
			p.moveCellFocusCol(gtx, -1)
		case key.NameRightArrow:
			p.moveCellFocusCol(gtx, 1)
		case key.NameTab:
			if e.Modifiers.Contain(key.ModShift) {
				p.outdentFocusedEditor(gtx)
			} else {
				p.indentFocusedEditor(gtx)
			}
		}
	}
}

// moveCellFocusRow moves keyboard focus to the next/previous non-section
// editor cell in the same column.
func (p *ValuesPage) moveCellFocusRow(gtx layout.Context, delta int) {
	if p.State.DefaultValues == nil || len(p.State.FilteredIndices) == 0 {
		return
	}

	entries := p.State.DefaultValues.Entries
	filtered := p.State.FilteredIndices
	row := p.State.FocusedRow
	col := p.State.FocusedCol

	for {
		row += delta
		if row < 0 || row >= len(filtered) {
			return
		}

		entryIdx := filtered[row]
		if entryIdx < len(entries) && !entries[entryIdx].IsSection() {
			break
		}
	}

	p.State.FocusedRow = row
	p.State.FocusedCol = col

	entryIdx := filtered[row]

	if col >= 0 && col < p.State.ColumnCount {
		editors := p.State.Columns[col].OverrideEditors
		if entryIdx < len(editors) {
			gtx.Execute(key.FocusCmd{Tag: &editors[entryIdx]})
		}
	}

	p.State.OverrideList.Position.First = max(0, row-2) //nolint:mnd // show 2 rows above
}

// moveCellFocusCol moves keyboard focus to the next/previous column on the
// same row, wrapping to the prev/next non-section row at the table edges.
func (p *ValuesPage) moveCellFocusCol(gtx layout.Context, delta int) {
	if p.State.DefaultValues == nil || len(p.State.FilteredIndices) == 0 || p.State.ColumnCount == 0 {
		return
	}

	entries := p.State.DefaultValues.Entries
	filtered := p.State.FilteredIndices
	row := p.State.FocusedRow

	if row < 0 || row >= len(filtered) {
		return
	}

	if idx := filtered[row]; idx >= len(entries) || entries[idx].IsSection() {
		return
	}

	col := p.State.FocusedCol + delta

	for col < 0 || col >= p.State.ColumnCount {
		if col < 0 {
			for {
				row--
				if row < 0 {
					return
				}

				idx := filtered[row]
				if idx < len(entries) && !entries[idx].IsSection() {
					break
				}
			}

			col = p.State.ColumnCount - 1
		} else {
			for {
				row++
				if row >= len(filtered) {
					return
				}

				idx := filtered[row]
				if idx < len(entries) && !entries[idx].IsSection() {
					break
				}
			}

			col = 0
		}
	}

	p.State.FocusedRow = row
	p.State.FocusedCol = col

	entryIdx := filtered[row]
	editors := p.State.Columns[col].OverrideEditors

	if entryIdx < len(editors) {
		gtx.Execute(key.FocusCmd{Tag: &editors[entryIdx]})
	}

	p.State.OverrideList.Position.First = max(0, row-2) //nolint:mnd // show 2 rows above
}

// focusedEditor returns the override editor at (FocusedRow, FocusedCol) along
// with that column's YAML indent width, but only if that editor currently has
// keyboard focus. Returns (nil, 0) otherwise so Tab in the search bar or
// elsewhere is a no-op.
func (p *ValuesPage) focusedEditor(gtx layout.Context) (*widget.Editor, int) {
	if p.State.DefaultValues == nil || len(p.State.FilteredIndices) == 0 {
		return nil, 0
	}

	row := p.State.FocusedRow
	col := p.State.FocusedCol

	if row < 0 || row >= len(p.State.FilteredIndices) {
		return nil, 0
	}

	if col < 0 || col >= p.State.ColumnCount {
		return nil, 0
	}

	entryIdx := p.State.FilteredIndices[row]
	editors := p.State.Columns[col].OverrideEditors

	if entryIdx >= len(editors) {
		return nil, 0
	}

	ed := &editors[entryIdx]
	if !gtx.Focused(ed) {
		return nil, 0
	}

	return ed, p.State.Columns[col].YAMLIndent()
}

// indentFocusedEditor inserts N spaces at the cursor of the focused cell
// editor, where N is the column's detected YAML indent.
func (p *ValuesPage) indentFocusedEditor(gtx layout.Context) {
	ed, n := p.focusedEditor(gtx)
	if ed == nil || n <= 0 {
		return
	}

	ed.Insert(strings.Repeat(" ", n))
}

// outdentFocusedEditor removes up to N leading spaces from the line containing
// the cursor in the focused cell editor, preserving the caret's offset within
// the line (and the selection if any).
func (p *ValuesPage) outdentFocusedEditor(gtx layout.Context) {
	ed, n := p.focusedEditor(gtx)
	if ed == nil || n <= 0 {
		return
	}

	text := ed.Text()
	start, end := ed.Selection()

	// Convert rune offset to byte offset (mirrors autoIndentAfterNewline).
	byteOff := 0
	runeIdx := 0

	for byteOff < len(text) && runeIdx < start {
		_, sz := utf8.DecodeRuneInString(text[byteOff:])
		byteOff += sz
		runeIdx++
	}

	lineStart := 0

	for i := byteOff - 1; i >= 0; i-- {
		if text[i] == '\n' {
			lineStart = i + 1

			break
		}
	}

	remove := 0
	for remove < n && lineStart+remove < len(text) && text[lineStart+remove] == ' ' {
		remove++
	}

	if remove == 0 {
		return
	}

	runeLineStart := utf8.RuneCountInString(text[:lineStart])

	ed.SetCaret(runeLineStart, runeLineStart+remove)
	ed.Insert("")

	// Shift the original caret/anchor past the removed run: positions inside
	// the removed spaces snap to the new line start; positions after shift
	// left by `remove`; positions before the line are untouched.
	adjust := func(pos int) int {
		switch {
		case pos < runeLineStart:
			return pos
		case pos <= runeLineStart+remove:
			return runeLineStart
		default:
			return pos - remove
		}
	}

	ed.SetCaret(adjust(start), adjust(end))
}
