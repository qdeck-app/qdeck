package state

import (
	"strings"

	"gioui.org/widget"

	"github.com/qdeck-app/qdeck/domain"
)

// ChartPageState holds all widget state for the charts page.
type ChartPageState struct {
	// Data
	Charts   []domain.Chart
	Versions []domain.ChartVersion
	Loading  bool

	// Selected chart for version list
	SelectedChart string

	// Widget state
	ChartList     widget.List
	ChartClicks   []widget.Clickable
	VersionList   widget.List
	VersionClicks []widget.Clickable
	PullButton    widget.Clickable

	// Save as tar.gz
	SaveClicks []widget.Clickable

	// Search
	SearchEditor widget.Editor
	FocusSearch  bool

	// Pre-lowercased fields for zero-alloc search filtering.
	ChartSearchNames         []string
	ChartSearchDescs         []string
	VersionSearchVersions    []string
	VersionSearchAppVersions []string

	// Keyboard navigation
	FocusedIndex int
}

//nolint:dupl // same shape as BuildVersionSearchCache but different source and cache fields
func (s *ChartPageState) BuildChartSearchCache() {
	refreshLowerCaches(len(s.Charts),
		lowerField(&s.ChartSearchNames, func(i int) string { return s.Charts[i].Name }),
		lowerField(&s.ChartSearchDescs, func(i int) string { return s.Charts[i].Description }),
	)
}

//nolint:dupl // same shape as BuildChartSearchCache but different source and cache fields
func (s *ChartPageState) BuildVersionSearchCache() {
	refreshLowerCaches(len(s.Versions),
		lowerField(&s.VersionSearchVersions, func(i int) string { return s.Versions[i].Version }),
		lowerField(&s.VersionSearchAppVersions, func(i int) string { return s.Versions[i].AppVersion }),
	)
}

type lowerCache struct {
	dst *[]string
	fn  func(int) string
}

func lowerField(dst *[]string, fn func(int) string) lowerCache {
	return lowerCache{dst: dst, fn: fn}
}

// refreshLowerCaches resizes each target to n entries (reusing capacity) and
// writes the lowercased extractor output into it. Used to keep multiple
// search-key caches for the same record count in sync.
func refreshLowerCaches(n int, caches ...lowerCache) {
	for _, c := range caches {
		if cap(*c.dst) >= n {
			*c.dst = (*c.dst)[:n]
		} else {
			*c.dst = make([]string, n)
		}

		for i := range n {
			(*c.dst)[i] = strings.ToLower(c.fn(i))
		}
	}
}

func (s *ChartPageState) EnsureChartClickables(count int) {
	growClickables(count, &s.ChartClicks)
}

func (s *ChartPageState) EnsureVersionClickables(count int) {
	growClickables(count, &s.VersionClicks, &s.SaveClicks)
}
