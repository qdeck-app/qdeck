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
	"gopkg.in/yaml.v3"

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
	overrideChevronSlotW   unit.Dp = 14 // fixed slot reserved before every key label
	overrideChevronSize    unit.Dp = 6  // edge length of the filled chevron triangle
	overrideBadgeGap       unit.Dp = 4  // left margin between the key label and anchor/alias badge
	overrideBadgePaddingH  unit.Dp = 4  // horizontal padding inside an anchor badge chip
	overrideBadgePaddingV  unit.Dp = 1  // vertical padding inside an anchor badge chip
	overrideBadgeRadius    unit.Dp = 2  // corner radius of an anchor badge chip
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

	// DefaultAnchors maps flat keys to the anchor/alias annotation from the
	// chart's default values.yaml. Set by the page each frame; nil means the
	// default file declared no anchors (the common case).
	DefaultAnchors map[string]service.AnchorInfo

	// ColumnCount is the number of active override columns (1-3).
	ColumnCount int

	// ColumnRatio controls the left proportion (0..1). Defaults to overrideDefaultRatio.
	ColumnRatio float32

	// ShowComments controls whether comment lines above default value entries are displayed.
	ShowComments bool

	hovers             []gesture.Hover
	cellClicks         []gesture.Click
	collapseClicks     []gesture.Click
	defaultBadgeClicks []gesture.Click
	columnBadgeClicks  [state.MaxCustomColumns][]gesture.Click
	rightClickTargets  [state.MaxCustomColumns][]rightClickTarget

	// lastRightClickPos is the pointer's position in table-local coordinates
	// at the most recent secondary-button press, captured by a table-root
	// event filter so right-click positions can be forwarded to the page in
	// a coord system the page can translate. Updated by drainRightClicks;
	// read when firing OnCellContextMenu.
	lastRightClickPos image.Point

	// tableRootTarget is the event.Tag used for a table-root pointer filter
	// that captures every press inside the table, regardless of which cell
	// it lands in. Distinct from per-cell rightClickTargets so both fire on
	// the same event without pass-through interference.
	tableRootTarget struct{ _ byte }

	HoveredRow int

	// CollapsedKeys is a read-only reference to the page's collapsed set,
	// keyed by flat key. Used only to pick the chevron glyph; mutation happens
	// via OnCollapseToggle. Nil is treated as "nothing collapsed".
	CollapsedKeys map[string]bool

	// OnCollapseToggle fires when the user clicks a section row's chevron.
	// The callback owns mutating the collapsed set and persisting it.
	OnCollapseToggle func(key string)

	// FocusedRow (visible filtered index) and FocusedCol (0-based override column)
	// identify the cell to paint with a focus highlight. Set by the page each frame.
	FocusedRow int
	FocusedCol int

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

	// OnJumpToFlatKey fires when the user clicks an alias badge. The argument
	// is the flat key of the anchor definition within the same file the alias
	// came from. Typical handler scrolls to and focuses that row.
	OnJumpToFlatKey func(gtx layout.Context, key string)

	// OnAnchorBadgeClicked fires when the user clicks a green `&name` anchor
	// badge. Typical handler opens a menu listing every alias that references
	// this anchor, turning the anchor badge into the reverse of an alias jump.
	OnAnchorBadgeClicked func(gtx layout.Context, col int, flatKey, anchorName string)

	// OnCellContextMenu fires on right-click of an override editor cell,
	// carrying the column index, the cell's flat key, and the pointer's
	// position in cell-local coordinates (unused by the current page handler
	// — the page reads the page-local cursor position tracked at its root).
	OnCellContextMenu func(col int, flatKey string, localPos image.Point)

	// OnAnchoredCellEdit fires when the user's keystrokes actually change the
	// text of a cell that participates in a YAML anchor or alias. The widget
	// reverts the change automatically (so the anchor/alias stays intact);
	// the handler typically opens a confirm dialog that, if accepted, clears
	// the anchor so subsequent typing goes through. Not firing this callback
	// — or doing nothing in it — silently ignores edits on anchored cells.
	OnAnchoredCellEdit func(col int, flatKey string)
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

func (t *OverrideTable) ensureCollapseClicks(count int) {
	if count > len(t.collapseClicks) {
		t.collapseClicks = append(t.collapseClicks, make([]gesture.Click, count-len(t.collapseClicks))...)
	}
}

func (t *OverrideTable) ensureBadgeClicks(count int) {
	if count > len(t.defaultBadgeClicks) {
		t.defaultBadgeClicks = append(t.defaultBadgeClicks, make([]gesture.Click, count-len(t.defaultBadgeClicks))...)
	}

	for c := range state.MaxCustomColumns {
		if count > len(t.columnBadgeClicks[c]) {
			t.columnBadgeClicks[c] = append(t.columnBadgeClicks[c], make([]gesture.Click, count-len(t.columnBadgeClicks[c]))...)
		}
	}
}

// rightClickTarget is a single-byte struct whose address uniquely identifies
// one (col, row) editor cell for Gio's pointer event dispatch. Empty structs
// would share an address across the slice and break dispatch; a 1-byte field
// forces unique element addresses.
type rightClickTarget struct {
	_ byte
}

func (t *OverrideTable) ensureRightClickTargets(count int) {
	for c := range state.MaxCustomColumns {
		if count > len(t.rightClickTargets[c]) {
			t.rightClickTargets[c] = append(t.rightClickTargets[c], make([]rightClickTarget, count-len(t.rightClickTargets[c]))...)
		}
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

// columnBounds returns the x offset and width of sub-column c within the
// right panel. The last column absorbs any remainder pixels from the integer
// division in columnGeometry so the right edge lines up with rightW. rightW
// is the total width of the right panel (identical to g.colW*g.count + g.totalDivW).
func (g colGeometry) columnBounds(c, rightW int) (x, w int) {
	x = g.rightStart + c*(g.colW+g.subDivW)
	w = g.colW

	if c == g.count-1 {
		w = rightW - g.totalDivW - g.colW*(g.count-1)
	}

	return x, w
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
	t.ensureCollapseClicks(len(filteredIndices))
	t.ensureBadgeClicks(len(filteredIndices))
	t.ensureRightClickTargets(len(filteredIndices))

	t.HoveredRow = overrideNoHover

	t.handleDrag(gtx)
	t.captureTableRootPointer(gtx)

	// Wrap the table body in pointer.PassOp so every event.Op registered
	// inside (editors, clickables, the badge clicks, per-cell right-click
	// tags) is pass=true. Gio's hitTest stops walking back the hit tree at
	// the first pass=false handler, and a pass=false editor would otherwise
	// block the table-root target from ever receiving press events.
	tablePass := pointer.PassOp{}.Push(gtx.Ops)
	defer tablePass.Pop()

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
						return layout.Inset{Left: indent + overrideChevronSlotW}.Layout(gtx,
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

											return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return t.layoutChevronSlot(gtx, index, entry.Key, section)
												}),
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return LayoutLabel(gtx, lbl)
												}),
											)
										})
									}),
									layout.Flexed(overrideValueProportion, func(gtx layout.Context) layout.Dimensions {
										if entryIdx >= len(t.DefaultValueEditors) {
											return layout.Dimensions{}
										}

										badgeInfo := t.DefaultAnchors[entry.Key]

										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
												return layoutDefaultValue(gtx, t.Theme, &t.DefaultValueEditors[entryIdx])
											}),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return t.layoutAnchorBadge(
													gtx, badgeInfo, t.DefaultAnchors,
													&t.defaultBadgeClicks[index], -1, entry.Key,
												)
											}),
										)
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

						return t.layoutRightColumns(gtx, index, entryIdx, entry.Key, g, entry.Type)
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

	t.handleCellClick(gtx, index, entryIdx, entry.Key, g, rightW, dims.Size.Y, section)

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
	case section && index == t.FocusedRow:
		// Sections have no editable cells, so the per-column focus rect below
		// doesn't fire. Paint a row-wide focus tint — same color as editable
		// cell focus — so keyboard focus on a section chevron is visible and
		// distinguishable from a plain hover row.
		paintRowBg(gtx, dims.Size.Y, theme.ColorFocus)
	case hovered:
		paintRowBg(gtx, dims.Size.Y, theme.ColorHover)
	}

	// Focus cell highlight: paint between the row background and the editor
	// content so the editor text remains crisp on top of the tinted fill.
	if !section && index == t.FocusedRow && t.FocusedCol >= 0 && t.FocusedCol < g.count {
		colX, colW := g.columnBounds(t.FocusedCol, rightW)

		rect := clip.Rect{
			Min: image.Pt(colX, 0),
			Max: image.Pt(colX+colW, dims.Size.Y),
		}.Push(gtx.Ops)
		paint.ColorOp{Color: theme.ColorFocus}.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		rect.Pop()
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
	rowIndex int,
	entryIdx int,
	entryKey string,
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
			_, w := g.columnBounds(col, rightW)

			gtx.Constraints.Min.X = w
			gtx.Constraints.Max.X = w

			editors := t.ColumnEditors[col]
			if entryIdx >= len(editors) {
				return layout.Dimensions{Size: image.Pt(w, 0)}
			}

			badgeInfo := t.columnAnchorInfo(col, entryKey)
			colAnchors := t.columnAnchors(col)

			return layout.Inset{Left: overridePaddingH}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					t.drainRightClicks(gtx, col, rowIndex, entryKey)

					dims := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return t.layoutEditorCell(gtx, col, entryIdx, hint)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return t.layoutAnchorBadge(
								gtx, badgeInfo, colAnchors,
								&t.columnBadgeClicks[col][rowIndex], col, entryKey,
							)
						}),
					)

					pass := pointer.PassOp{}.Push(gtx.Ops)
					area := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
					event.Op(gtx.Ops, &t.rightClickTargets[col][rowIndex])
					area.Pop()
					pass.Pop()

					return dims
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

			var tree *yaml.Node

			if cs := t.ColumnStates[c]; cs != nil {
				indent = cs.YAMLIndent()
				if cs.CustomValues != nil {
					tree = cs.CustomValues.NodeTree
				}
			}

			yamlText, yamlErr := state.OverridesToYAML(entries, editors, indent, tree)
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

// propagateAnchoredValueEdit mirrors a cell edit into every alias that
// resolves through the same anchor. YAML semantics say aliases take their
// value from the anchor, so editing the anchor's scalar (or a leaf under an
// anchored mapping) must visually update the parallel alias cells — without
// this the UI drifts from what the saved file will contain.
//
// The nearest anchored ancestor (or the edited cell itself) determines the
// alias name; the suffix after that ancestor's flat key is appended to each
// alias's flat key to locate the corresponding editor.
func (t *OverrideTable) propagateAnchoredValueEdit(
	gtx layout.Context,
	col int,
	changedKey string,
	entries []service.FlatValueEntry,
	editors []widget.Editor,
) {
	cs := t.ColumnStates[col]
	if cs == nil || cs.CustomValues == nil || len(cs.CustomValues.Anchors) == 0 {
		return
	}

	anchors := cs.CustomValues.Anchors

	anchorKey, anchorName := findAnchoredAncestor(anchors, changedKey)
	if anchorName == "" {
		return
	}

	rawSuffix := strings.TrimPrefix(strings.TrimPrefix(changedKey, anchorKey), ".")

	// Cache the dot-prefixed suffix once — each alias target is aliasKey+suffix
	// with no branch or extra concat inside the loop.
	var suffix string
	if rawSuffix != "" {
		suffix = "." + rawSuffix
	}

	changedIdx := indexOfEntry(entries, changedKey)
	if changedIdx < 0 || changedIdx >= len(editors) {
		return
	}

	newText := editors[changedIdx].Text()

	for aliasKey, info := range anchors {
		if info.Role != service.AnchorRoleAlias || info.Name != anchorName {
			continue
		}

		targetKey := aliasKey + suffix

		idx := indexOfEntry(entries, targetKey)
		if idx < 0 || idx >= len(editors) {
			continue
		}

		if editors[idx].Text() == newText {
			continue
		}

		editors[idx].SetText(newText)
		t.drainEditorEvents(gtx, &editors[idx])
		cs.MarkOverride(idx, state.StripYAMLComments(newText) != "")
	}
}

// findAnchoredAncestor returns the flat key of the nearest ancestor (or the
// key itself) annotated with role=Anchor in anchors, along with the anchor
// name. Returns ("", "") when no anchored ancestor is found.
func findAnchoredAncestor(anchors map[string]service.AnchorInfo, key string) (string, string) {
	bestKey := ""
	bestName := ""

	for k, info := range anchors {
		if info.Role != service.AnchorRoleAnchor {
			continue
		}

		if k == key || strings.HasPrefix(key, k+".") {
			if len(k) > len(bestKey) {
				bestKey = k
				bestName = info.Name
			}
		}
	}

	return bestKey, bestName
}

func indexOfEntry(entries []service.FlatValueEntry, key string) int {
	for i, e := range entries {
		if e.Key == key {
			return i
		}
	}

	return -1
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

// layoutChevronSlot renders the collapse/expand chevron for section rows and
// reserves an equivalent-width empty slot for leaf rows so labels stay aligned.
// On section rows, clicks inside the slot invoke OnCollapseToggle.
func (t *OverrideTable) layoutChevronSlot(
	gtx layout.Context,
	index int,
	key string,
	section bool,
) layout.Dimensions {
	slotW := gtx.Dp(overrideChevronSlotW)
	size := image.Pt(slotW, slotW)

	if !section {
		return layout.Dimensions{Size: size}
	}

	// Drain click events for this row's chevron. Only fire on Click (mouse up
	// inside the region) so a drag-scroll initiated on the chevron doesn't
	// accidentally toggle it. The gesture fires mid-frame after FilteredIndices
	// has already been computed, so request a redraw — the next frame picks up
	// the mutated collapsed set.
	for {
		ev, ok := t.collapseClicks[index].Update(gtx.Source)
		if !ok {
			break
		}

		if ev.Kind == gesture.KindClick && t.OnCollapseToggle != nil {
			t.OnCollapseToggle(key)
			gtx.Execute(op.InvalidateCmd{})
		}
	}

	drawChevronTriangle(gtx, size, t.CollapsedKeys[key])

	// Register a click+cursor region over the entire slot.
	area := clip.Rect{Max: size}.Push(gtx.Ops)
	t.collapseClicks[index].Add(gtx.Ops)
	pointer.CursorPointer.Add(gtx.Ops)
	area.Pop()

	return layout.Dimensions{Size: size}
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

// columnAnchorInfo returns anchor/alias metadata for a flat key in a specific
// override column, or the zero value when that column did not load an anchored
// file or the key has no anchor annotation.
func (t *OverrideTable) columnAnchorInfo(col int, key string) service.AnchorInfo {
	cs := t.ColumnStates[col]
	if cs == nil || cs.CustomValues == nil {
		return service.AnchorInfo{}
	}

	return cs.CustomValues.Anchors[key]
}

// aliasTextMatchesEffective reports whether text equals the scalar the alias
// at key in col currently resolves to. Used by the alias-edit guard to
// distinguish a programmatic sync (text already matches) from a real user
// divergence that should prompt for unlock.
func (t *OverrideTable) aliasTextMatchesEffective(col int, key, text string) bool {
	cs := t.ColumnStates[col]
	if cs == nil || cs.CustomValues == nil || cs.CustomValues.NodeTree == nil {
		return false
	}

	resolved, ok := service.EffectiveScalarAt(cs.CustomValues.NodeTree, key)

	return ok && resolved == text
}

// columnAnchors returns the full anchor map for a column, or nil when the
// column has no loaded file. Used to resolve an alias badge's jump target.
func (t *OverrideTable) columnAnchors(col int) map[string]service.AnchorInfo {
	cs := t.ColumnStates[col]
	if cs == nil || cs.CustomValues == nil {
		return nil
	}

	return cs.CustomValues.Anchors
}

// layoutEditorCell renders one override editor cell, drawing indent guides
// underneath for multi-line values. Factored out of layoutRightColumns so the
// cell body can be stacked with an anchor badge overlay without nesting.
// The editor is always editable — anchored cells keep a visible caret and
// allow selection; actual text mutations are caught in processEditorChanges
// and reverted with a warning dialog.
func (t *OverrideTable) layoutEditorCell(gtx layout.Context, col, entryIdx int, hint string) layout.Dimensions {
	editors := t.ColumnEditors[col]
	ed := material.Editor(t.Theme, &editors[entryIdx], hint)
	ed.TextSize = viewerEditorTextSize

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

// layoutAnchorBadge renders a small pill marking a YAML anchor definition
// (`&name`, green) or alias usage (`*name`, blue). Returns zero-size
// dimensions when info is empty so the surrounding Flex.Rigid collapses with
// no visible gap.
//
// Both roles are clickable when their respective handler is wired:
//   - Alias badges fire OnJumpToFlatKey with the anchor's source flat key so
//     the user jumps to the anchor definition.
//   - Anchor badges fire OnAnchorBadgeClicked with the column, flat key, and
//     anchor name so the page can show the reverse menu (aliases → jump).
//
// The badge registers a click region and a pointer cursor only for the role
// whose handler is available; non-clickable badges remain purely decorative.
//
// col and flatKey are only used for the anchor-badge handler; aliases derive
// their jump target from the anchors map.
func (t *OverrideTable) layoutAnchorBadge(
	gtx layout.Context,
	info service.AnchorInfo,
	anchors map[string]service.AnchorInfo,
	click *gesture.Click,
	col int,
	flatKey string,
) layout.Dimensions {
	if info.Role == service.AnchorRoleNone || info.Name == "" {
		return layout.Dimensions{}
	}

	var (
		sigil string
		bg    = theme.ColorAccent
	)

	switch info.Role {
	case service.AnchorRoleAnchor:
		sigil = "&"
		bg = theme.ColorStatsAdded
	case service.AnchorRoleAlias:
		sigil = "*"
		bg = theme.ColorAccent
	case service.AnchorRoleNone:
		return layout.Dimensions{}
	}

	clickable := click != nil && t.badgeHandler(info.Role) != nil
	if clickable {
		for {
			ev, ok := click.Update(gtx.Source)
			if !ok {
				break
			}

			if ev.Kind != gesture.KindClick {
				continue
			}

			switch info.Role {
			case service.AnchorRoleAlias:
				if target, found := findAnchorSourceKey(anchors, info.Name); found {
					t.OnJumpToFlatKey(gtx, target)
				}
			case service.AnchorRoleAnchor:
				t.OnAnchorBadgeClicked(gtx, col, flatKey, info.Name)
			case service.AnchorRoleNone:
			}
		}
	}

	lbl := material.Caption(t.Theme, sigil+info.Name)
	lbl.Color = theme.ColorWhite
	lbl.MaxLines = 1

	gap := gtx.Dp(overrideBadgeGap)
	radius := gtx.Dp(overrideBadgeRadius)
	innerInset := layout.Inset{
		Left:   overrideBadgePaddingH,
		Right:  overrideBadgePaddingH,
		Top:    overrideBadgePaddingV,
		Bottom: overrideBadgePaddingV,
	}

	innerGtx := gtx
	innerGtx.Constraints.Min = image.Point{}

	macro := op.Record(gtx.Ops)
	pillDims := innerInset.Layout(innerGtx, func(gtx layout.Context) layout.Dimensions {
		return LayoutLabel(gtx, lbl)
	})
	pillCall := macro.Stop()

	offset := op.Offset(image.Pt(gap, 0)).Push(gtx.Ops)
	shape := clip.UniformRRect(image.Rectangle{Max: pillDims.Size}, radius).Push(gtx.Ops)
	paint.ColorOp{Color: bg}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	shape.Pop()
	pillCall.Add(gtx.Ops)

	if clickable {
		area := clip.Rect{Max: pillDims.Size}.Push(gtx.Ops)
		click.Add(gtx.Ops)
		pointer.CursorPointer.Add(gtx.Ops)
		area.Pop()
	}

	offset.Pop()

	return layout.Dimensions{Size: image.Point{X: gap + pillDims.Size.X, Y: pillDims.Size.Y}}
}

// badgeHandler reports whether a handler is wired for a badge role — used to
// decide whether to register a click region and a pointer cursor. Returns a
// non-nil func when the role has a callback; nil otherwise.
func (t *OverrideTable) badgeHandler(role service.AnchorRole) any {
	switch role {
	case service.AnchorRoleAlias:
		if t.OnJumpToFlatKey != nil {
			return t.OnJumpToFlatKey
		}
	case service.AnchorRoleAnchor:
		if t.OnAnchorBadgeClicked != nil {
			return t.OnAnchorBadgeClicked
		}
	case service.AnchorRoleNone:
	}

	return nil
}

// findAnchorSourceKey scans anchors for an entry with role=Anchor and name=n
// and returns the flat key where it is defined. Returns ("", false) when no
// matching anchor is in the map — the alias either points at an anchor
// defined outside the file (not representable here) or the map is nil.
func findAnchorSourceKey(anchors map[string]service.AnchorInfo, n string) (string, bool) {
	for k, info := range anchors {
		if info.Role == service.AnchorRoleAnchor && info.Name == n {
			return k, true
		}
	}

	return "", false
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
