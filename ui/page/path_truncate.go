package page

import (
	"math"
	"path/filepath"

	"gioui.org/layout"
	"gioui.org/text"
	"gioui.org/widget/material"
	"golang.org/x/image/math/fixed"
)

const ellipsisPrefix = "\u2026/"

// truncatePathLeft truncates a file path from the left so it fits within
// maxWidthPx, prepending "…/" when segments are removed. If the path fits,
// it is returned as-is. Binary searches over separator positions to minimize
// the number of shaping passes required for long paths.
func truncatePathLeft(lbl *material.LabelStyle, gtx layout.Context, maxWidthPx int, path string) string {
	params := text.Parameters{
		Font:     lbl.Font,
		PxPerEm:  fixed.I(gtx.Sp(lbl.TextSize)),
		MaxWidth: math.MaxInt,
	}

	if measureTextWidth(lbl.Shaper, params, path) <= maxWidthPx {
		return path
	}

	budget := maxWidthPx - measureTextWidth(lbl.Shaper, params, ellipsisPrefix)

	// Collect separator positions once; path[seps[i]+1:] is a right-anchored
	// candidate suffix for any i. Ordering by separator index is also an
	// ordering by suffix length (shorter suffixes have higher i), so a binary
	// search finds the longest suffix that still fits.
	var seps []int

	for i := range len(path) {
		if path[i] == filepath.Separator {
			seps = append(seps, i)
		}
	}

	if len(seps) == 0 {
		return ellipsisPrefix + path
	}

	lo, hi := 0, len(seps)
	for lo < hi {
		mid := (lo + hi) / 2 //nolint:mnd // binary-search midpoint
		if measureTextWidth(lbl.Shaper, params, path[seps[mid]+1:]) <= budget {
			hi = mid
		} else {
			lo = mid + 1
		}
	}

	// Even the bare filename may overflow the budget; return it anyway and
	// let the label's MaxLines handle the final clip.
	if lo == len(seps) {
		lo--
	}

	return ellipsisPrefix + path[seps[lo]+1:]
}

// measureTextWidth returns the pixel width of the given text when shaped with the provided font parameters.
func measureTextWidth(shaper *text.Shaper, params text.Parameters, str string) int {
	shaper.LayoutString(params, str)

	var width fixed.Int26_6

	for g, ok := shaper.NextGlyph(); ok; g, ok = shaper.NextGlyph() {
		width += g.Advance
	}

	return width.Ceil()
}
