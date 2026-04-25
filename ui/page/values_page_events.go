package page

import (
	"strings"
	"unicode/utf8"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/widget"

	"github.com/qdeck-app/qdeck/domain"
	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/state"
)

// onCommentChanged harvests the comment editors in the given column back into
// its CustomValues banner / trailer / foot-comment fields and marks the column
// modified so save picks up the changes. Comment edits don't go through the
// editor-parse runner (which would clobber FootComments by re-parsing values
// only); instead they update the CustomValues struct directly.
//
// Banner vs. trailer is identified positionally, matching commentInitialText:
// the first comment row before any non-comment entry is the banner, comment
// rows after at least one non-comment entry are the trailer. Foot-block rows
// carry their FootAfterKey so they map straight back into the FootComments map.
//
// All editor texts are formatted via FormatCommentForYAML so the stored value
// is the "# "-prefixed verbatim form yaml.v3 emits — the same shape
// parseOrphanComments captured at load time, ensuring the round-trip stays
// stable across save → reload.
func (p *ValuesPage) onCommentChanged(colIdx int) {
	if colIdx < 0 || colIdx >= state.MaxCustomColumns {
		return
	}

	col := &p.State.Columns[colIdx]
	if col.CustomValues == nil {
		return
	}

	editors := col.OverrideEditors
	entries := p.State.Entries

	var (
		bannerText   string
		trailerText  string
		foots        map[string]string
		sectionHeads map[string]string
	)

	seenNonComment := false

	for i, entry := range entries {
		if i >= len(editors) {
			continue
		}

		text := editors[i].Text()

		switch {
		case entry.IsSection():
			seenNonComment = true

			if text == "" {
				continue
			}

			if sectionHeads == nil {
				sectionHeads = make(map[string]string)
			}

			sectionHeads[entry.Key] = service.FormatCommentForYAML(text)
		case !entry.IsComment():
			seenNonComment = true
		case entry.FootAfterKey != "":
			if text == "" {
				continue
			}

			if foots == nil {
				foots = make(map[string]string)
			}

			foots[entry.FootAfterKey] = service.FormatCommentForYAML(text)
		case !seenNonComment:
			bannerText = service.FormatCommentForYAML(text)
		default:
			trailerText = service.FormatCommentForYAML(text)
		}
	}

	col.CustomValues.DocHeadComment = bannerText
	col.CustomValues.DocFootComment = trailerText
	col.CustomValues.FootComments = foots
	col.CustomValues.SectionHeads = sectionHeads
	col.ValuesModified = true
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
		p.State.FocusHighlightAttempts = 0
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

		if !entries[i].IsFocusable() {
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
		if !entries[i].IsFocusable() {
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
		// keys and unfold it via Cmd+/. Comment rows are skipped — they have
		// no editor and no chevron, so landing on one would either lose focus
		// silently or feel broken.
		if entry.IsFocusable() || (entry.IsSection() && p.State.CollapsedKeys[entry.Key]) {
			break
		}
	}

	p.State.FocusedRow = row
	p.State.FocusedCol = col

	entryIdx := filtered[row]

	if !entries[entryIdx].IsFocusable() {
		// Section rows (and any other non-focusable row that ended the loop —
		// only collapsed sections do) have no visible editor; release keyboard
		// focus so the previous editor doesn't keep receiving keystrokes.
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

	if idx := filtered[row]; idx >= len(entries) || !entries[idx].IsFocusable() {
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
				if idx < len(entries) && entries[idx].IsFocusable() {
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
				if idx < len(entries) && entries[idx].IsFocusable() {
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
