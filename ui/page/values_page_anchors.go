package page

import (
	"image"
	"slices"
	"strconv"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"

	"github.com/qdeck-app/qdeck/domain"
	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/state"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

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
	if !entry.IsFocusable() {
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
