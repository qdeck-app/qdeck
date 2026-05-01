package widget

import (
	"image"
	"strings"
	"unicode/utf8"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"github.com/qdeck-app/qdeck/ui/theme"
)

// indentGuide describes one vertical guide line: its indent level and
// the range of text lines it spans.
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
			paint.ColorOp{Color: theme.Default.Muted2}.Add(gtx.Ops)
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
