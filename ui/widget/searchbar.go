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

// SearchBar wraps an Editor and provides filtered index computation.
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
	s.lowerComments = buildSearchLowerCache(s.lowerComments, n, func(i int) string { return entries[i].Comment })
}

// FilterEntriesWithMultiOverrides returns indices matching key, value, comment,
// or override editor text across multiple columns.
// Reuses the provided out slice to avoid per-frame allocations.
func (s *SearchBar) FilterEntriesWithMultiOverrides(
	entries []service.FlatValueEntry,
	columnEditors [][]widget.Editor,
	out []int,
) []int {
	query := strings.ToLower(s.Editor.Text())
	out = out[:0]

	if query == "" {
		for i := range entries {
			out = append(out, i)
		}

		return out
	}

	s.rebuildCacheIfNeeded(entries)

	for i := range entries {
		if strings.Contains(s.lowerKeys[i], query) ||
			strings.Contains(s.lowerValues[i], query) ||
			strings.Contains(s.lowerComments[i], query) {
			out = append(out, i)

			continue
		}

		for _, eds := range columnEditors {
			if i < len(eds) && strings.Contains(strings.ToLower(eds[i].Text()), query) {
				out = append(out, i)

				break
			}
		}
	}

	return out
}

const (
	searchPaddingV    unit.Dp = 4
	searchPaddingH    unit.Dp = 8
	searchBorderWidth unit.Dp = 1
)

// Layout renders the search text field with a border spanning the full width.
func (s *SearchBar) Layout(gtx layout.Context, th *material.Theme, hint string) layout.Dimensions {
	borderW := gtx.Dp(searchBorderWidth)
	width := gtx.Constraints.Max.X

	return layout.Stack{}.Layout(gtx,
		// Top separator line.
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			sz := image.Pt(width, gtx.Constraints.Min.Y)

			// Top border.
			top := clip.Rect{Max: image.Pt(sz.X, borderW)}.Push(gtx.Ops)
			paint.ColorOp{Color: theme.ColorSeparator}.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			top.Pop()

			return layout.Dimensions{Size: sz}
		}),
		// Editor content.
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = width

			return layout.Inset{
				Top: searchPaddingV, Bottom: searchPaddingV,
				Left: searchPaddingH, Right: searchPaddingH,
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				editor := material.Editor(th, s.Editor, hint)

				return LayoutEditor(gtx, th.Shaper, editor)
			})
		}),
	)
}
