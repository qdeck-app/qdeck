package widget

import (
	"image"
	"io"
	"strings"
	"unicode/utf8"

	"gioui.org/gesture"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"
	"gopkg.in/yaml.v3"

	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/state"
)

// processEditorChanges checks for editor changes across all columns.
func (t *OverrideTable) processEditorChanges(
	gtx layout.Context,
	entries []service.FlatValueEntry,
	entryIdx int,
	section bool,
) {
	if section {
		return
	}

	for c := range t.colCount() {
		editors := t.ColumnEditors[c]
		if entryIdx >= len(editors) {
			continue
		}

		entryKey := entries[entryIdx].Key
		textBefore := editors[entryIdx].Text()
		lenBefore := len(textBefore)
		changed := false

		for {
			ev, ok := editors[entryIdx].Update(gtx)
			if !ok {
				break
			}

			if _, isChange := ev.(widget.ChangeEvent); isChange {
				changed = true
			}
		}

		if !changed {
			continue
		}

		// Alias cells block edits: their value is dictated by the anchor,
		// so changing the text here would silently break the alias at save
		// time. Revert and prompt the user to unlock first.
		//
		// Anchor-definition cells (role=Anchor) stay fully editable — the
		// .Anchor tag lives on the node and survives value edits, so the
		// user can tweak the anchored value without severing any aliases.
		//
		// If OnAnchoredCellEdit is nil the page has opted out of the unlock
		// flow, so reverting would leave the user with no feedback and no
		// way to edit. Fall through and let the keystroke land — the alias
		// will be severed at save time, matching an anchor-unaware table.
		if t.OnAnchoredCellEdit != nil && t.columnAnchorInfo(c, entryKey).Role == service.AnchorRoleAlias {
			// A ChangeEvent whose resulting text already matches the alias's
			// effective value isn't a real divergence — this happens right
			// after "Alias to…" when the controller programmatically syncs
			// the editor to the resolved anchor value (fires a ChangeEvent
			// but commits no drift), and also if the user happens to type
			// the same value the anchor resolves to. In both cases the save
			// would be a no-op, so skip the unlock prompt.
			if t.aliasTextMatchesEffective(c, entryKey, editors[entryIdx].Text()) {
				continue
			}

			editors[entryIdx].SetText(textBefore)
			t.drainEditorEvents(gtx, &editors[entryIdx])

			t.OnAnchoredCellEdit(c, entryKey)

			continue
		}

		// Quick pre-filter: only attempt auto-indent when exactly one
		// byte was added (not paste or deletion). The actual newline
		// check happens inside autoIndentAfterNewline.
		if len(editors[entryIdx].Text()) == lenBefore+1 && autoIndentAfterNewline(&editors[entryIdx]) {
			t.drainEditorEvents(gtx, &editors[entryIdx])
		}

		if t.ColumnStates[c] != nil {
			t.ColumnStates[c].MarkOverride(entryIdx, state.StripYAMLComments(editors[entryIdx].Text()) != "")
		}

		t.propagateAnchoredValueEdit(gtx, c, entryKey, entries, editors)

		if t.OnChanged != nil {
			indent := service.DefaultYAMLIndent

			var (
				tree *yaml.Node
				docs service.DocComments
			)

			if cs := t.ColumnStates[c]; cs != nil {
				indent = cs.YAMLIndent()
				if cs.CustomValues != nil {
					tree = cs.CustomValues.NodeTree
					docs = cs.DocCommentsForSave()
				}
			}

			yamlText, yamlErr := state.OverridesToYAML(entries, editors, indent, tree, docs)
			t.OnChanged(c, yamlText, yamlErr)
		}
	}
}

// drainEditorEvents consumes pending editor events so a programmatic SetText
// doesn't leak a ChangeEvent back to the next frame's loop.
func (t *OverrideTable) drainEditorEvents(gtx layout.Context, ed *widget.Editor) {
	for {
		_, ok := ed.Update(gtx)
		if !ok {
			return
		}
	}
}

// handleCellClick focuses the correct column editor when a right-cell click occurs,
// or copies the field key to clipboard when the left key area is clicked. For
// section rows the whole row copies the key since there are no value cells.
func (t *OverrideTable) handleCellClick(
	gtx layout.Context,
	index int,
	entryIdx int,
	entryKey string,
	g colGeometry,
	rightW int,
	rowH int,
	section bool,
) {
	for {
		ev, ok := t.cellClicks[index].Update(gtx.Source)
		if !ok {
			break
		}

		if ev.Kind != gesture.KindPress {
			continue
		}

		clickX := ev.Position.X
		if section || clickX < g.rightStart {
			// Left-side click (or anywhere on a section row): copy the key path.
			gtx.Execute(clipboard.WriteCmd{
				Type: "text/plain",
				Data: io.NopCloser(strings.NewReader(entryKey)),
			})

			if t.OnKeyCopied != nil {
				t.OnKeyCopied(entryKey)
			}

			continue
		}

		// Determine which column was clicked.
		denom := g.colW + g.subDivW
		if denom <= 0 {
			continue
		}

		col := (clickX - g.rightStart) / denom
		if col >= g.count {
			col = g.count - 1
		}

		editors := t.ColumnEditors[col]
		if entryIdx < len(editors) {
			gtx.Execute(key.FocusCmd{Tag: &editors[entryIdx]})

			if t.OnCellFocused != nil {
				t.OnCellFocused(index, col)
			}
		}
	}

	// Show pointer cursor over the left key area (click-to-copy). For section
	// rows the entire row is clickable, so the pointer cursor spans it.
	keyW := g.leftW / 2 //nolint:mnd // key column is half of the left panel
	if section {
		keyW = g.rightStart + rightW
	}

	keyArea := clip.Rect{Max: image.Pt(keyW, rowH)}.Push(gtx.Ops)
	pointer.CursorPointer.Add(gtx.Ops)
	keyArea.Pop()

	if section {
		return
	}

	// Show text cursor over the right cell area.
	rightArea := clip.Rect{
		Min: image.Pt(g.rightStart, 0),
		Max: image.Pt(g.rightStart+rightW, rowH),
	}.Push(gtx.Ops)
	pointer.CursorText.Add(gtx.Ops)
	rightArea.Pop()
}

// drainRightClicks consumes secondary-button press events for a specific
// (col, rowIndex) cell target. When the press is a right-click, fires
// OnCellContextMenu with the table-local position captured separately by
// captureTableRootPointer — cell-local Position would require a transform
// the widget doesn't track, so we use the table-root-tagged event instead.
func (t *OverrideTable) drainRightClicks(gtx layout.Context, col, rowIndex int, entryKey string) {
	if t.OnCellContextMenu == nil {
		return
	}

	target := &t.rightClickTargets[col][rowIndex]

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: target,
			Kinds:  pointer.Press,
		})
		if !ok {
			break
		}

		pe, isPtr := ev.(pointer.Event)
		if !isPtr || pe.Kind != pointer.Press {
			continue
		}

		if pe.Buttons.Contain(pointer.ButtonSecondary) {
			t.OnCellContextMenu(col, entryKey, t.lastRightClickPos)
		}
	}
}

// captureTableRootPointer registers a clip covering the whole table with a
// dedicated event.Tag wrapped in pointer.PassOp so it does not block — and
// is not blocked by — per-cell event.Op registrations inside the list. Press
// events delivered to this tag arrive in table-local coords, which the page
// can translate to page-local by adding the table's page-Y offset.
//
// Cell-local coords (what pe.Position would give inside drainRightClicks)
// would require composing every ancestor offset, a transform the widget
// does not track.
func (t *OverrideTable) captureTableRootPointer(gtx layout.Context) {
	pass := pointer.PassOp{}.Push(gtx.Ops)
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, &t.tableRootTarget)
	area.Pop()
	pass.Pop()

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: &t.tableRootTarget,
			Kinds:  pointer.Press,
		})
		if !ok {
			break
		}

		if pe, isPtr := ev.(pointer.Event); isPtr && pe.Kind == pointer.Press {
			t.lastRightClickPos = pe.Position.Round()
		}
	}
}

// handleDrag processes pointer events for the column resize divider.
func (t *OverrideTable) handleDrag(gtx layout.Context) {
	ratio := t.ratio()
	totalW := gtx.Constraints.Max.X
	dividerW := gtx.Dp(overrideDividerW)
	dividerX := int(ratio * float32(totalW))

	// Wider hit area for easier dragging.
	hitPad := gtx.Dp(overridePaddingH)

	barRect := image.Rect(dividerX-hitPad, 0, dividerX+dividerW+hitPad, gtx.Constraints.Max.Y)
	area := clip.Rect(barRect).Push(gtx.Ops)
	event.Op(gtx.Ops, t)
	pointer.CursorColResize.Add(gtx.Ops)
	area.Pop()

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: t,
			Kinds:  pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel,
		})
		if !ok {
			break
		}

		e, isPtr := ev.(pointer.Event)
		if !isPtr {
			continue
		}

		switch e.Kind {
		case pointer.Press:
			if t.drag {
				break
			}

			t.dragID = e.PointerID
			t.dragX = e.Position.X
			t.drag = true
		case pointer.Drag:
			if t.dragID != e.PointerID {
				break
			}

			if totalW > 0 {
				deltaX := e.Position.X - t.dragX
				t.ColumnRatio = ratio + deltaX/float32(totalW)
				t.ColumnRatio = max(min(t.ColumnRatio, overrideMaxRatio), overrideMinRatio)
			}

			t.dragX = e.Position.X
		case pointer.Release, pointer.Cancel:
			t.drag = false
		}
	}
}

// autoIndentAfterNewline checks if a newline was just inserted at the cursor
// position and, if so, inserts the same leading whitespace as the previous line.
// Returns true if indentation was inserted.
//
// Note: the byte-level scanning for '\n' and ' ' is safe because YAML
// indentation uses only ASCII characters (space + newline), so multi-byte
// runes never produce false positives for these byte values.
func autoIndentAfterNewline(editor *widget.Editor) bool {
	start, end := editor.Selection()
	if start != end || start == 0 {
		return false
	}

	edText := editor.Text()

	// Convert rune offset to byte offset by scanning forward, stopping
	// early once we reach the cursor position.
	byteOff := 0
	runeIdx := 0

	for byteOff < len(edText) && runeIdx < start {
		_, size := utf8.DecodeRuneInString(edText[byteOff:])
		byteOff += size
		runeIdx++
	}

	if byteOff <= 0 || edText[byteOff-1] != '\n' {
		return false
	}

	// Find the start of the previous line.
	prevLineStart := 0

	for i := byteOff - 2; i >= 0; i-- { //nolint:mnd // skip past the newline at byteOff-1
		if edText[i] == '\n' {
			prevLineStart = i + 1

			break
		}
	}

	// Count leading spaces of the previous line.
	spaces := 0

	for i := prevLineStart; i < byteOff-1; i++ {
		if edText[i] == ' ' {
			spaces++
		} else {
			break
		}
	}

	if spaces == 0 {
		return false
	}

	editor.Insert(strings.Repeat(" ", spaces))

	return true
}
