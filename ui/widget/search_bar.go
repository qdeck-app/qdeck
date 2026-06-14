package widget

import (
	"image"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/theme"
)

// SearchBar wraps an Editor and computes the indices of matching entries so the
// caller can highlight and jump between them (it does not filter the table).
type SearchBar struct {
	Editor *widget.Editor

	// Pre-computed lowercase caches to avoid per-frame allocations.
	lowerKeys     []string
	lowerValues   []string
	lowerComments []string
	cachedLen     int
	cachedPtr     *service.FlatValueEntry // data pointer for identity check
}

// buildSearchLowerCache populates a reusable string slice with lowercased values extracted by fn.
func buildSearchLowerCache(buf []string, n int, fn func(int) string) []string {
	if cap(buf) >= n {
		buf = buf[:n]
	} else {
		buf = make([]string, n)
	}

	for i := range n {
		buf[i] = strings.ToLower(fn(i))
	}

	return buf
}

// rebuildCacheIfNeeded refreshes the lowercase caches when the entries slice changes.
// Compares both length and data pointer to detect replacement with a same-length slice.
func (s *SearchBar) rebuildCacheIfNeeded(entries []service.FlatValueEntry) {
	n := len(entries)

	var ptr *service.FlatValueEntry
	if n > 0 {
		ptr = &entries[0]
	}

	if n == s.cachedLen && ptr == s.cachedPtr {
		return
	}

	s.cachedLen = n
	s.cachedPtr = ptr
	s.lowerKeys = buildSearchLowerCache(s.lowerKeys, n, func(i int) string { return entries[i].Key })
	s.lowerValues = buildSearchLowerCache(s.lowerValues, n, func(i int) string { return entries[i].Value })
	// Indexes both user-side (Comment) and chart-default (DefaultComment)
	// annotations so search hits documentation prose from either source.
	// Joined with a newline — search queries are single-line input, so a
	// query can't span the boundary the way a space-join would allow.
	s.lowerComments = buildSearchLowerCache(s.lowerComments, n, func(i int) string {
		if entries[i].DefaultComment == "" {
			return entries[i].Comment
		}

		if entries[i].Comment == "" {
			return entries[i].DefaultComment
		}

		return entries[i].Comment + "\n" + entries[i].DefaultComment
	})
}

// MatchingEntries returns the indices of entries whose key, value, comment, or
// override editor text (across columns) contains the current query. Unlike a
// filter, an empty query yields no matches — the table shows every row and the
// caller navigates between these indices instead of hiding non-matches.
//
// Reuses the provided out slice to avoid per-frame allocations.
func (s *SearchBar) MatchingEntries(
	entries []service.FlatValueEntry,
	columnEditors [][]widget.Editor,
	out []int,
) []int {
	query := strings.ToLower(s.Editor.Text())
	out = out[:0]

	if query == "" {
		return out
	}

	matchesQuery := func(i int) bool {
		if strings.Contains(s.lowerKeys[i], query) ||
			strings.Contains(s.lowerValues[i], query) ||
			strings.Contains(s.lowerComments[i], query) {
			return true
		}

		for _, eds := range columnEditors {
			if i < len(eds) && strings.Contains(strings.ToLower(eds[i].Text()), query) {
				return true
			}
		}

		return false
	}

	s.rebuildCacheIfNeeded(entries)

	for i := range entries {
		// Orphan-comment rows have an empty Key, so the caller can't scroll to
		// or highlight them — counting them would yield phantom matches the
		// prev/next buttons skip over. Their prose is still reachable via the
		// key/value of neighbouring rows.
		if entries[i].IsComment() {
			continue
		}

		if !matchesQuery(i) {
			continue
		}

		out = append(out, i)
	}

	return out
}

const (
	searchPaddingV    unit.Dp = 4
	searchPaddingH    unit.Dp = 8
	searchBorderWidth unit.Dp = 1
)

// Layout renders the search text field with a border spanning the full width.
// trailing, when non-nil, is laid out as a rigid to the right of the editor
// (e.g. the match counter and prev/next navigation buttons); it is vertically
// centered against the editor so the controls share the field's baseline.
func (s *SearchBar) Layout(gtx layout.Context, th *material.Theme, hint string, trailing layout.Widget) layout.Dimensions {
	borderW := gtx.Dp(searchBorderWidth)
	width := gtx.Constraints.Max.X

	return layout.Stack{}.Layout(gtx,
		// Top separator line.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(width, gtx.Constraints.Min.Y)

			// Top border.
			top := clip.Rect{Max: image.Pt(sz.X, borderW)}.Push(gtx.Ops)
			paint.ColorOp{Color: theme.Default.Border}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			top.Pop()

			return layout.Dimensions{Size: sz}
		}),
		// Editor content plus optional trailing navigation controls.
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = width

			return layout.Inset{
				Top: searchPaddingV, Bottom: searchPaddingV,
				Left: searchPaddingH, Right: searchPaddingH,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				editorField := func(gtx layout.Context) layout.Dimensions {
					editor := material.Editor(th, s.Editor, hint)

					return LayoutEditor(gtx, th.Shaper, editor)
				}

				if trailing == nil {
					return editorField(gtx)
				}

				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, editorField),
					layout.Rigid(trailing),
				)
			})
		}),
	)
}
