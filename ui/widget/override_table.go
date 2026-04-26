package widget

import (
	"image"
	"image/color"
	"strings"
	"unicode/utf8"

	"gioui.org/gesture"
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
	// overrideCommentMaxLines clamps how many lines an orphan-comment row paints.
	// Foot blocks in real chart values can be 20+ lines (commented-out YAML
	// example configurations are common), and rendering all of them would push
	// editable rows off-screen. Round-trip preservation is independent of how
	// much we display; the underlying tree carries every byte through to save.
	overrideCommentMaxLines         = 3
	overrideBadgePaddingH   unit.Dp = 4 // horizontal padding inside an anchor badge chip
	overrideBadgePaddingV   unit.Dp = 1 // vertical padding inside an anchor badge chip
	overrideBadgeRadius     unit.Dp = 2 // corner radius of an anchor badge chip
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

	// SearchQuery is the current search text. When non-empty, key-column
	// labels paint a yellow highlight behind the first case-insensitive match.
	// Set by the page before Layout each frame.
	SearchQuery string

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

	// Per-anchor color cache, keyed by anchor name. Lazy-init on first miss
	// inside anchorColor — keeps the layout hot path allocation-free after
	// the first frame each anchor name is rendered.
	anchorColorCache map[string]color.NRGBA

	// anchorMembershipScratch is the per-row collection buffer used by
	// collectAnchorMemberships. Reused via [:0] so the row paint loop adds
	// no allocations after the slice grows once.
	anchorMembershipScratch []anchorMembership

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

	// OnCommentChanged fires when the user types in a comment-row editor.
	// The page-level handler harvests the column's comment editors into a
	// DocComments structure so banner/trailer/foot edits round-trip on save.
	// col identifies the source column whose comment editor changed; v1
	// always reports commentSourceCol (0).
	OnCommentChanged func(col int)

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

	// Scrollbar markers overlay the whole table because they're rendered
	// against the scrollbar column regardless of list content position. The
	// list itself takes the full table area — the parent-path indicator that
	// used to live as a sticky header inside the table now lives in the page
	// header above (see ValuesPage.layoutStickyParent), so scrolling never
	// triggers a layout change in the list and stays perfectly smooth.
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return material.List(t.Theme, t.List).Layout(gtx, len(filteredIndices),
				func(gtx layout.Context, index int) layout.Dimensions {
					return t.layoutRow(gtx, entries, filteredIndices, index)
				})
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

	// Orphan-comment rows render as an editable caption in the right (override)
	// panel — they're the user's annotations on the values file, not chart-side
	// documentation. Round-tripping the underlying YAML foot comment goes
	// through the source column's CustomValues fields on save.
	if entry.IsComment() {
		return t.layoutCommentRow(gtx, index, entryIdx, entry)
	}

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
			// Section comment editor, rendered in the RIGHT panel above the
			// section row. Section rows have no value editor, so this is the
			// place where the user's section-divider text surfaces and can be
			// edited. Always shown (regardless of ShowComments) because it's
			// structural documentation; the editor accepts an empty value to
			// delete the comment on save.
			//
			// Leaf rows do NOT render a separate editor here: their custom-file
			// head comment is already encoded inline in the override editor's
			// text as `# comment\nvalue` (via formatCommentForEditor on load),
			// which IS the right-panel display + edit surface.
			//
			// Sources from commentSourceCol's editor pool (column 0 in v1) so
			// section comment editing in multi-column setups always targets
			// the user's primary values file.
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !section {
					return layout.Dimensions{}
				}

				editors := t.ColumnEditors[commentSourceCol]
				if entryIdx >= len(editors) {
					return layout.Dimensions{}
				}

				if editors[entryIdx].Text() == "" {
					return layout.Dimensions{}
				}

				t.processCommentEditorChange(gtx, entryIdx)

				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{Size: image.Pt(leftW+dividerW, 0)}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = rightW
						gtx.Constraints.Max.X = rightW

						// No depth indent on section-comment editors: the right
						// panel has no chevron/indent guides to align against,
						// so an indented comment just looks pushed in for no
						// visual reason. Match the leaf editor's plain
						// overridePaddingH so section comments sit flush with
						// the override editors directly above and below.
						return layout.Inset{
							Left:  overridePaddingH,
							Right: overridePaddingH,
						}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return t.layoutCommentEditor(gtx, entryIdx, 0)
						})
					}),
				)
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
													return LayoutHighlightedLabel(gtx, lbl, t.SearchQuery)
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
							// Section rows have no editor cells, but when a custom file
							// attaches an anchor or alias to the section key itself (e.g.
							// `master: &masterConfig`) the badge still needs to be visible.
							return t.layoutSectionBadges(gtx, index, entry.Key, g, rightW)
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
	case section && entry.IsCustomOnly:
		// Brand-new section that exists only in an override file: paint a
		// horizontal lavender gradient that fades to transparent across the
		// row, calling out the new subtree without obscuring downstream
		// content (badges, focus tint stacked above).
		paintCustomOnlySectionGradient(gtx, dims.Size.Y, theme.ColorCustomOnlyMarker)
	case hasOverride:
		// Override tint covers only the right side of the row — that's where
		// the user's overridden value sits. Tinting the whole row would
		// imply the key+default columns also changed, which they didn't.
		paintRowBgFrom(gtx, g.rightStart, dims.Size.Y, theme.ColorOverride)
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

	// Anchor membership stripes on the row's left side, sitting at the indent
	// column where each anchor's key text begins. A row inside multiple
	// nested anchors gets one stripe per ancestor at that ancestor's own
	// indent depth — the shallowest stripe is leftmost, deeper stripes
	// appear further right at their own indent positions. Sections are
	// included because an anchor can be defined on a mapping header
	// (e.g. `master: &masterConfig`).
	t.anchorMembershipScratch = t.collectAnchorMemberships(entry.Key, t.anchorMembershipScratch[:0])
	if len(t.anchorMembershipScratch) > 0 {
		stripeW := gtx.Dp(anchorStripeWidth)
		baseX := gtx.Dp(overridePaddingH)
		levelW := gtx.Dp(overrideIndentPerLevel)

		for _, m := range t.anchorMembershipScratch {
			x := baseX + m.depth*levelW

			paintAnchorStripe(gtx, dims.Size.Y, x, stripeW, t.anchorColor(m.name))
		}
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

// layoutSectionBadges renders just the anchor/alias badge for each active
// override column on a section row. Section rows have no editor cells, so
// the badge sits where the editor would be, aligned to the right edge of
// each column so it lines up with regular-row badges below.
func (t *OverrideTable) layoutSectionBadges(
	gtx layout.Context,
	rowIndex int,
	entryKey string,
	g colGeometry,
	rightW int,
) layout.Dimensions {
	var children [state.MaxCustomColumns * 2]layout.FlexChild

	n := 0

	for c := range g.count {
		col := c

		children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			_, w := g.columnBounds(col, rightW)

			gtx.Constraints.Min.X = w
			gtx.Constraints.Max.X = w

			badgeInfo := t.columnAnchorInfo(col, entryKey)
			colAnchors := t.columnAnchors(col)

			return layout.Inset{Left: overridePaddingH}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Start}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Dimensions{}
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return t.layoutAnchorBadge(
								gtx, badgeInfo, colAnchors,
								&t.columnBadgeClicks[col][rowIndex], col, entryKey,
							)
						}),
					)
				})
		})
		n++

		if c < g.count-1 {
			children[n] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{Size: image.Pt(g.subDivW, 0)}
			})
			n++
		}
	}

	return layout.Flex{}.Layout(gtx, children[:n]...)
}

// layoutCommentRow paints an orphan-comment row from the user's custom values
// file. Comment rows live in the same lane as the override editor (right side
// of the table) so they read as the user's own annotations on the values
// file, not as commentary mixed into the chart-defaults view. The left panel
// (key + default value) stays empty — comment rows have no underlying leaf.
//
// Foot-block rows clamp to overrideCommentMaxLines so a 20-line YAML example
// commented out for documentation can't dominate the table; banner and
// trailer rows render unclamped because the user explicitly typed them at
// file scope and wants the full text visible. ShowComments is NOT consulted —
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
	// sub-column dividers / tree guides).
	divLine := clip.Rect{
		Min: image.Pt(leftW, 0),
		Max: image.Pt(leftW+dividerW, dims.Size.Y),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	divLine.Pop()

	// Horizontal separator at the bottom of the row.
	separatorH := gtx.Dp(overrideSeparatorH)

	sep := clip.Rect{
		Min: image.Pt(0, dims.Size.Y-separatorH),
		Max: image.Pt(totalW, dims.Size.Y),
	}.Push(gtx.Ops)
	paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	sep.Pop()

	return dims
}

// commentSourceCol is the column whose editor pool backs comment-row text.
// V1 always uses 0 — comments are sourced from the first loaded custom file.
const commentSourceCol = 0

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
	ed.Color = theme.ColorMuted
	ed.TextSize = viewerEditorTextSize

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

// CurrentParent returns the parent key path of the first row currently visible
// in the list, or "" when no nested row is visible. The page header above the
// table reads this each frame to render a fixed-position context strip — the
// same information the previous in-list sticky header used to convey, but
// without ever resizing the list. Comment rows have empty Key, so picking
// them as the first visible row would always yield ""; skip forward to the
// next non-comment row to source a stable parent path.
func (t *OverrideTable) CurrentParent(entries []service.FlatValueEntry, filteredIndices []int) string {
	first := t.List.Position.First
	if first < 0 || first >= len(filteredIndices) {
		return ""
	}

	for i := first; i < len(filteredIndices); i++ {
		entryIdx := filteredIndices[i]
		if entryIdx >= len(entries) {
			return ""
		}

		if entries[entryIdx].IsComment() {
			continue
		}

		return parentPath(entries[entryIdx].Key)
	}

	return ""
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
