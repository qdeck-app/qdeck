package widget

import (
	"image"

	"gioui.org/gesture"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/theme"
)

// commentSourceCol is the column whose editor pool backs comment-row text.
// V1 always uses 0 — comments are sourced from the first loaded custom file.
const commentSourceCol = 0

// layoutCommentRow paints an orphan-comment row from the user's custom values
// file. Comment rows live in the same lane as the override editor (right side
// of the table) so they read as the user's own annotations on the values
// file, not as commentary mixed into the chart-defaults view. The left panel
// (key + default value) stays empty — comment rows have no underlying leaf.
//
// Foot-block rows clamp to overrideCommentMaxLines so a 20-line YAML example
// commented out for documentation can't dominate the table; banner and
// trailer rows render unclamped because the user explicitly typed them at
// file scope and wants the full text visible. ShowDocs is NOT consulted —
// these are first-class user annotations on their own file, not chart-side
// documentation noise.
//
// The comment editor lives at columnEditors[sourceCol][entryIdx]. The slot is
// otherwise unused (comment rows have no value), so reusing it avoids a
// parallel editor pool. Source column is currently always 0 — multi-column
// comment lanes are a follow-up.
func (t *OverrideTable) layoutCommentRow(
	gtx layout.Context, index int, entryIdx int, entry service.FlatValueEntry,
) layout.Dimensions {
	hovered := t.hovers[index].Update(gtx.Source)
	if hovered {
		t.HoveredRow = index
	} else if t.HoveredRow == index {
		t.HoveredRow = overrideNoHover
	}

	indent := overrideIndentPerLevel * unit.Dp(max(0, entry.Depth-1))
	totalW := gtx.Constraints.Max.X

	ratio := t.ratio()
	dividerW := gtx.Dp(overrideDividerW)
	leftW := int(ratio * float32(totalW))
	rightW := max(totalW-leftW-dividerW, 0)

	maxLines := 0
	if entry.FootAfterKey != "" {
		maxLines = overrideCommentMaxLines
	}

	t.processCommentEditorChange(gtx, entryIdx)

	// Record content so backgrounds/dividers paint underneath the editor text.
	m := op.Record(gtx.Ops)

	dims := layout.Inset{Top: overridePaddingV, Bottom: overridePaddingV}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				// Left panel: empty. The comment doesn't belong to any leaf
				// key, so the key + default value cells are blank. Reserving
				// the width keeps the right-side comment aligned with leaf
				// override editors directly above and below.
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(leftW, 0)}
				}),
				// Vertical divider matching leaf rows.
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{Size: image.Pt(dividerW, 0)}
				}),
				// Right panel: comment editor, indented to the surrounding
				// leaf's depth so the foot-block visually trails the leaf
				// it annotates.
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = rightW
					gtx.Constraints.Max.X = rightW

					return layout.Inset{
						Left:  overridePaddingH + indent,
						Right: overridePaddingH,
					}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return t.layoutCommentEditor(gtx, entryIdx, maxLines)
					})
				}),
			)
		})

	if dims.Size.X < totalW {
		dims.Size.X = totalW
	}

	c := m.Stop()

	defer clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops).Pop()

	// Register hover + click slots so the per-row buffers stay aligned with
	// FilteredIndices. Click on the right panel focuses the comment editor.
	t.hovers[index].Add(gtx.Ops)
	t.cellClicks[index].Add(gtx.Ops)

	t.handleCommentRowClick(gtx, index, entryIdx, leftW)

	c.Add(gtx.Ops)

	// Vertical divider painted on top of the recorded content (matches
	// drawRowDecorations on leaf rows but inlined since we don't need the
	// sub-column dividers / tree guides). Uses Guide (paper-thin) rather
	// than Border so the grid feels light, per the design spec.
	divLine := clip.Rect{
		Min: image.Pt(leftW, 0),
		Max: image.Pt(leftW+dividerW, dims.Size.Y),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.Default.Guide}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	divLine.Pop()

	// Horizontal separator at the bottom of the row.
	separatorH := gtx.Dp(overrideSeparatorH)

	sep := clip.Rect{
		Min: image.Pt(0, dims.Size.Y-separatorH),
		Max: image.Pt(totalW, dims.Size.Y),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.Default.Guide}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	sep.Pop()

	return dims
}

// layoutCommentEditor paints the editor for a comment row. maxLines is
// currently advisory only — the underlying widget.Editor in this Gio version
// doesn't expose a per-instance line clamp, so foot-block rows rely on the
// caller's typical foot-block content (a few lines) being short enough not
// to dominate; banner/trailer rows render unclamped by design. _ is
// reserved for a future clamp once we add a line-aware editor wrapper.
func (t *OverrideTable) layoutCommentEditor(gtx layout.Context, entryIdx, _ int) layout.Dimensions {
	editors := t.ColumnEditors[commentSourceCol]
	if entryIdx >= len(editors) {
		return layout.Dimensions{}
	}

	editors[entryIdx].SingleLine = false

	ed := material.Editor(t.Theme, &editors[entryIdx], "")
	ed.Color = theme.Default.Muted
	ed.TextSize = theme.Default.SizeXL

	return LayoutEditor(gtx, t.Theme.Shaper, ed)
}

// processCommentEditorChange drains pending change events on the comment
// editor for entryIdx and fires OnChanged once if any landed. Mirrors the
// shape of processEditorChanges but skips the anchor / autoindent / override
// flag bookkeeping that's leaf-specific.
func (t *OverrideTable) processCommentEditorChange(gtx layout.Context, entryIdx int) {
	editors := t.ColumnEditors[commentSourceCol]
	if entryIdx >= len(editors) {
		return
	}

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

	if !changed || t.OnCommentChanged == nil {
		return
	}

	t.OnCommentChanged(commentSourceCol)
}

// handleCommentRowClick focuses the comment editor when the user clicks
// inside the right panel, matching how leaf rows put a text cursor at the
// click point.
func (t *OverrideTable) handleCommentRowClick(gtx layout.Context, index, entryIdx, leftW int) {
	for {
		ev, ok := t.cellClicks[index].Update(gtx.Source)
		if !ok {
			break
		}

		if ev.Kind != gesture.KindPress {
			continue
		}

		if ev.Position.X < leftW {
			continue
		}

		editors := t.ColumnEditors[commentSourceCol]
		if entryIdx < len(editors) {
			gtx.Execute(key.FocusCmd{Tag: &editors[entryIdx]})
		}
	}
}
