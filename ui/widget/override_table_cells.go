package widget

import (
	"image"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/state"
	"github.com/qdeck-app/qdeck/ui/theme"
)

// layoutEditorCell renders one override editor cell, drawing indent guides
// underneath for multi-line values. Factored out of layoutRightColumns so the
// cell body can be stacked with an anchor badge overlay without nesting.
// The editor is always editable — anchored cells keep a visible caret and
// allow selection; actual text mutations are caught in processEditorChanges
// and reverted with a warning dialog.
func (t *OverrideTable) layoutEditorCell(
	gtx layout.Context,
	col, entryIdx, rowIndex int,
	hint, entryType, entryKey string,
	entries []service.FlatValueEntry,
) layout.Dimensions {
	editors := t.ColumnEditors[col]

	// Nullify dispatch: a directly-nullified cell, or any cell masked by
	// a nullified ancestor section, renders as the "null" pill chip.
	// Replaces both the text editor and the bool switch — the underlying
	// editor still exists for state, but the pill is the only visible
	// affordance. Auto-clear on user keystrokes (processEditorChanges)
	// drops the flag so the cell re-renders as a normal editor next
	// frame; the in-cell nullify button is the primary toggle path.
	//
	// Any head comment the user typed (or that the loaded file carried
	// next to the `~` value) is surfaced above the pill so the "why
	// null?" explanation stays visible without un-nullifying first.
	if t.cellNullifiedCovered(col, entryKey) {
		return t.layoutNullCell(gtx, state.ExtractLeadingComment(editors[entryIdx].Text()))
	}

	// Number-typed cells get an input filter so non-numeric keystrokes are
	// dropped before they ever land in the editor — protects against the
	// "typed a letter into a port number, render fails at deploy time"
	// class of typo. Set per-frame because the editor instance is reused
	// across rows by the page state pool; cost is one string assignment.
	if isNumericType(entryType) && !strings.Contains(editors[entryIdx].Text(), "\n") {
		editors[entryIdx].Filter = numericInputFilter
	} else {
		editors[entryIdx].Filter = ""
	}

	// Bool dispatch: render an inline pill switch instead of the text
	// editor. The editor remains the source of truth — the switch reads
	// its current value from editor text and writes back via SetText on
	// toggle. Multi-line bool overrides (rare but possible if a YAML
	// scalar literal block is hand-pasted) fall through to the text path
	// to preserve fidelity. Skipped above when the cell is nullified, so
	// a covered descendant of a nullified section never shows a bool
	// toggle — only the null chip.
	if entryType == typeNameBool && !strings.Contains(editors[entryIdx].Text(), "\n") {
		return t.layoutBoolSwitchCell(gtx, col, entryIdx, rowIndex, entryKey, entries)
	}

	ed := material.Editor(t.Theme, &editors[entryIdx], hint)
	ed.TextSize = viewerEditorTextSize

	// Force the editor to fill its allocated cell width. widget.Editor sizes
	// its visible clip to gtx.Constraints.Constrain(textBoundingBox), so when
	// text is narrower than the cell the editor's hit clip is narrower too —
	// clicks in the empty horizontal space focus the row gesture but never
	// reach the editor's own clicker, so the caret doesn't move and the user
	// has to click again on real text. Multi-line rows make this routine on
	// Windows because there's more vertical space where a click can land in a
	// short line. Setting Min.X = Max.X expands the clip to the full column.
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	edText := editors[entryIdx].Text()

	if !strings.Contains(edText, "\n") {
		return LayoutEditor(gtx, t.Theme.Shaper, ed)
	}

	indent := service.DefaultYAMLIndent
	if t.ColumnStates[col] != nil {
		indent = t.ColumnStates[col].YAMLIndent()
	}

	// Record editor ops so guides paint underneath and the recorded editor ops
	// replay on top. The editor must be laid out first so Regions()/CaretPos()
	// return valid positions.
	macro := op.Record(gtx.Ops)
	dims := LayoutEditor(gtx, t.Theme.Shaper, ed)
	editorCall := macro.Stop()

	t.drawIndentGuides(gtx, edText, &editors[entryIdx], indent)
	editorCall.Add(gtx.Ops)

	return dims
}

// layoutBoolSwitchCell renders a pill switch for a bool-typed override
// cell. Click semantics:
//
//   - Locked alias cell: invokes OnAnchoredCellEdit (no write). Matches
//     the text-editor revert flow in processEditorChanges.
//   - Empty override (no value yet): the switch shows the chart default
//     read from t.DefaultValueEditors so the user sees the inherited
//     value, not a misleading "off". Toggling fills the override with
//     the new value.
//   - Otherwise: write the new value via SetText, drain the resulting
//     ChangeEvent so processEditorChanges doesn't double-fire OnChanged
//     next frame, then commit via the shared OnChanged path.
func (t *OverrideTable) layoutBoolSwitchCell(
	gtx layout.Context,
	col, entryIdx, rowIndex int,
	entryKey string,
	entries []service.FlatValueEntry,
) layout.Dimensions {
	editors := t.ColumnEditors[col]
	if entryIdx >= len(editors) {
		return layout.Dimensions{}
	}

	overrideText := editors[entryIdx].Text()

	current := ParseBoolValue(overrideText)
	if overrideText == "" && entryIdx < len(t.DefaultValueEditors) {
		current = ParseBoolValue(t.DefaultValueEditors[entryIdx].Text())
	}

	if rowIndex >= len(t.switchStates[col]) {
		// Defensive: ensureSwitchStates runs before per-row layout, so
		// this shouldn't fire — but guard rather than panic on a
		// malformed frame.
		return layout.Dimensions{}
	}

	// Capture the Flexed slot width up-front. LayoutSwitch reports just
	// the pill's 22dp natural width back to the parent Flex, which then
	// places the badge + nullify button immediately after the pill
	// (left side of the cell) instead of against the cell's right edge.
	// Padding the returned dim to the full slot width keeps the button
	// pinned to the right, matching string/number cells.
	slotW := gtx.Constraints.Max.X

	newValue, dims := LayoutSwitch(gtx, &t.switchStates[col][rowIndex], current)
	dims.Size.X = slotW

	if newValue == current {
		return dims
	}

	// Block edits to alias cells the same way processEditorChanges does
	// for the text-editor path. The unlock dialog handles the rest.
	if t.OnAnchoredCellEdit != nil && t.columnAnchorInfo(col, entryKey).Role == service.AnchorRoleAlias {
		t.OnAnchoredCellEdit(col, entryKey)

		return dims
	}

	editors[entryIdx].SetText(FormatBoolValue(newValue))
	t.drainEditorEvents(gtx, &editors[entryIdx])

	if t.ColumnStates[col] != nil && t.ColumnStates[col].IsNullifiedCovered(entryKey) {
		t.ColumnStates[col].ClearNullified(entryKey)
	}

	t.commitOverrideUpdate(gtx, col, entryIdx, entryKey, entries)

	return dims
}

// nullCellCommentGap is the vertical gap between the head comment and
// the null pill on a nullified cell. Tight — the two read as a single
// annotated affordance.
const nullCellCommentGap unit.Dp = 2

// layoutNullCell renders a nullified cell's right-side affordance.
// When the cell carries a head comment, it appears as italic muted
// text on the row above the null pill so the "why is this null?"
// explanation stays visible without un-nullifying. Empty-comment cells
// render just the pill.
//
// The reported dim.Size.X is padded to the Flexed slot width so the
// trailing anchor badge + nullify button stay pinned to the same
// x-coord they occupied before the cell was nullified. Without the
// pad, Gio's Flex would place those rigids immediately after the
// pill (which is much narrower than the slot), and the button would
// visibly "jump" leftward on click.
func (t *OverrideTable) layoutNullCell(gtx layout.Context, headComment string) layout.Dimensions {
	slotW := gtx.Constraints.Max.X
	compact := slotW < gtx.Dp(overrideNullifyCompactWidth)

	// Force the pill chrome to span the full Flexed slot so the cell's
	// visible content edge matches what an empty editor would draw —
	// the trailing badge + nullify button then land at the identical
	// x-coord whether the cell is in its empty or nullified state.
	gtx.Constraints.Min.X = slotW

	var dims layout.Dimensions

	if headComment == "" {
		dims = LayoutNullPill(gtx, t.Theme, compact)
	} else {
		dims = layout.Flex{Axis: layout.Vertical, Alignment: layout.Start}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutNullCellComment(gtx, t.Theme, headComment)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: nullCellCommentGap}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return LayoutNullPill(gtx, t.Theme, compact)
				})
			}),
		)
	}

	if dims.Size.X < slotW {
		dims.Size.X = slotW
	}

	return dims
}

// layoutNullCellComment renders the head-comment caption above the null
// pill — re-prefixes each line with `# ` so the visual matches how the
// comment would appear if the cell were edited as text.
func layoutNullCellComment(gtx layout.Context, th *material.Theme, head string) layout.Dimensions {
	var b strings.Builder

	lines := strings.Split(head, "\n")
	for i, line := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}

		b.WriteString("# ")
		b.WriteString(line)
	}

	lbl := material.Body2(th, b.String())
	lbl.Color = theme.Default.Muted
	lbl.Font.Style = font.Italic
	lbl.TextSize = viewerEditorTextSize
	lbl.MaxLines = len(lines)

	return LayoutLabel(gtx, lbl)
}

// layoutMissingDefault renders the negative-space placeholder for an
// extra (override-only) row's default cell. Fires for both leaf and
// section rows — sections have no value editor on the left, so the
// cell footprint is otherwise unused. The italic muted text is the
// load-bearing visual signal that the row has no chart default to fall
// back to — a typo in the override key would silently render as an
// extra row otherwise.
func layoutMissingDefault(gtx layout.Context, th *material.Theme) layout.Dimensions {
	lbl := material.Label(th, theme.Default.SizeSM, "not in chart")
	lbl.Color = theme.Default.Muted2
	lbl.Font.Style = font.Italic
	lbl.MaxLines = 1

	return LayoutLabel(gtx, lbl)
}

// layoutDefaultValue renders a read-only default value with a transparent
// editor overlay so the text supports selection and copy without becoming
// editable.
func layoutDefaultValue(gtx layout.Context, th *material.Theme, editor *widget.Editor) layout.Dimensions {
	// Render crisp text with our pixel-snapped label renderer.
	lbl := material.Body2(th, editor.Text())
	lbl.TextSize = viewerEditorTextSize
	lbl.Alignment = editor.Alignment

	m := op.Record(gtx.Ops)
	lblDims := LayoutLabel(gtx, lbl)
	lblCall := m.Stop()

	// Overlay a transparent editor to preserve text selection and copy.
	ed := material.Editor(th, editor, "")
	ed.TextSize = viewerEditorTextSize
	ed.Color = theme.Default.Transparent
	ed.Editor.SingleLine = false

	edM := op.Record(gtx.Ops)
	edDims := LayoutEditor(gtx, th.Shaper, ed)
	edCall := edM.Stop()

	lblCall.Add(gtx.Ops)
	edCall.Add(gtx.Ops)

	return layout.Dimensions{
		Size:     image.Pt(max(lblDims.Size.X, edDims.Size.X), max(lblDims.Size.Y, edDims.Size.Y)),
		Baseline: edDims.Baseline,
	}
}
