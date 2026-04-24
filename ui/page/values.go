package page

import (
	"image"
	"image/color"
	"io"
	"path/filepath"
	"slices"
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

	"github.com/qdeck-app/qdeck/domain"
	"github.com/qdeck-app/qdeck/service"
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

	// scrollContextRows is how many rows of context to show above a
	// scrolled-into-view target row (cell navigation, restored focus).
	scrollContextRows = 2

	// Hotkey + color-legend hint shown in the notification bar's idle slot.
	helpLegendItemGap    unit.Dp = 10
	helpGlyphTextGap     unit.Dp = 3
	helpShortcutTrailGap unit.Dp = 10
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

// helpShortcutLine renders the hotkey help using native glyphs per platform:
// Mac gets modifier/tab symbols, Windows/Linux gets spelled-out names.
//
//nolint:gochecknoglobals // platform-specific hint resolved once at init
var helpShortcutLine = customwidget.ShortcutLabel(
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
	OnShowCommentsChanged   func(show bool)
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
	p.Table.ShowComments = p.State.ShowComments.Value

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

	// Build columnEditors slice for search filter.
	for c := range p.State.ColumnCount {
		p.columnEditorSlices[c] = p.State.Columns[c].OverrideEditors
	}

	// Recompute filtered indices.
	p.State.FilteredIndices = p.Search.FilterEntriesWithMultiOverrides(
		p.State.Entries,
		p.columnEditorSlices[:p.State.ColumnCount],
		p.State.FilteredIndices,
	)

	// Snapshot the user's collapsed set when search becomes active, so that
	// any search-induced auto-uncollapse below can be reverted when the user
	// clears the search. Restore on the reverse transition.
	inSearch := p.Search.Editor.Text() != ""
	p.syncSearchCollapseSnapshot(inSearch)

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
	// pending key and fall through to the clamps below.
	if p.State.PendingFocusKey != "" {
		entries := p.State.Entries
		for r, idx := range p.State.FilteredIndices {
			if idx < len(entries) && entries[idx].Key == p.State.PendingFocusKey {
				p.State.FocusedRow = r

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
	// initial highlight lands on an editable cell. Collapsed section headers
	// are intentionally landable — users press Cmd+/ there to unfold.
	if len(p.State.FilteredIndices) > 0 && p.State.FocusedRow >= 0 {
		entries := p.State.Entries
		filtered := p.State.FilteredIndices

		if idx := filtered[p.State.FocusedRow]; idx < len(entries) &&
			entries[idx].IsSection() && !p.State.CollapsedKeys[entries[idx].Key] {
			for r, fi := range filtered {
				if fi < len(entries) && !entries[fi].IsSection() {
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
	// to click. Guarded on the search editor not being focused so a user who
	// clicked Search while the chart was loading keeps their focus intact.
	if p.State.PendingFocusHighlight {
		p.State.PendingFocusHighlight = false

		if p.State.FocusedRow >= 0 && p.State.FocusedRow < len(p.State.FilteredIndices) {
			// Scroll the restored row into view; otherwise the highlight
			// lands off-screen and the user sees an unscrolled list with no
			// visible focus indicator.
			p.State.OverrideList.Position.First = max(0, p.State.FocusedRow-scrollContextRows)
			p.State.OverrideList.Position.Offset = 0
		}

		if !gtx.Focused(p.Search.Editor) &&
			p.State.ColumnCount > 0 &&
			p.State.FocusedRow >= 0 && p.State.FocusedRow < len(p.State.FilteredIndices) &&
			p.State.FocusedCol >= 0 && p.State.FocusedCol < p.State.ColumnCount {
			entryIdx := p.State.FilteredIndices[p.State.FocusedRow]
			editors := p.State.Columns[p.State.FocusedCol].OverrideEditors

			if entryIdx < len(editors) {
				gtx.Execute(key.FocusCmd{Tag: &editors[entryIdx]})
			}
		}

		// Treat the restored focus as already-synced so we don't immediately
		// re-persist it back to disk on the next change-detection check.
		p.lastFocusedRow = p.State.FocusedRow
		p.lastFocusedCol = p.State.FocusedCol
	} else if p.State.FocusedRow != p.lastFocusedRow || p.State.FocusedCol != p.lastFocusedCol {
		p.lastFocusedRow = p.State.FocusedRow
		p.lastFocusedCol = p.State.FocusedCol

		if p.OnCellFocusChanged != nil {
			var entryKey string

			entries := p.State.Entries
			if p.State.FocusedRow >= 0 && p.State.FocusedRow < len(p.State.FilteredIndices) {
				if idx := p.State.FilteredIndices[p.State.FocusedRow]; idx < len(entries) {
					entryKey = entries[idx].Key
				}
			}

			p.OnCellFocusChanged(entryKey, p.State.FocusedCol)
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

// layoutDeleteAnchorDialogOverlay renders the "delete anchor + aliases?"
// confirmation opened from the AliasesOf dialog. Confirming fires the
// OnAnchorDelete callback; the controller mutates the NodeTree to remove
// the anchor and sever every alias.
func (p *ValuesPage) layoutDeleteAnchorDialogOverlay(gtx layout.Context) layout.Dimensions {
	if !p.State.DeleteAnchorDialogOpen {
		return layout.Dimensions{}
	}

	dispatchConfirmDialog(gtx, &p.DeleteAnchorDialog, func() {
		if p.OnAnchorDelete != nil {
			p.OnAnchorDelete(p.State.PendingDeleteAnchorCol, p.State.PendingDeleteAnchorName)
		}
	}, p.closeDeleteAnchorDialog)

	return p.DeleteAnchorDialog.Layout(gtx, p.Theme, p.deleteAnchorMessage())
}

// dispatchConfirmDialog drains the confirm-dialog update and dispatches to
// onYes or closes unconditionally. Consolidates the Update/Close/Invalidate
// boilerplate shared by every confirm dialog overlay.
func dispatchConfirmDialog(
	gtx layout.Context,
	dialog *customwidget.ConfirmDialog,
	onYes func(),
	onClose func(),
) {
	switch dialog.Update(gtx) {
	case customwidget.ConfirmYes:
		onYes()
		onClose()
		gtx.Execute(op.InvalidateCmd{})
	case customwidget.ConfirmNo:
		onClose()
		gtx.Execute(op.InvalidateCmd{})
	case customwidget.ConfirmNone:
	}
}

// deleteAnchorMessage formats the confirmation text, including the alias
// count so the user sees the blast radius before confirming.
func (p *ValuesPage) deleteAnchorMessage() string {
	name := p.State.PendingDeleteAnchorName
	refs := aliasUsageKeys(p.State, p.State.PendingDeleteAnchorCol, name)

	if len(refs) == 0 {
		return "Delete anchor &" + name + "?"
	}

	return "Delete anchor &" + name + "? " + strconv.Itoa(len(refs)) +
		" alias(es) will be replaced with their current values."
}

func (p *ValuesPage) closeDeleteAnchorDialog() {
	p.State.DeleteAnchorDialogOpen = false
	p.State.PendingDeleteAnchorCol = 0
	p.State.PendingDeleteAnchorName = ""
}

// layoutUnlockDialogOverlay renders the "unlock this cell?" confirm dialog
// when the user has typed on a locked (anchored) cell. Confirming fires the
// OnUnlockCell callback so the controller removes the anchor/alias; the
// cell becomes editable the next frame.
func (p *ValuesPage) layoutUnlockDialogOverlay(gtx layout.Context) layout.Dimensions {
	if !p.State.UnlockDialogOpen {
		return layout.Dimensions{}
	}

	dispatchConfirmDialog(gtx, &p.UnlockDialog, func() {
		if p.OnUnlockCell != nil {
			p.OnUnlockCell(p.State.PendingUnlockCol, p.State.PendingUnlockKey)
		}
	}, p.closeUnlockDialog)

	return p.UnlockDialog.Layout(gtx, p.Theme, "Editing this cell will remove its YAML anchor/alias. Continue?")
}

func (p *ValuesPage) closeUnlockDialog() {
	p.State.UnlockDialogOpen = false
	p.State.PendingUnlockCol = 0
	p.State.PendingUnlockKey = ""
}

// layoutAnchorMenuOverlay renders the right-click context menu when
// State.AnchorMenuOpen is true. Returns zero-size when closed so it doesn't
// block underlying input.
func (p *ValuesPage) layoutAnchorMenuOverlay(gtx layout.Context) layout.Dimensions {
	if !p.State.AnchorMenuOpen {
		return layout.Dimensions{}
	}

	// Disable the "Alias to…" item when the target column has no anchors —
	// otherwise the picker would open empty.
	p.AnchorMenu.DisableAlias = len(availableAnchorNames(p.State, p.State.AnchorMenuCol)) == 0

	switch p.AnchorMenu.Update(gtx) {
	case customwidget.AnchorMenuCreate:
		p.beginAnchorOp(state.AnchorOpCreate, p.State.AnchorMenuCol, p.State.AnchorMenuKey)
		p.State.AnchorMenuOpen = false

		gtx.Execute(op.InvalidateCmd{})
	case customwidget.AnchorMenuAlias:
		p.beginAnchorOp(state.AnchorOpAlias, p.State.AnchorMenuCol, p.State.AnchorMenuKey)
		p.State.AnchorMenuOpen = false

		gtx.Execute(op.InvalidateCmd{})
	case customwidget.AnchorMenuDismiss:
		p.State.AnchorMenuOpen = false

		gtx.Execute(op.InvalidateCmd{})
	case customwidget.AnchorMenuNone:
	}

	// Place the menu at the actual right-click point. Clipping inside the
	// widget keeps it on screen when the pointer is near an edge.
	return p.AnchorMenu.Layout(gtx, p.Theme, p.State.AnchorMenuPos)
}

// layoutAnchorDialogOverlay renders the create/alias modal when
// State.AnchorOp is non-zero. On submit applies the op via callbacks and
// closes; on cancel just closes.
func (p *ValuesPage) layoutAnchorDialogOverlay(gtx layout.Context) layout.Dimensions {
	mode := p.dialogMode()
	if mode == customwidget.AnchorDialogNone {
		return layout.Dimensions{}
	}

	switch p.AnchorDialog.Update(gtx, mode) {
	case customwidget.AnchorActionSubmit:
		// applyAnchorOp dispatches on State.AnchorOp, so dispatch BEFORE
		// closing (which resets the mode). Otherwise the branch that
		// matches this submission is lost and the action is silently a no-op.
		p.applyAnchorOp(gtx, p.AnchorDialog.Result())
		p.closeAnchorDialog()
		gtx.Execute(op.InvalidateCmd{})
	case customwidget.AnchorActionCancel:
		p.closeAnchorDialog()
		gtx.Execute(op.InvalidateCmd{})
	case customwidget.AnchorActionRename:
		// Transition directly from AliasesOf to Rename — same modal, reload
		// it with the Create body pre-filled with the current anchor name.
		col := p.State.AnchorOpCol
		oldName := p.State.AnchorOpName
		p.State.AnchorOp = state.AnchorOpRename
		p.State.AnchorOpCol = col
		p.State.AnchorOpName = oldName
		p.State.AnchorOpKey = ""
		p.AnchorDialog.SetupForCreate(oldName)
		gtx.Execute(op.InvalidateCmd{})
	case customwidget.AnchorActionDelete:
		// Close the aliases dialog and open the delete-confirm overlay. The
		// target is captured before close so the confirm handler knows
		// which anchor to remove.
		p.State.PendingDeleteAnchorCol = p.State.AnchorOpCol
		p.State.PendingDeleteAnchorName = p.State.AnchorOpName
		p.State.DeleteAnchorDialogOpen = true
		p.closeAnchorDialog()
		gtx.Execute(op.InvalidateCmd{})
	case customwidget.AnchorActionNone:
	}

	// The action handlers above can switch modes (AliasesOf → Rename) or
	// close the dialog outright. Recompute so Layout renders the post-Update
	// body and the one-shot FocusCmd inside AnchorDialog.Layout lands on
	// the editor actually rendered this frame.
	mode = p.dialogMode()
	if mode == customwidget.AnchorDialogNone {
		return layout.Dimensions{}
	}

	return p.AnchorDialog.Layout(gtx, p.Theme, mode, p.dialogTitle())
}

func (p *ValuesPage) dialogMode() customwidget.AnchorDialogMode {
	switch p.State.AnchorOp {
	case state.AnchorOpCreate, state.AnchorOpRename:
		// Rename reuses the Create body — a single text input pre-filled with
		// the current name. Dispatch is distinguished at submit time via
		// applyAnchorOp, not by the widget.
		return customwidget.AnchorDialogCreate
	case state.AnchorOpAlias:
		return customwidget.AnchorDialogAlias
	case state.AnchorOpAliasesOf:
		return customwidget.AnchorDialogAliasesOf
	case state.AnchorOpNone:
	}

	return customwidget.AnchorDialogNone
}

func (p *ValuesPage) dialogTitle() string {
	switch p.State.AnchorOp {
	case state.AnchorOpCreate:
		return "Create anchor"
	case state.AnchorOpRename:
		return "Rename anchor &" + p.State.AnchorOpName
	case state.AnchorOpAlias:
		return "Alias to anchor"
	case state.AnchorOpAliasesOf:
		return "Aliases of &" + p.State.AnchorOpName
	case state.AnchorOpNone:
	}

	return ""
}

// applyAnchorOp dispatches the confirmed dialog submission to the controller
// or the page's navigation helpers, depending on the current mode.
func (p *ValuesPage) applyAnchorOp(gtx layout.Context, value string) {
	if value == "" {
		return
	}

	switch p.State.AnchorOp {
	case state.AnchorOpCreate:
		if p.OnAnchorCreate != nil {
			p.OnAnchorCreate(p.State.AnchorOpCol, p.State.AnchorOpKey, value)
		}
	case state.AnchorOpRename:
		if p.OnAnchorRename != nil && p.State.AnchorOpName != "" && p.State.AnchorOpName != value {
			p.OnAnchorRename(p.State.AnchorOpCol, p.State.AnchorOpName, value)
		}
	case state.AnchorOpAlias:
		if p.OnAnchorAlias != nil {
			p.OnAnchorAlias(p.State.AnchorOpCol, p.State.AnchorOpKey, value)
		}
	case state.AnchorOpAliasesOf:
		p.jumpToFlatKey(gtx, value)
	case state.AnchorOpNone:
	}
}

// openUnlockDialog is wired to the table's OnAnchoredCellEdit: when the user
// types on a cell participating in a YAML anchor/alias, the widget reverts
// the text and hands the (col, flatKey) pair here. We stash the target and
// flip the confirm dialog open; the next frame renders the overlay.
func (p *ValuesPage) openUnlockDialog(col int, flatKey string) {
	if p.State.UnlockDialogOpen {
		return
	}

	p.State.UnlockDialogOpen = true
	p.State.PendingUnlockCol = col
	p.State.PendingUnlockKey = flatKey
}

// openAnchorDialog is the entry point for the Cmd+Shift+A / Cmd+Shift+L
// shortcuts. It resolves the currently focused cell and opens the dialog in
// create or alias mode. Does nothing when no override column is active or
// the focused row isn't editable.
func (p *ValuesPage) openAnchorDialog(mode state.AnchorOpMode) {
	col, flatKey, ok := p.resolveFocusedCell()
	if !ok {
		return
	}

	p.beginAnchorOp(mode, col, flatKey)
	// Close any right-click menu so only one UI is active at a time.
	p.State.AnchorMenuOpen = false
}

// openAnchorMenuFromKeyboard is the keyboard equivalent of a right-click.
// Resolves the focused cell and opens the context menu at a position near
// the cell (computed from the list's scroll state + table column geometry)
// rather than the last-known pointer position, so the menu lands next to
// whatever the user is actually editing.
func (p *ValuesPage) openAnchorMenuFromKeyboard(gtx layout.Context) {
	col, flatKey, ok := p.resolveFocusedCell()
	if !ok {
		return
	}

	p.State.AnchorMenuOpen = true
	p.State.AnchorMenuCol = col
	p.State.AnchorMenuKey = flatKey
	p.State.AnchorMenuPos = p.approxFocusedCellOnScreen(gtx)
	p.State.AnchorOp = state.AnchorOpNone
	p.AnchorMenu.Reset()
}

// approxFocusedCellOnScreen returns a page-local point near the top-left of
// the currently focused override cell. Uses the list's Position.First and
// Offset to estimate vertical placement, and the page's column ratio +
// ColumnCount for horizontal placement. Row height is approximated with a
// constant because widget.List doesn't expose per-row pixel offsets — good
// enough because AnchorContextMenu clamps its own origin to stay on screen.
func (p *ValuesPage) approxFocusedCellOnScreen(gtx layout.Context) image.Point {
	rowHeight := gtx.Dp(keyboardMenuRowHeight)

	pos := p.State.OverrideList.Position

	visibleRow := p.State.FocusedRow - pos.First
	if visibleRow < 0 {
		visibleRow = 0
	}

	// pos.Offset is Gio's sub-row pixel offset of the first visible row — it's
	// positive when the first row has been scrolled partially up off-screen,
	// so subtracting it shifts our estimate down to compensate. The subsequent
	// clamp guarantees the menu origin never lands above the table.
	y := p.dropdownTopOffset + visibleRow*rowHeight - pos.Offset
	if y < p.dropdownTopOffset {
		y = p.dropdownTopOffset
	}

	totalW := gtx.Constraints.Max.X

	ratio := p.Table.ColumnRatio
	if ratio <= 0 {
		ratio = keyboardMenuDefaultRatio
	}

	leftW := int(float32(totalW) * ratio)
	rightW := totalW - leftW

	colCount := p.State.ColumnCount
	if colCount < 1 {
		colCount = 1
	}

	colW := rightW / colCount

	col := p.State.FocusedCol
	if col < 0 {
		col = 0
	}

	x := leftW + col*colW + gtx.Dp(keyboardMenuColumnInset)

	return image.Pt(x, y)
}

// openAnchorMenu is wired to the table's OnCellContextMenu. Records the menu
// target and position. The dialog is deferred until the user picks an action.
// localPos from the widget is in cell-local coordinates and not directly
// usable for placement; we substitute lastPointerPos which is tracked in
// page-local coordinates by handleKeyEvents, giving accurate menu placement
// under the cursor.
func (p *ValuesPage) openAnchorMenu(col int, flatKey string, tableLocalPos image.Point) {
	p.State.AnchorMenuOpen = true
	p.State.AnchorMenuCol = col
	p.State.AnchorMenuKey = flatKey
	// tableLocalPos is in table-root-local coords (captured by the widget's
	// root pointer filter). The table begins at dropdownTopOffset from the
	// page's top-left, so add it to land in page-local coords.
	p.State.AnchorMenuPos = image.Point{
		X: tableLocalPos.X,
		Y: tableLocalPos.Y + p.dropdownTopOffset,
	}
	// Dismiss any open dialog — menu takes precedence.
	p.State.AnchorOp = state.AnchorOpNone
	p.AnchorMenu.Reset()
}

// beginAnchorOp initializes the dialog for a create/alias prompt on a cell.
func (p *ValuesPage) beginAnchorOp(mode state.AnchorOpMode, col int, flatKey string) {
	p.State.AnchorOp = mode
	p.State.AnchorOpCol = col
	p.State.AnchorOpKey = flatKey
	p.State.AnchorOpName = ""

	switch mode {
	case state.AnchorOpCreate:
		p.AnchorDialog.SetupForCreate(suggestAnchorName(flatKey))
	case state.AnchorOpAlias:
		p.AnchorDialog.SetupForAlias(availableAnchorNames(p.State, col))
	case state.AnchorOpAliasesOf, state.AnchorOpNone:
		// AliasesOf is initialized by openAliasesOfDialog which already
		// knows the anchor name and items; the bare begin path is unused.
	}
}

// openAliasesOfDialog is wired to the table's OnAnchorBadgeClicked. It opens
// the AnchorDialog in AliasesOf mode, populating the list with every flat
// key where the column's file has an alias pointing at anchorName. On submit,
// the picked flat key is jumped-to via the existing jumpToFlatKey handler.
func (p *ValuesPage) openAliasesOfDialog(gtx layout.Context, col int, flatKey, anchorName string) {
	aliases := aliasUsageKeys(p.State, col, anchorName)

	p.State.AnchorOp = state.AnchorOpAliasesOf
	p.State.AnchorOpCol = col
	p.State.AnchorOpKey = flatKey
	p.State.AnchorOpName = anchorName
	p.State.AnchorMenuOpen = false

	p.AnchorDialog.SetupForAliasesOf(aliases)
	gtx.Execute(op.InvalidateCmd{})
}

// aliasUsageKeys returns flat keys where the column's file has an alias to
// anchorName. Sorted for stable UI ordering.
func aliasUsageKeys(st *state.ValuesPageState, col int, anchorName string) []string {
	if col < 0 || col >= state.MaxCustomColumns {
		return nil
	}

	cs := &st.Columns[col]
	if cs.CustomValues == nil || len(cs.CustomValues.Anchors) == 0 {
		return nil
	}

	var keys []string

	for k, info := range cs.CustomValues.Anchors {
		if info.Role == service.AnchorRoleAlias && info.Name == anchorName {
			keys = append(keys, k)
		}
	}

	slices.Sort(keys)

	return keys
}

// closeAnchorDialog clears the dialog state without applying the op.
func (p *ValuesPage) closeAnchorDialog() {
	p.State.AnchorOp = state.AnchorOpNone
	p.State.AnchorOpKey = ""
	p.State.AnchorOpCol = 0
	p.State.AnchorOpName = ""
}

// resolveFocusedCell returns the column and flat key for the currently
// focused editable cell, or ok=false when no valid target is available.
func (p *ValuesPage) resolveFocusedCell() (int, string, bool) {
	if p.State.ColumnCount == 0 {
		return 0, "", false
	}

	row := p.State.FocusedRow

	col := p.State.FocusedCol
	if row < 0 || row >= len(p.State.FilteredIndices) {
		return 0, "", false
	}

	if col < 0 || col >= p.State.ColumnCount {
		return 0, "", false
	}

	entryIdx := p.State.FilteredIndices[row]
	if entryIdx >= len(p.State.Entries) {
		return 0, "", false
	}

	entry := p.State.Entries[entryIdx]
	if entry.IsSection() {
		return 0, "", false
	}

	return col, entry.Key, true
}

// suggestAnchorName offers a default anchor name when the user opens the
// create dialog. Joins the flat key's last two segments with "_" so the
// default hints at the parent (`containerPorts_metrics` is more meaningful
// than bare `metrics`), then strips characters that YAML anchors don't
// allow — brackets from array indices, dots left by weird segments, etc.
// Falls back to "anchor" when the sanitized result is empty and prefixes
// "_" when the first char would be a digit.
func suggestAnchorName(flatKey string) string {
	parts := strings.Split(flatKey, ".")

	tail := parts
	if len(parts) > 2 { //nolint:mnd // last-two segments, not a tunable.
		tail = parts[len(parts)-2:]
	}

	return sanitizeAnchorName(strings.Join(tail, "_"))
}

// sanitizeAnchorName keeps only runes allowed by service.AnchorNameRegex
// (letters, digits, underscore, hyphen). Any other rune is dropped. A
// leading digit gets an "_" prefix so the result begins with a letter or
// underscore. Returns "anchor" when the input has no usable runes.
func sanitizeAnchorName(s string) string {
	var b strings.Builder
	b.Grow(len(s))

	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-':
			b.WriteRune(r)
		}
	}

	result := b.String()
	if result == "" {
		return "anchor"
	}

	if c := result[0]; c >= '0' && c <= '9' {
		result = "_" + result
	}

	return result
}

// availableAnchorNames returns the list of anchor names declared in the given
// column's loaded file. Empty when no file is loaded or no anchors exist.
func availableAnchorNames(st *state.ValuesPageState, col int) []string {
	if col < 0 || col >= state.MaxCustomColumns {
		return nil
	}

	cs := &st.Columns[col]
	if cs.CustomValues == nil || len(cs.CustomValues.Anchors) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(cs.CustomValues.Anchors))

	for _, info := range cs.CustomValues.Anchors {
		if info.Role == service.AnchorRoleAnchor && info.Name != "" {
			seen[info.Name] = struct{}{}
		}
	}

	if len(seen) == 0 {
		return nil
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}

	slices.Sort(names)

	return names
}

// jumpToFlatKey is the OnJumpToFlatKey handler: when the user clicks an alias
// badge, scroll and focus the row that defines the matching anchor. Ancestors
// of the target are uncollapsed so the row is reachable by the list
// virtualizer. The actual scroll+focus happens on the next frame via the
// PendingFocusKey / PendingFocusHighlight machinery, which already handles
// the post-filter row lookup and FocusCmd dispatch.
func (p *ValuesPage) jumpToFlatKey(gtx layout.Context, key string) {
	if key == "" {
		return
	}

	if p.State.CollapsedKeys == nil {
		p.State.CollapsedKeys = make(map[string]bool)
	}

	for parent := domain.FlatKey(key).Parent(); parent != ""; parent = parent.Parent() {
		delete(p.State.CollapsedKeys, string(parent))
	}

	p.State.PendingFocusKey = key
	p.State.PendingFocusHighlight = true

	gtx.Execute(op.InvalidateCmd{})
}

// onCollapseToggle flips the collapsed state for the given section key and
// fires OnCollapseChanged so the controller persists the new set. During a
// search the toggle is mirrored into CollapsedPreSearch so the user's intent
// survives the search-clear restore.
func (p *ValuesPage) onCollapseToggle(key string) {
	if p.State.CollapsedKeys == nil {
		p.State.CollapsedKeys = make(map[string]bool)
	}

	if p.State.CollapsedKeys[key] {
		delete(p.State.CollapsedKeys, key)
	} else {
		p.State.CollapsedKeys[key] = true
	}

	if p.State.SearchCollapseActive {
		if p.State.CollapsedPreSearch == nil {
			p.State.CollapsedPreSearch = make(map[string]bool)
		}

		if p.State.CollapsedPreSearch[key] {
			delete(p.State.CollapsedPreSearch, key)
		} else {
			p.State.CollapsedPreSearch[key] = true
		}
	}

	if p.OnCollapseChanged != nil {
		p.OnCollapseChanged()
	}
}

// syncSearchCollapseSnapshot manages CollapsedPreSearch across the empty↔︎
// non-empty search transitions. Entering search captures the user's current
// collapsed set so search-induced auto-uncollapses can be undone later.
// Leaving search restores that snapshot into the effective CollapsedKeys and
// persists the result (idempotent when no mid-search user toggles happened).
func (p *ValuesPage) syncSearchCollapseSnapshot(inSearch bool) {
	switch {
	case inSearch && !p.State.SearchCollapseActive:
		p.State.CollapsedPreSearch = cloneCollapsed(p.State.CollapsedKeys)
		p.State.SearchCollapseActive = true
	case !inSearch && p.State.SearchCollapseActive:
		if !collapsedEqual(p.State.CollapsedKeys, p.State.CollapsedPreSearch) {
			p.State.CollapsedKeys = cloneCollapsed(p.State.CollapsedPreSearch)

			if p.OnCollapseChanged != nil {
				p.OnCollapseChanged()
			}
		}

		p.State.CollapsedPreSearch = nil
		p.State.SearchCollapseActive = false
	}
}

// cloneCollapsed returns an independent copy of a collapsed-keys map, or nil
// when the source has no entries. Callers depend on a non-aliased copy so
// later mutations of one don't leak into the other.
func cloneCollapsed(m map[string]bool) map[string]bool {
	if len(m) == 0 {
		return nil
	}

	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}

	return out
}

// collapsedEqual reports whether two collapsed-keys maps describe the same
// set. Both nil and empty maps are treated as equivalent.
func collapsedEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}

	for k := range a {
		if !b[k] {
			return false
		}
	}

	return true
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

	// Track the cursor position in page-local coordinates so the right-click
	// context menu can be placed at the actual click point. The widget only
	// sees cell-local coords; resolving them to page-local would require
	// composing every ancestor offset, so we sidestep the transform by
	// subscribing to page-root pointer events — Position there is already
	// page-local. Move updates on every hover; Press is redundant but cheap.
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: p,
			Kinds:  pointer.Move | pointer.Press,
		})
		if !ok {
			break
		}

		if pe, isPtr := ev.(pointer.Event); isPtr {
			p.lastPointerPos = pe.Position.Round()
		}
	}

	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameUpArrow, Required: cellNavMod},
			key.Filter{Name: key.NameDownArrow, Required: cellNavMod},
			key.Filter{Name: key.NameLeftArrow, Required: cellNavMod},
			key.Filter{Name: key.NameRightArrow, Required: cellNavMod},
			key.Filter{Name: key.NameTab},
			key.Filter{Name: key.NameTab, Required: key.ModShift},
			key.Filter{Name: "/", Required: key.ModShortcut},
			key.Filter{Name: "A", Required: cellNavMod},
			key.Filter{Name: "L", Required: cellNavMod},
			key.Filter{Name: "M", Required: cellNavMod},
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
		case "/":
			p.handleCollapseShortcut(gtx)
		case "A":
			p.openAnchorDialog(state.AnchorOpCreate)
			gtx.Execute(op.InvalidateCmd{})
		case "L":
			p.openAnchorDialog(state.AnchorOpAlias)
			gtx.Execute(op.InvalidateCmd{})
		case "M":
			p.openAnchorMenuFromKeyboard(gtx)
			gtx.Execute(op.InvalidateCmd{})
		}
	}
}

// handleCollapseShortcut toggles the collapsed state of the section enclosing
// the focused cell. When the focused row is itself a section, that section is
// toggled; when it's a leaf, its parent section is toggled. On collapse the
// focus is moved to the next editable leaf after the section so the user
// isn't left with focus on a now-hidden row.
func (p *ValuesPage) handleCollapseShortcut(gtx layout.Context) {
	if p.State.DefaultValues == nil || len(p.State.FilteredIndices) == 0 {
		return
	}

	row := p.State.FocusedRow
	if row < 0 || row >= len(p.State.FilteredIndices) {
		return
	}

	entries := p.State.Entries

	entryIdx := p.State.FilteredIndices[row]
	if entryIdx >= len(entries) {
		return
	}

	targetKey := entries[entryIdx].Key
	if !entries[entryIdx].IsSection() {
		parent := domain.FlatKey(targetKey).Parent()
		if parent == "" {
			return
		}

		targetKey = string(parent)
	}

	wasCollapsed := p.State.CollapsedKeys[targetKey]

	p.onCollapseToggle(targetKey)

	// Relocate focus so the cursor always lands on an editable cell.
	// Collapse: first leaf outside the now-hidden subtree.
	// Expand:   first newly-visible leaf inside the section.
	var nextKey string
	if wasCollapsed {
		nextKey = p.firstLeafInsideSection(targetKey)
	} else {
		nextKey = p.nextLeafOutsideSection(targetKey)
	}

	if nextKey != "" {
		p.State.PendingFocusKey = nextKey
		p.State.PendingFocusHighlight = true
	}

	gtx.Execute(op.InvalidateCmd{})
}

// firstLeafInsideSection returns the flat key of the first editable leaf
// inside the given section that isn't hidden by a nested collapsed section.
// Returns "" when every descendant is either a section header or buried under
// another collapsed ancestor (caller leaves focus alone in that case).
//
// Descendants are NOT guaranteed to be contiguous in the sorted entries list:
// a sibling sharing sectionKey as a byte prefix can sort between descendants
// (e.g. "svc.tls", "svc.tlsCertFile", "svc.tls[0]" — because '.' < 'C' < '[').
// So we keep scanning past non-descendants and only stop once we've left the
// byte range of keys starting with sectionKey entirely.
func (p *ValuesPage) firstLeafInsideSection(sectionKey string) string {
	entries := p.State.Entries

	sectionIdx := -1

	for i, e := range entries {
		if e.Key == sectionKey {
			sectionIdx = i

			break
		}
	}

	if sectionIdx < 0 {
		return ""
	}

	prefix := sectionKey

	for i := sectionIdx + 1; i < len(entries); i++ {
		k := entries[i].Key

		// Completely past the prefix range → no more descendants can exist.
		if len(k) < len(prefix) || k[:len(prefix)] != prefix {
			return ""
		}

		// Same-prefix sibling (e.g. "svc.tlsCertFile" vs section "svc.tls"):
		// skip, but keep scanning — real descendants may sort after it.
		if len(k) == len(prefix) {
			continue
		}

		if sep := k[len(prefix)]; sep != '.' && sep != '[' {
			continue
		}

		if entries[i].IsSection() {
			continue
		}

		if p.isHiddenByCollapsedAncestor(k) {
			continue
		}

		return k
	}

	return ""
}

// isHiddenByCollapsedAncestor reports whether any ancestor of key is in the
// current collapsed set. Walks via FlatKey.Parent so list-header sections
// (e.g. "foo.bar" above "foo.bar[0].baz") are not skipped.
func (p *ValuesPage) isHiddenByCollapsedAncestor(key string) bool {
	for p2 := domain.FlatKey(key).Parent(); p2 != ""; p2 = p2.Parent() {
		if p.State.CollapsedKeys[string(p2)] {
			return true
		}
	}

	return false
}

// nextLeafOutsideSection returns the flat key of the first editable leaf that
// comes after the given section in the sorted entries list, skipping the
// section's own descendants. Falls back to the last leaf before the section
// when the section sits at the end of the list. Returns "" if no leaf exists.
func (p *ValuesPage) nextLeafOutsideSection(sectionKey string) string {
	entries := p.State.Entries

	sectionIdx := -1

	for i, e := range entries {
		if e.Key == sectionKey {
			sectionIdx = i

			break
		}
	}

	if sectionIdx < 0 {
		return ""
	}

	prefix := sectionKey

	for i := sectionIdx + 1; i < len(entries); i++ {
		k := entries[i].Key

		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			if sep := k[len(prefix)]; sep == '.' || sep == '[' {
				continue
			}
		}

		if entries[i].IsSection() {
			continue
		}

		// Another collapsed section may hide this leaf; skip so the focus
		// target is guaranteed to be visible after the filter rebuild.
		if p.isHiddenByCollapsedAncestor(k) {
			continue
		}

		return k
	}

	for i := sectionIdx - 1; i >= 0; i-- {
		if entries[i].IsSection() {
			continue
		}

		if p.isHiddenByCollapsedAncestor(entries[i].Key) {
			continue
		}

		return entries[i].Key
	}

	return ""
}

// moveCellFocusRow moves keyboard focus to the next/previous non-section
// editor cell in the same column.
func (p *ValuesPage) moveCellFocusRow(gtx layout.Context, delta int) {
	if p.State.DefaultValues == nil || len(p.State.FilteredIndices) == 0 {
		return
	}

	entries := p.State.Entries
	filtered := p.State.FilteredIndices
	row := p.State.FocusedRow
	col := p.State.FocusedCol

	for {
		row += delta
		if row < 0 || row >= len(filtered) {
			return
		}

		entryIdx := filtered[row]
		if entryIdx >= len(entries) {
			continue
		}

		entry := entries[entryIdx]
		// Stop on editable leaves (the common case) or on collapsed section
		// headers, so the user can reach a collapsed chevron with the arrow
		// keys and unfold it via Cmd+/.
		if !entry.IsSection() || p.State.CollapsedKeys[entry.Key] {
			break
		}
	}

	p.State.FocusedRow = row
	p.State.FocusedCol = col

	entryIdx := filtered[row]

	if entries[entryIdx].IsSection() {
		// Section rows have no visible editor; release keyboard focus so the
		// previous editor doesn't keep receiving keystrokes.
		gtx.Execute(key.FocusCmd{Tag: nil})
	} else if col >= 0 && col < p.State.ColumnCount {
		editors := p.State.Columns[col].OverrideEditors
		if entryIdx < len(editors) {
			gtx.Execute(key.FocusCmd{Tag: &editors[entryIdx]})
		}
	}

	p.State.OverrideList.Position.First = max(0, row-scrollContextRows)
}

// moveCellFocusCol moves keyboard focus to the next/previous column on the
// same row, wrapping to the prev/next non-section row at the table edges.
func (p *ValuesPage) moveCellFocusCol(gtx layout.Context, delta int) {
	if p.State.DefaultValues == nil || len(p.State.FilteredIndices) == 0 || p.State.ColumnCount == 0 {
		return
	}

	entries := p.State.Entries
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

	p.State.OverrideList.Position.First = max(0, row-scrollContextRows)
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
