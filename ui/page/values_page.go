package page

import (
	"image"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/platform"
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

	// keyboardMenu* are approximation constants used by
	// approxFocusedCellOnScreen when Cmd+Shift+M opens the anchor context
	// menu via keyboard. Row height is a guess because widget.List doesn't
	// report per-row pixel positions; the menu widget clamps on its own if
	// the approximation lands near the viewport edge.
	keyboardMenuRowHeight    unit.Dp = 26
	keyboardMenuColumnInset  unit.Dp = 20
	keyboardMenuDefaultRatio float32 = 0.6

	renderDefaultsLabelBase          = "Render with default values" //nolint:goconst // render label base
	renderOverridesLabelBase         = "Render with all overrides"  //nolint:goconst // render label base
	downloadIconSize         unit.Dp = 12
	editorIconSize           unit.Dp = 12
	toolbarBtnGap            unit.Dp = 8 // gap between adjacent toolbar buttons

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

	// stickyParentStripH is the fixed height of the parent-path strip that
	// sits between the column headers and the table. It shows the parent
	// key path of the first visible row — the same context the previous
	// in-list sticky header used to convey, but rendered in the page
	// header area so scrolling never resizes the list.
	stickyParentStripH    unit.Dp = 24
	stickyParentStripPadV unit.Dp = 4
	stickyDiagGap         unit.Dp = 14 // total slot width for the divider drawn between diagnostic chips
	stickyDiagDividerH    unit.Dp = 12 // height of the hairline divider drawn inside that slot
	stickyDiagDividerW    unit.Dp = 1  // hairline width

	// stickyDiagMaxChips is the upper bound on chips the diagnostics row can
	// render at once: 2 fixed (override count, extra count) plus one
	// per-column encoding label. The pre-allocated FlexChild array is sized
	// for these chips with a divider between every adjacent pair.
	stickyDiagMaxChips = 2 + state.MaxCustomColumns

	recentItemPadV   unit.Dp = 2
	showDocsSize     unit.Dp = 18
	showDocsTextMult float32 = 0.85

	maxRecentValues = 10 // must match service/recent_service.go

	copyLabel                    = "Copy"
	helmCmdMaxWidthRatio float32 = 0.35 // fraction of row width reserved for helm command

	// scrollContextRows is how many rows of context to show above a
	// scrolled-into-view target row (cell navigation, restored focus).
	scrollContextRows = 2

	// focusHighlightMaxAttempts caps the per-frame retry budget when
	// PendingFocusHighlight cannot land focus. Each frame burns one attempt,
	// so 60 covers ~1 second at 60 FPS — long enough for a list scroll to
	// register the editor's tag, short enough that a row that never accepts
	// focus (collapsed ancestor, stale entry index) gives up cleanly instead
	// of spinning forever.
	focusHighlightMaxAttempts = 60

	// Hotkey + color-legend hint shown in the notification bar's idle slot.
	helpLegendItemGap unit.Dp = 10
	helpGlyphTextGap  unit.Dp = 3
)

// cellNavMod is the modifier for arrow-key cell navigation. On macOS we use
// Ctrl+Shift+Arrow to avoid stealing Cmd+Arrow (line/doc nav) and Option+Arrow
// (word nav) from the focused editor. On Windows/Linux we use Alt+Arrow,
// which is free inside text editors on those platforms.
//
//nolint:gochecknoglobals // platform-specific modifier resolved once at init
var cellNavMod = func() key.Modifiers {
	if platform.IsMac {
		return key.ModCtrl | key.ModShift
	}

	return key.ModAlt
}()

// helpShortcutLine renders the hotkey help using native glyphs per platform:
// Mac gets modifier/tab symbols, Windows/Linux gets spelled-out names.
//
//nolint:gochecknoglobals // platform-specific hint resolved once at init
var helpShortcutLine = platform.ShortcutLabel(
	"Ctrl+Shift+Arrows nav \u00b7 Tab/Shift+Tab indent \u00b7 \u2318+/ fold \u00b7 Ctrl+Shift+M anchor menu \u00b7 ",
	"Alt+Arrows nav \u00b7 Tab/Shift+Tab indent \u00b7 Ctrl+/ fold \u00b7 Alt+M anchor menu \u00b7 ",
)

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
	OnShowDocsChanged       func(show bool)
	// OnCellFocusChanged fires when (FocusedRow, FocusedCol) actually change.
	// entryKey is the flat key of the focused entry (stable across sessions
	// and filter changes); empty when no entry is resolvable at row.
	OnCellFocusChanged func(entryKey string, col int)

	// OnCollapseChanged fires whenever State.CollapsedKeys is mutated (via
	// chevron click or auto-expand during search). The controller persists
	// the updated set as part of ChartUIState.
	OnCollapseChanged func()

	// OnAnchorCreate sets anchorName as a YAML anchor on the cell at flatKey
	// inside override column colIdx. Controller mutates the column's NodeTree
	// and marks values modified. Called after the user confirms the dialog.
	OnAnchorCreate func(colIdx int, flatKey, anchorName string)

	// OnAnchorAlias replaces the cell at flatKey with an alias to an existing
	// anchor named anchorName within the same file.
	OnAnchorAlias func(colIdx int, flatKey, anchorName string)

	// OnUnlockCell removes whichever anchor or alias annotation lives on the
	// cell at flatKey so the user can edit it. Fired after the user confirms
	// the unlock dialog shown when a typing key arrives at a locked cell.
	OnUnlockCell func(colIdx int, flatKey string)

	// OnAnchorRename changes the name of an existing anchor and every alias
	// referencing it inside the column's file. Fired after the user submits
	// the rename dialog (AnchorOpRename mode).
	OnAnchorRename func(colIdx int, oldName, newName string)

	// OnAnchorDelete removes an anchor and severs every alias that points at
	// it, replacing aliases with literal copies of the anchored value so no
	// data is lost.
	OnAnchorDelete func(colIdx int, anchorName string)
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

	// lastFocusedRow / lastFocusedCol cache the most recently observed focus
	// so OnCellFocusChanged fires only on actual changes, not every frame.
	// Seeded by NewValuesPage from the existing state to avoid spurious fires.
	lastFocusedRow int
	lastFocusedCol int

	// AnchorDialog and AnchorMenu are the widget instances driving the
	// anchor/alias mutation UI. They live on the page (not state) so state
	// types don't need to import ui/widget. The page's Layout wires them to
	// AnchorOp/AnchorMenuOpen fields in state.
	AnchorDialog customwidget.AnchorDialog
	AnchorMenu   customwidget.AnchorContextMenu

	// UnlockDialog warns when the user tries to edit a locked (anchored)
	// cell: confirming removes the anchor so typing can proceed.
	UnlockDialog    customwidget.ConfirmDialog
	unlockDialogYes widget.Clickable
	unlockDialogNo  widget.Clickable

	// DeleteAnchorDialog confirms a destructive anchor removal including the
	// severance of all aliases pointing at it.
	DeleteAnchorDialog customwidget.ConfirmDialog
	deleteAnchorYes    widget.Clickable
	deleteAnchorNo     widget.Clickable

	// lastPointerPos caches the most recent pointer position in page-local
	// coordinates, updated each frame from pointer.Move/Press events. Used
	// to place the context menu where the user actually right-clicked;
	// cell-local coords emitted by the table widget would require composing
	// every ancestor offset to recover page coords.
	lastPointerPos image.Point
}

func NewValuesPage(th *material.Theme, st *state.ValuesPageState, cb ValuesPageCallbacks) *ValuesPage {
	st.SearchEditor.SingleLine = true

	p := &ValuesPage{
		Theme: th,
		State: st,
		Table: customwidget.OverrideTable{
			Theme:      th,
			List:       &st.OverrideList,
			HoveredRow: -1,
		},
		Search:              customwidget.SearchBar{Editor: &st.SearchEditor},
		ValuesPageCallbacks: cb,
		lastFocusedRow:      st.FocusedRow,
		lastFocusedCol:      st.FocusedCol,
	}
	p.UnlockDialog = customwidget.ConfirmDialog{
		YesButton: &p.unlockDialogYes,
		NoButton:  &p.unlockDialogNo,
	}
	p.DeleteAnchorDialog = customwidget.ConfirmDialog{
		YesButton: &p.deleteAnchorYes,
		NoButton:  &p.deleteAnchorNo,
	}

	return p
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
		// No chart loaded: keep lastFocused synced to state so the next chart
		// load starts from a clean baseline. Without this, ResetState's zeroed
		// focus would look like a "change" vs. the previous chart's cached
		// values and fire a spurious OnCellFocusChanged on the first frame.
		p.lastFocusedRow = p.State.FocusedRow
		p.lastFocusedCol = p.State.FocusedCol

		if p.State.Loading {
			return layoutCenteredLoading(gtx, p.Theme)
		}

		if p.State.LoadError != "" {
			return layoutCenteredError(gtx, p.Theme, "Failed to load chart", p.State.LoadError)
		}

		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return customwidget.LayoutLabel(gtx, material.Body1(p.Theme, "No chart selected"))
		})
	}

	entryCount := len(p.State.Entries)

	// Ensure editors are allocated for all active columns.
	p.State.EnsureDefaultEditors(entryCount)

	for c := range p.State.ColumnCount {
		p.State.EnsureColumnEditors(c, entryCount)
	}

	// Wire the table editors from column state.
	p.Table.DefaultValueEditors = p.State.DefaultValueEditors
	p.Table.ColumnCount = p.State.ColumnCount
	p.Table.ShowDocs = p.State.ShowDocs.Value

	if p.State.DefaultValues != nil {
		p.Table.DefaultAnchors = p.State.DefaultValues.Anchors
	} else {
		p.Table.DefaultAnchors = nil
	}

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
	p.Table.OnCommentChanged = p.onCommentChanged
	p.Table.OnKeyCopied = p.OnKeyCopied
	p.Table.OnCellFocused = func(row, col int) {
		p.State.FocusedRow = row
		p.State.FocusedCol = col
	}
	p.Table.CollapsedKeys = p.State.CollapsedKeys
	p.Table.OnCollapseToggle = p.onCollapseToggle
	p.Table.OnJumpToFlatKey = p.jumpToFlatKey
	p.Table.OnCellContextMenu = p.openAnchorMenu
	p.Table.OnAnchorBadgeClicked = p.openAliasesOfDialog
	p.Table.OnAnchoredCellEdit = p.openUnlockDialog
	p.Table.SearchQuery = p.Search.Editor.Text()

	// Build columnEditors slice for search filter.
	for c := range p.State.ColumnCount {
		p.columnEditorSlices[c] = p.State.Columns[c].OverrideEditors
	}

	// Recompute filtered indices.
	p.State.FilteredIndices = p.Search.FilterEntriesWithMultiOverrides(
		p.State.Entries,
		p.columnEditorSlices[:p.State.ColumnCount],
		p.State.ExtrasOnly,
		p.State.FilteredIndices,
	)

	// Snapshot the user's collapsed set when search becomes active, so that
	// any search-induced auto-uncollapse below can be reverted when the user
	// clears the search. Restore on the reverse transition.
	inSearch := p.Search.Editor.Text() != ""
	wasActive := p.State.SearchCollapseActive
	p.syncSearchCollapseSnapshot(inSearch)

	// Reset scroll to the top on the empty→non-empty search transition so the
	// first match is visible instead of hidden below the prior scroll offset.
	if !wasActive && p.State.SearchCollapseActive {
		p.State.OverrideList.Position.First = 0
		p.State.OverrideList.Position.Offset = 0
	}

	// Auto-expand any collapsed ancestors of search matches so results are
	// never hidden. Mutates CollapsedKeys only — CollapsedPreSearch retains
	// the user's intent and is what the controller persists while
	// SearchCollapseActive is true, so nothing is lost on search clear.
	if inSearch && len(p.State.CollapsedKeys) > 0 {
		service.UncollapseMatchAncestors(
			p.State.Entries,
			p.State.FilteredIndices,
			p.State.CollapsedKeys,
		)
	}

	// Hide entries inside collapsed sections. Skipped during an active search
	// so matches inside (previously) collapsed sections stay visible.
	if !inSearch && len(p.State.CollapsedKeys) > 0 {
		p.State.FilteredIndices = service.ApplyCollapseFilter(
			p.State.Entries,
			p.State.FilteredIndices,
			p.State.CollapsedKeys,
			p.State.FilteredIndices,
		)
	}

	// Resolve a pending restored focus key to a filtered row. If the key is no
	// longer visible (filtered out or removed from the chart), drop the
	// pending key and fall through to the clamps below. Tracked so that the
	// section-advance logic below can tell an explicit user jump (respect the
	// chosen row, even if it's a section) apart from a first-load default
	// focus landing on an expanded section (advance to a leaf).
	resolvedExplicitFocus := false

	if p.State.PendingFocusKey != "" {
		entries := p.State.Entries
		for r, idx := range p.State.FilteredIndices {
			if idx < len(entries) && entries[idx].Key == p.State.PendingFocusKey {
				p.State.FocusedRow = r
				resolvedExplicitFocus = true

				break
			}
		}

		p.State.PendingFocusKey = ""
	}

	// Clamp focused row to stay within filtered bounds.
	if p.State.FocusedRow >= len(p.State.FilteredIndices) {
		p.State.FocusedRow = max(0, len(p.State.FilteredIndices)-1)
	}

	// Clamp focused column in case columns were removed.
	if p.State.FocusedCol >= p.State.ColumnCount {
		p.State.FocusedCol = max(0, p.State.ColumnCount-1)
	}

	// If the focused row points at an expanded section header (e.g. default
	// zero value on first load), advance to the first non-section row so the
	// initial highlight lands on an editable cell. Skipped when the user
	// explicitly jumped to this row this frame — an alias badge click on a
	// root section is a deliberate ask to focus that section, not a drift.
	// Collapsed section headers are intentionally landable either way — users
	// press Cmd+/ there to unfold.
	if !resolvedExplicitFocus && len(p.State.FilteredIndices) > 0 && p.State.FocusedRow >= 0 {
		entries := p.State.Entries
		filtered := p.State.FilteredIndices

		if idx := filtered[p.State.FocusedRow]; idx < len(entries) &&
			entries[idx].IsSection() && !p.State.CollapsedKeys[entries[idx].Key] {
			for r, fi := range filtered {
				if fi < len(entries) && entries[fi].IsFocusable() {
					p.State.FocusedRow = r

					break
				}
			}
		}
	}

	p.Table.FocusedRow = p.State.FocusedRow
	p.Table.FocusedCol = p.State.FocusedCol

	// When the controller signals a pending focus highlight (fires once per
	// chart load after the async UI-state load completes), focus the
	// highlighted cell's editor so the user can start typing without having
	// to click. The flag stays set across frames until focus actually lands —
	// the first attempt may be dropped because the editor's event tag wasn't
	// yet registered in the ops tree (list still scrolling to row) or because
	// the editors slice was reallocated mid-load. A one-line guard protects
	// an actively-searching user: steal focus back to the cell only when the
	// search editor is unfocused or empty.
	if p.State.PendingFocusHighlight {
		if p.State.FocusedRow >= 0 && p.State.FocusedRow < len(p.State.FilteredIndices) {
			p.State.OverrideList.Position.First = max(0, p.State.FocusedRow-scrollContextRows)
			p.State.OverrideList.Position.Offset = 0
		}

		// Section rows have no editor cells, so a FocusCmd retry would spin
		// forever waiting for a tag that never registers. Scroll lands the
		// row in view; that's all the user can meaningfully "focus" on a
		// section header. Same fallthrough for out-of-range indices.
		targetIsFocusable := false

		if p.State.ColumnCount > 0 &&
			p.State.FocusedRow >= 0 && p.State.FocusedRow < len(p.State.FilteredIndices) &&
			p.State.FocusedCol >= 0 && p.State.FocusedCol < p.State.ColumnCount {
			entryIdx := p.State.FilteredIndices[p.State.FocusedRow]
			if entryIdx < len(p.State.Entries) && p.State.Entries[entryIdx].IsFocusable() {
				targetIsFocusable = true
			}
		}

		done := true

		if targetIsFocusable {
			entryIdx := p.State.FilteredIndices[p.State.FocusedRow]
			editors := p.State.Columns[p.State.FocusedCol].OverrideEditors

			if entryIdx < len(editors) {
				searchBusy := gtx.Focused(p.Search.Editor) && p.Search.Editor.Text() != ""

				switch {
				case gtx.Focused(&editors[entryIdx]):
					// Focus landed — stop retrying.
				case searchBusy:
					// User is actively typing in search; don't steal focus.
				default:
					gtx.Execute(key.FocusCmd{Tag: &editors[entryIdx]})
					gtx.Execute(op.InvalidateCmd{})

					p.State.FocusHighlightAttempts++

					if p.State.FocusHighlightAttempts < focusHighlightMaxAttempts {
						done = false
					}
				}
			}
		}

		if done {
			p.State.PendingFocusHighlight = false
			p.State.FocusHighlightAttempts = 0

			// Treat the restored focus as already-synced so we don't
			// immediately re-persist it back to disk on the next
			// change-detection check.
			p.lastFocusedRow = p.State.FocusedRow
			p.lastFocusedCol = p.State.FocusedCol
		}
	} else if p.State.FocusedRow != p.lastFocusedRow || p.State.FocusedCol != p.lastFocusedCol {
		p.lastFocusedRow = p.State.FocusedRow
		p.lastFocusedCol = p.State.FocusedCol

		if p.OnCellFocusChanged != nil {
			p.OnCellFocusChanged(p.State.FocusedEntryKey(), p.State.FocusedCol)
		}
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
					searchHint := platform.ShortcutLabel("\u2318+F", "Ctrl+F")

					if p.State.ExtrasFilterClick.Clicked(gtx) {
						p.State.ExtrasOnly = !p.State.ExtrasOnly
					}

					dims := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return p.Search.Layout(gtx, p.Theme, "Search values... ("+searchHint+")")
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Right: valuesSpacing}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return customwidget.LayoutExtrasFilterPill(gtx, p.Theme, &p.State.ExtrasFilterClick, p.State.ExtrasOnly)
							})
						}),
					)

					totalRigidH += dims.Size.Y

					return dims
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					dims := p.layoutColumnHeaders(gtx)
					totalRigidH += dims.Size.Y
					p.dropdownTopOffset = totalRigidH

					return dims
				}),
				layout.Rigid(p.layoutStickyParent),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return p.Table.Layout(gtx,
						p.State.Entries,
						p.State.FilteredIndices,
					)
				}),
			)
		}),
		layout.Expanded(p.layoutRecentDropdownOverlay),
		layout.Expanded(p.layoutAnchorMenuOverlay),
		layout.Expanded(p.layoutAnchorDialogOverlay),
		layout.Expanded(p.layoutUnlockDialogOverlay),
		layout.Expanded(p.layoutDeleteAnchorDialogOverlay),
	)
}

// processDropdownEvents handles all recent dropdown click events and the
// +Values button in one place to avoid double-consuming Clicked() events.
// In particular, AddColumnButton.Clicked must be consumed here (not during
// layout) because layoutTrailingButtons uses op.Record to probe button width,
// which would consume the click event in the discarded measurement pass.
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
					lbl.Color = theme.Default.Danger

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
