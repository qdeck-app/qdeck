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

//nolint:dupl // same cache pattern as BuildVersionSearchCache but different data types
func (s *ChartPageState) BuildChartSearchCache() {
	n := len(s.Charts)
	s.ChartSearchNames = buildLowerCache(s.ChartSearchNames, n, func(i int) string { return s.Charts[i].Name })
	s.ChartSearchDescs = buildLowerCache(s.ChartSearchDescs, n, func(i int) string { return s.Charts[i].Description })
}

//nolint:dupl // same cache pattern as BuildChartSearchCache but different data types
func (s *ChartPageState) BuildVersionSearchCache() {
	n := len(s.Versions)
	s.VersionSearchVersions = buildLowerCache(s.VersionSearchVersions, n, func(i int) string { return s.Versions[i].Version })
	s.VersionSearchAppVersions = buildLowerCache(s.VersionSearchAppVersions, n, func(i int) string { return s.Versions[i].AppVersion })
}

// buildLowerCache populates a reusable string slice with lowercased values extracted by fn.
func buildLowerCache(buf []string, n int, fn func(int) string) []string {
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

// EnsureChartClickables grows clickable slices.
func (s *ChartPageState) EnsureChartClickables(count int) {
	for len(s.ChartClicks) < count {
		s.ChartClicks = append(s.ChartClicks, widget.Clickable{})
	}
}

// EnsureVersionClickables grows clickable slices.
func (s *ChartPageState) EnsureVersionClickables(count int) {
	for len(s.VersionClicks) < count {
		s.VersionClicks = append(s.VersionClicks, widget.Clickable{})
	}

	for len(s.SaveClicks) < count {
		s.SaveClicks = append(s.SaveClicks, widget.Clickable{})
	}
}
