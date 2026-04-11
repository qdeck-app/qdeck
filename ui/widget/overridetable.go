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
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/domain"
	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/state"
	"github.com/qdeck-app/qdeck/ui/theme"
)

const (
	overrideDefaultRatio         = 0.6
	overrideMinRatio     float32 = 0.2
	overrideMaxRatio     float32 = 0.85

	overrideKeyProportion   = 0.5
	overrideValueProportion = 0.5

	overridePaddingV       unit.Dp = 4
	overridePaddingH       unit.Dp = 8
	overrideIndentPerLevel unit.Dp = 12
	overrideStickyPaddingV unit.Dp = 6
	overrideScrollbarWidth unit.Dp = 10 // MinorWidth(6) + 2*MinorPadding(2)
	overrideSeparatorH     unit.Dp = 1
	overrideTreeGuideW     unit.Dp = 1
	overrideDividerW       unit.Dp = 2
	overrideSubDividerW    unit.Dp = 1
	overrideNoHover                = -1
	overrideMarkerW        unit.Dp = 4
	overrideMarkerMinH     unit.Dp = 2
	overrideIndentGuideW   unit.Dp = 1
	overrideIndentDotLen   unit.Dp = 2
	overrideIndentDotGap   unit.Dp = 3
)

// OverrideTable renders a unified table with default values on the left and
// editable override editors on the right. Supports up to MaxCustomColumns
// independent editor columns side by side. Uses a single virtualized list so
// that row heights always match. A draggable vertical divider separates the
// default values from the override columns.
type OverrideTable struct {
	Theme *material.Theme
	List  *widget.List

	// DefaultValueEditors holds read-only editors for default value cells,
	// enabling text selection and copy. Set by the page before Layout each frame.
	DefaultValueEditors []widget.Editor

	// ColumnEditors holds editor slices for each active column.
	// Set by the page before Layout each frame (slice header copies, no alloc).
	ColumnEditors [state.MaxCustomColumns][]widget.Editor

	// ColumnStates provides access to cached override flags per column.
	// Set by the page before Layout each frame (pointer copies, no alloc).
	ColumnStates [state.MaxCustomColumns]*state.CustomColumnState

	// ColumnCount is the number of active override columns (1-3).
	ColumnCount int

	// ColumnRatio controls the left proportion (0..1). Defaults to overrideDefaultRatio.
	ColumnRatio float32

	// ShowComments controls whether comment lines above default value entries are displayed.
	ShowComments bool

	hovers     []gesture.Hover
	cellClicks []gesture.Click
	HoveredRow int

	// Column resize drag state (same pattern as SplitView).
	drag   bool
	dragID pointer.ID
	dragX  float32

	// Reusable scratch buffers for indent guide rendering (no per-frame alloc).
	igDepths  []int
	igGuides  []indentGuide
	igYStart  []int
	igYEnd    []int
	igRegions []widget.Region // shared across measureIndentWidth/fillLineY calls within one drawIndentGuides pass

	// OnChanged fires when any override editor text changes.
	// The callback receives the column index, the generated YAML string,
	// and an error if YAML serialization failed.
	OnChanged func(colIdx int, yamlText string, err error)

	// OnCellFocused fires when a cell is clicked/focused.
	// The callback receives the visible row index and column index.
	OnCellFocused func(row, col int)

	// OnKeyCopied fires when a key path is copied to the clipboard via left-cell click.
	OnKeyCopied func(key string)
}

func (t *OverrideTable) ensureHovers(count int) {
	if count > len(t.hovers) {
		t.hovers = append(t.hovers, make([]gesture.Hover, count-len(t.hovers))...)
	}
}

func (t *OverrideTable) ensureCellClicks(count int) {
	if count > len(t.cellClicks) {
		t.cellClicks = append(t.cellClicks, make([]gesture.Click, count-len(t.cellClicks))...)
	}
}

func (t *OverrideTable) ratio() float32 {
	if t.ColumnRatio <= 0 {
		return overrideDefaultRatio
	}

	return t.ColumnRatio
}

func (t *OverrideTable) colCount() int {
	if t.ColumnCount < 1 {
		return 1
	}

	return t.ColumnCount
}

// colGeometry holds the computed sub-column layout metrics for the right panel.
type colGeometry struct {
	count      int
	leftW      int
	dividerW   int
	subDivW    int
	totalDivW  int
	colW       int
	rightStart int
}

// columnGeometry computes sub-column widths and positions for the right panel.
func columnGeometry(gtx layout.Context, leftW, dividerW, rightW, colCount int) colGeometry {
	subDivW := gtx.Dp(overrideSubDividerW)
	totalDivW := subDivW * (colCount - 1)
	colW := max((rightW-totalDivW)/colCount, 0)

	return colGeometry{
		count:      colCount,
		leftW:      leftW,
		dividerW:   dividerW,
		subDivW:    subDivW,
		totalDivW:  totalDivW,
		colW:       colW,
		rightStart: leftW + dividerW,
	}
}

// overrideHint returns the editor placeholder for the given value type.
func overrideHint(entryType string) string {
	switch entryType {
	case "string":
		return "click to override (string)"
	case "number":
		return "click to override (number)"
	case "bool":
		return "click to override (bool)"
	case "null":
		return "click to override (null)"
	case "unknown":
		return "click to override (unknown)"
	default:
		return "click to override"
	}
}

// Layout renders the unified table with key, default value, and override editor columns.
func (t *OverrideTable) Layout(
	gtx layout.Context,
	entries []service.FlatValueEntry,
	filteredIndices []int,
) layout.Dimensions {
	t.List.Axis = layout.Vertical
	t.ensureHovers(len(filteredIndices))
	t.ensureCellClicks(len(filteredIndices))

	t.HoveredRow = overrideNoHover

	t.handleDrag(gtx)

	parent := t.stickyParent(entries, filteredIndices)

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return material.List(t.Theme, t.List).Layout(gtx, len(filteredIndices),
				func(gtx layout.Context, index int) layout.Dimensions {
					return t.layoutRow(gtx, entries, filteredIndices, index)
				})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			if parent == "" {
				return layout.Dimensions{}
			}

			return t.layoutStickyHeader(gtx, parent)
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			t.layoutScrollbarMarkers(gtx, entries, filteredIndices)

			return layout.Dimensions{}
		}),
	)
}

func (t *OverrideTable) layoutRow(
	gtx layout.Context,
	entries []service.FlatValueEntry,
	filteredIndices []int,
	index int,
) layout.Dimensions {
	if index >= len(filteredIndices) {
		return layout.Dimensions{}
	}

	entryIdx := filteredIndices[index]
	if entryIdx >= len(entries) {
		return layout.Dimensions{}
	}

	entry := entries[entryIdx]
	indent := overrideIndentPerLevel * unit.Dp(max(0, entry.Depth-1))
	section := entry.IsSection()

	hovered := t.hovers[index].Update(gtx.Source)
	if hovered {
		t.HoveredRow = index
	} else if t.HoveredRow == index {
		t.HoveredRow = overrideNoHover
	}

	keyText := lastSegment(entry.Key)
	if hovered {
		keyText = entry.Key
	}

	t.processEditorChanges(gtx, entries, entryIdx, section)

	// Record content for z-order.
	m := op.Record(gtx.Ops)

	ratio := t.ratio()
	dividerW := gtx.Dp(overrideDividerW)
	totalW := gtx.Constraints.Max.X
	leftW := int(ratio * float32(totalW))
	rightW := max(totalW-leftW-dividerW, 0)

	g := columnGeometry(gtx, leftW, dividerW, rightW, t.colCount())

	dims := layout.Inset{
		Top: overridePaddingV, Bottom: overridePaddingV,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// Comment above entry (optional), constrained to left panel width.
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if entry.Comment == "" || !t.ShowComments {
					return layout.Dimensions{}
				}

				gtx.Constraints.Max.X = leftW

				return layout.Inset{Left: overridePaddingH}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: indent}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								lbl := material.Caption(t.Theme, entry.Comment)
								lbl.Color = theme.ColorMuted

								return LayoutLabel(gtx, lbl)
							})
					})
			}),
			// Main row: left (key + value) | divider | right (override columns)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx,
					// Left portion: key + default value
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = leftW
						gtx.Constraints.Max.X = leftW

						return layout.Inset{Left: overridePaddingH, Right: overridePaddingH}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{}.Layout(gtx,
									layout.Flexed(overrideKeyProportion, func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: indent}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											displayKey := keyText
											if section {
												displayKey += ":"
											}

											lbl := material.Body2(t.Theme, displayKey)
											lbl.MaxLines = 1

											return LayoutLabel(gtx, lbl)
										})
									}),
									layout.Flexed(overrideValueProportion, func(gtx layout.Context) layout.Dimensions {
										if entryIdx >= len(t.DefaultValueEditors) {
											return layout.Dimensions{}
										}

										return layoutDefaultValue(gtx, t.Theme, &t.DefaultValueEditors[entryIdx])
									}),
								)
							})
					}),
					// Main divider
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: image.Pt(dividerW, 0)}
					}),
					// Right portion: override editor sub-columns
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = rightW
						gtx.Constraints.Max.X = rightW

						if section {
							return layout.Dimensions{Size: image.Pt(rightW, 0)}
						}

						return t.layoutRightColumns(gtx, entryIdx, g, entry.Type)
					}),
				)
			}),
		)
	})

	c := m.Stop()

	// Clip rect for entire row.
	defer clip.Rect(image.Rectangle{Max: dims.Size}).Push(gtx.Ops).Pop()

	// Register hover gesture.
	t.hovers[index].Add(gtx.Ops)

	// Register click gesture for the right cell and set text cursor.
	t.cellClicks[index].Add(gtx.Ops)

	if !section {
		t.handleCellClick(gtx, index, entryIdx, entry.Key, g, rightW, dims.Size.Y)
	}

	// Override highlight or hover background.
	hasOverride := !section && t.hasAnyOverride(entryIdx)

	var gitStatus domain.GitChangeStatus
	if !section {
		gitStatus = t.gitChangeStatus(entries[entryIdx].Key)
	}

	switch {
	case hasOverride:
		paintRowBg(gtx, dims.Size.Y, theme.ColorOverride)
	case gitStatus == domain.GitAdded:
		paintRowBg(gtx, dims.Size.Y, theme.ColorGitAdded)
	case gitStatus == domain.GitModified:
		paintRowBg(gtx, dims.Size.Y, theme.ColorGitModified)
	case hovered:
		paintRowBg(gtx, dims.Size.Y, theme.ColorHover)
	}

	// Replay content.
	c.Add(gtx.Ops)

	// Row decorations: divider, sub-column dividers, tree guides, separator.
	t.drawRowDecorations(gtx, g, entry, dims, totalW)

	// Git change indicator bar on the override cell's left edge.
	if gitStatus != domain.GitUnchanged {
		barColor := theme.ColorGitAddedBar
		if gitStatus == domain.GitModified {
			barColor = theme.ColorGitModifiedBar
		}

		paintGitIndicator(gtx, g.rightStart, dims.Size.Y, barColor)
	}

	return dims
}

// layoutRightColumns renders the editor sub-columns in the right panel.
func (t *OverrideTable) layoutRightColumns(
	gtx layout.Context,
	entryIdx int,
	g colGeometry,
	entryType string,
) layout.Dimensions {
	rightW := g.colW*g.count + g.totalDivW

	// Use fixed-size array to avoid per-frame allocation.
	var children [state.MaxCustomColumns * 2]layout.FlexChild

	n := 0
	hint := overrideHint(entryType)

	for c := range g.count {
		col := c

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			w := g.colW
			// Give any remainder pixels to the last column.
			if col == g.count-1 {
				w = rightW - g.totalDivW - g.colW*(g.count-1)
			}

			gtx.Constraints.Min.X = w
			gtx.Constraints.Max.X = w

			editors := t.ColumnEditors[col]
			if entryIdx >= len(editors) {
				return layout.Dimensions{Size: image.Pt(w, 0)}
			}

			return layout.Inset{Left: overridePaddingH}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					ed := material.Editor(t.Theme, &editors[entryIdx], hint)
					ed.TextSize = viewerEditorTextSize

					// Draw indent ruler ticks for multi-line cells.
					edText := editors[entryIdx].Text()

					if strings.Contains(edText, "\n") {
						indent := service.DefaultYAMLIndent
						if t.ColumnStates[col] != nil {
							indent = t.ColumnStates[col].YAMLIndent()
						}

						// Record editor ops so we can draw guides behind the text.
						// The editor must be laid out first so Regions()/CaretPos()
						// return valid positions; then guides paint underneath, and
						// the recorded editor ops replay on top.
						macro := op.Record(gtx.Ops)
						dims := LayoutEditor(gtx, t.Theme.Shaper, ed)
						editorCall := macro.Stop()

						t.drawIndentGuides(gtx, edText, &editors[entryIdx], indent)
						editorCall.Add(gtx.Ops)

						return dims
					}

					return LayoutEditor(gtx, t.Theme.Shaper, ed)
				})
		})
		n++

		// Sub-divider between columns (not after the last).
		if c < g.count-1 {
			children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(g.subDivW, 0)}
			})
			n++
		}
	}

	return layout.Flex{}.Layout(gtx, children[:n]...)
}

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

		changed := false
		lenBefore := len(editors[entryIdx].Text())

		for {
			ev, ok := editors[entryIdx].Update(gtx)
			if !ok {
				break
			}

			if _, isChange := ev.(widget.ChangeEvent); isChange {
				changed = true
			}
		}

		if changed {
			// Quick pre-filter: only attempt auto-indent when exactly one
			// byte was added (not paste or deletion). The actual newline
			// check happens inside autoIndentAfterNewline.
			if len(editors[entryIdx].Text()) == lenBefore+1 && autoIndentAfterNewline(&editors[entryIdx]) {
				// Drain the ChangeEvent produced by the indent insertion.
				for {
					_, ok := editors[entryIdx].Update(gtx)
					if !ok {
						break
					}
				}
			}

			if t.ColumnStates[c] != nil {
				t.ColumnStates[c].MarkOverride(entryIdx, state.StripYAMLComments(editors[entryIdx].Text()) != "")
			}

			if t.OnChanged != nil {
				indent := service.DefaultYAMLIndent
				if t.ColumnStates[c] != nil {
					indent = t.ColumnStates[c].YAMLIndent()
				}

				yamlText, yamlErr := state.OverridesToYAML(entries, editors, indent)
				t.OnChanged(c, yamlText, yamlErr)
			}
		}
	}
}

// handleCellClick focuses the correct column editor when a right-cell click occurs,
// or copies the field key to clipboard when the left key area is clicked.
func (t *OverrideTable) handleCellClick(
	gtx layout.Context,
	index int,
	entryIdx int,
	entryKey string,
	g colGeometry,
	rightW int,
	rowH int,
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
		if clickX < g.rightStart {
			// Left-side click: copy the full key path to clipboard.
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

	// Show pointer cursor over the left key area (click-to-copy).
	keyW := g.leftW / 2 //nolint:mnd // key column is half of the left panel
	keyArea := clip.Rect{Max: image.Pt(keyW, rowH)}.Push(gtx.Ops)
	pointer.CursorPointer.Add(gtx.Ops)
	keyArea.Pop()

	// Show text cursor over the right cell area.
	rightArea := clip.Rect{
		Min: image.Pt(g.rightStart, 0),
		Max: image.Pt(g.rightStart+rightW, rowH),
	}.Push(gtx.Ops)
	pointer.CursorText.Add(gtx.Ops)
	rightArea.Pop()
}

// drawRowDecorations renders the divider line, sub-column dividers, tree guides,
// and horizontal separator for a single row.
func (t *OverrideTable) drawRowDecorations(
	gtx layout.Context,
	g colGeometry,
	entry service.FlatValueEntry,
	dims layout.Dimensions,
	totalW int,
) {
	rowH := dims.Size.Y

	// Main vertical divider.
	divLine := clip.Rect{
		Min: image.Pt(g.leftW, 0),
		Max: image.Pt(g.leftW+g.dividerW, rowH),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	divLine.Pop()

	// Sub-column dividers.
	if g.count > 1 {
		for c := 1; c < g.count; c++ {
			x := g.rightStart + c*g.colW + (c-1)*g.subDivW

			line := clip.Rect{
				Min: image.Pt(x, 0),
				Max: image.Pt(x+g.subDivW, rowH),
			}.Push(gtx.Ops)
			paint.ColorOp{Color: theme.ColorTreeGuide}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			line.Pop()
		}
	}

	// Tree guide lines.
	guideW := gtx.Dp(overrideTreeGuideW)

	for d := 1; d < entry.Depth; d++ {
		x := gtx.Dp(overridePaddingH + unit.Dp(d-1)*overrideIndentPerLevel + overrideIndentPerLevel/2)

		guide := clip.Rect{
			Min: image.Pt(x, 0),
			Max: image.Pt(x+guideW, rowH),
		}.Push(gtx.Ops)
		paint.ColorOp{Color: theme.ColorTreeGuide}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		guide.Pop()
	}

	// Horizontal separator.
	separatorH := gtx.Dp(overrideSeparatorH)

	sep := clip.Rect{
		Min: image.Pt(0, rowH-separatorH),
		Max: image.Pt(totalW, rowH),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	sep.Pop()
}

// hasAnyOverride returns true if any column has a non-empty editor for the given entry.
func (t *OverrideTable) hasAnyOverride(entryIdx int) bool {
	for c := range t.colCount() {
		if t.ColumnStates[c] != nil && t.ColumnStates[c].HasOverrideAt(entryIdx) {
			return true
		}
	}

	return false
}

// gitChangeStatus returns the highest-priority git change status for the given flat key
// across all active columns. GitModified takes precedence over GitAdded.
func (t *OverrideTable) gitChangeStatus(key string) domain.GitChangeStatus {
	best := domain.GitUnchanged

	for c := range t.colCount() {
		if t.ColumnStates[c] != nil && t.ColumnStates[c].GitChanges != nil {
			if status, ok := t.ColumnStates[c].GitChanges[key]; ok {
				if status == domain.GitModified {
					return domain.GitModified
				}

				if status > best {
					best = status
				}
			}
		}
	}

	return best
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

// stickyParent returns the parent key path for the first visible entry.
func (t *OverrideTable) stickyParent(entries []service.FlatValueEntry, filteredIndices []int) string {
	first := t.List.Position.First
	if first < 0 || first >= len(filteredIndices) {
		return ""
	}

	entryIdx := filteredIndices[first]
	if entryIdx >= len(entries) {
		return ""
	}

	return parentPath(entries[entryIdx].Key)
}

func (t *OverrideTable) layoutStickyHeader(gtx layout.Context, parent string) layout.Dimensions {
	// Leave space on the right for the scrollbar so the header does not overlap it.
	scrollW := gtx.Dp(overrideScrollbarWidth)
	headerW := max(gtx.Constraints.Max.X-scrollW, 0)

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			rect := clip.Rect{Max: image.Pt(headerW, gtx.Constraints.Min.Y)}.Push(gtx.Ops)
			paint.ColorOp{Color: theme.ColorStickyHeader}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			rect.Pop()

			return layout.Dimensions{Size: image.Pt(headerW, gtx.Constraints.Min.Y)}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Max.X = headerW

			return layout.Inset{
				Top: overrideStickyPaddingV, Bottom: overrideStickyPaddingV,
				Left: overridePaddingH, Right: overridePaddingH,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Body2(t.Theme, parent)
				lbl.Color = theme.ColorSecondary
				lbl.MaxLines = 1

				return LayoutLabel(gtx, lbl)
			})
		}),
	)
}

// indentGuide describes one vertical guide line: its indent level and
// the range of text lines it spans.
// layoutDefaultValue renders a read-only default value using our label
// renderer (256-glyph batch) for correct anti-aliasing.
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
	ed.Color = theme.ColorTransparent
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

type indentGuide struct {
	level     int // indent depth (1-based)
	firstLine int // first text line at this depth (0-based)
	lastLine  int // last text line at this depth (0-based)
}

// ensureIndentBufs grows scratch buffers to hold at least n lines and
// maxDepth guide descriptors.
func (t *OverrideTable) ensureIndentBufs(n, maxGuides int) {
	if cap(t.igDepths) < n {
		t.igDepths = make([]int, n)
		t.igYStart = make([]int, n)
		t.igYEnd = make([]int, n)
	}

	t.igDepths = t.igDepths[:n]
	t.igYStart = t.igYStart[:n]
	t.igYEnd = t.igYEnd[:n]

	if cap(t.igGuides) < maxGuides {
		t.igGuides = make([]indentGuide, 0, maxGuides)
	}

	t.igGuides = t.igGuides[:0]
}

// drawIndentGuides draws vertical indentation guide lines in a multi-line
// editor cell. Performs a single pass over the text to compute indent depths,
// line Y positions, and guide ranges, then draws dotted lines.
// Uses pre-allocated scratch buffers to avoid per-frame allocations.
func (t *OverrideTable) drawIndentGuides(gtx layout.Context, edText string, editor *widget.Editor, indentUnit int) {
	numLines := strings.Count(edText, "\n") + 1
	if numLines <= 1 || indentUnit == 0 {
		return
	}

	totalRunes := utf8.RuneCountInString(edText)

	// Pre-allocate buffers. Use a reasonable initial guide capacity
	// (indent depth rarely exceeds 8 in YAML).
	const defaultMaxGuides = 8
	t.ensureIndentBufs(numLines, defaultMaxGuides)

	depths := t.igDepths
	lineYStart := t.igYStart
	lineYEnd := t.igYEnd

	// Single pass: scan text to compute depths and Y positions together.
	pxPerLevel := 0
	maxDepth := 0
	lineIdx := 0
	runeOff := 0
	lineRuneStart := 0
	spaces := 0
	counting := true
	hasContent := false

	for byteIdx := 0; byteIdx < len(edText); {
		r, size := utf8.DecodeRuneInString(edText[byteIdx:])
		byteIdx += size

		if r == '\n' {
			// Compute depth.
			if !hasContent && lineIdx > 0 {
				depths[lineIdx] = depths[lineIdx-1]
			} else {
				depths[lineIdx] = spaces / indentUnit
			}

			if depths[lineIdx] > maxDepth {
				maxDepth = depths[lineIdx]
			}

			// Measure pixel width from the first indented line.
			if pxPerLevel == 0 && spaces >= indentUnit {
				pxPerLevel = t.measureIndentWidth(editor, lineRuneStart, indentUnit)
			}

			// Compute Y position for this line.
			t.fillLineY(editor, lineIdx, lineRuneStart, runeOff, totalRunes, lineYStart, lineYEnd)

			lineIdx++
			runeOff++ // account for the '\n' rune
			lineRuneStart = runeOff
			spaces = 0
			counting = true
			hasContent = false

			continue
		}

		if counting {
			if r == ' ' {
				spaces++
			} else {
				counting = false
				hasContent = true
			}
		} else {
			hasContent = true
		}

		runeOff++
	}

	// Process the final line.
	if lineIdx < numLines {
		if !hasContent && lineIdx > 0 {
			depths[lineIdx] = depths[lineIdx-1]
		} else {
			depths[lineIdx] = spaces / indentUnit
		}

		if depths[lineIdx] > maxDepth {
			maxDepth = depths[lineIdx]
		}

		if pxPerLevel == 0 && spaces >= indentUnit {
			pxPerLevel = t.measureIndentWidth(editor, lineRuneStart, indentUnit)
		}

		t.fillLineY(editor, lineIdx, lineRuneStart, runeOff, totalRunes, lineYStart, lineYEnd)
	}

	if pxPerLevel == 0 || maxDepth == 0 {
		return
	}

	// Build guide descriptors: for each indent level, find the first and
	// last line at or deeper than that level.
	for level := 1; level <= maxDepth; level++ {
		first, last := -1, -1

		for i, d := range depths[:numLines] {
			if d >= level {
				if first == -1 {
					first = i
				}

				last = i
			}
		}

		if first != -1 {
			t.igGuides = append(t.igGuides, indentGuide{level: level, firstLine: first, lastLine: last})
		}
	}

	if len(t.igGuides) == 0 {
		return
	}

	// Find the cursor position to hide the guide at the cursor's indent column
	// on the cursor line, so the caret stays visible.
	caretLine, caretCol := editor.CaretPos()
	// Integer division: hides the guide when the caret falls anywhere
	// within that indent level's column span, keeping the caret area clear.
	caretIndentLevel := caretCol / indentUnit

	guideW := gtx.Dp(overrideIndentGuideW)
	dotLen := gtx.Dp(overrideIndentDotLen)
	dotGap := gtx.Dp(overrideIndentDotGap)
	dotStep := dotLen + dotGap

	if dotStep <= 0 {
		dotStep = 1
	}

	for _, g := range t.igGuides {
		x := g.level * pxPerLevel
		yMin := lineYStart[g.firstLine]
		yMax := lineYEnd[g.lastLine]

		if yMax <= yMin {
			continue
		}

		// Only skip the guide that matches the cursor's indent column on the cursor line.
		skipCursorLine := g.level == caretIndentLevel &&
			caretLine >= g.firstLine && caretLine <= g.lastLine

		// Draw dotted line.
		for y := yMin; y < yMax; y += dotStep {
			segEnd := y + dotLen
			if segEnd > yMax {
				segEnd = yMax
			}

			if skipCursorLine {
				cursorYMin := lineYStart[caretLine]
				cursorYMax := lineYEnd[caretLine]

				if segEnd > cursorYMin && y < cursorYMax {
					continue
				}
			}

			dot := clip.Rect{
				Min: image.Pt(x, y),
				Max: image.Pt(x+guideW, segEnd),
			}.Push(gtx.Ops)
			paint.ColorOp{Color: theme.ColorIndentTick}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			dot.Pop()
		}
	}
}

// measureIndentWidth returns the pixel width of indentUnit spaces starting at
// lineRuneStart, using editor.Regions. Returns 0 if the region cannot be measured.
func (t *OverrideTable) measureIndentWidth(editor *widget.Editor, lineRuneStart, indentUnit int) int {
	t.igRegions = editor.Regions(lineRuneStart, lineRuneStart+indentUnit, t.igRegions)
	if len(t.igRegions) > 0 {
		return t.igRegions[0].Bounds.Max.X - t.igRegions[0].Bounds.Min.X
	}

	return 0
}

// fillLineY computes the Y start/end for a single text line using editor.Regions.
// For empty lines (or the first line when empty), it uses the nearest non-empty
// line's height to avoid drift from compounding estimates. When no reference
// line exists (e.g. first line is empty), it queries the editor for a fallback
// line height.
func (t *OverrideTable) fillLineY(
	editor *widget.Editor,
	lineIdx, lineRuneStart, lineRuneEnd, totalRunes int,
	lineYStart, lineYEnd []int,
) {
	regionEnd := lineRuneEnd
	if regionEnd > totalRunes {
		regionEnd = totalRunes
	}

	if regionEnd > lineRuneStart {
		t.igRegions = editor.Regions(lineRuneStart, lineRuneStart+1, t.igRegions)
		if len(t.igRegions) > 0 {
			lineYStart[lineIdx] = t.igRegions[0].Bounds.Min.Y
			lineYEnd[lineIdx] = t.igRegions[0].Bounds.Max.Y

			return
		}
	}

	// Empty line (or Regions returned nothing): find the nearest non-empty
	// line's height to avoid compounding estimation errors.
	refH := 0
	refEnd := 0

	for back := lineIdx - 1; back >= 0; back-- {
		h := lineYEnd[back] - lineYStart[back]
		if h > 0 {
			refH = h
			refEnd = lineYEnd[back]

			break
		}
	}

	// Fallback for the first line (or all-empty prefix): query the editor
	// for any character's region to get a line height estimate.
	if refH == 0 && totalRunes > 0 {
		t.igRegions = editor.Regions(0, 1, t.igRegions)
		if len(t.igRegions) > 0 {
			refH = t.igRegions[0].Bounds.Max.Y - t.igRegions[0].Bounds.Min.Y
		}
	}

	lineYStart[lineIdx] = refEnd
	lineYEnd[lineIdx] = refEnd + refH
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

// layoutScrollbarMarkers draws colored markers alongside the scrollbar for overridden entries.
func (t *OverrideTable) layoutScrollbarMarkers(
	gtx layout.Context,
	entries []service.FlatValueEntry,
	filteredIndices []int,
) {
	totalH := gtx.Constraints.Max.Y
	totalEntries := len(filteredIndices)

	if totalEntries == 0 || totalH <= 0 {
		return
	}

	totalW := gtx.Constraints.Max.X
	markerW := gtx.Dp(overrideMarkerW)
	markerH := max(gtx.Dp(overrideMarkerMinH), 1)
	scrollX := totalW - gtx.Dp(overrideScrollbarWidth)

	for visIdx, entryIdx := range filteredIndices {
		if entryIdx >= len(entries) {
			continue
		}

		if !t.hasAnyOverride(entryIdx) {
			continue
		}

		y := int(float64(visIdx) / float64(totalEntries) * float64(totalH))

		rect := clip.Rect{
			Min: image.Pt(scrollX, y),
			Max: image.Pt(scrollX+markerW, y+markerH),
		}.Push(gtx.Ops)
		paint.ColorOp{Color: theme.ColorScrollMarker}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		rect.Pop()
	}
}
