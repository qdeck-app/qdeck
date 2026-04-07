package state

import (
	"path/filepath"
	"strings"

	"gioui.org/widget"

	"github.com/qdeck-app/qdeck/domain"
	"github.com/qdeck-app/qdeck/service"
)

// YAMLIndent returns the detected indentation for this column's loaded file,
// falling back to DefaultYAMLIndent for new files or when no file is loaded.
func (c *CustomColumnState) YAMLIndent() int {
	if c.CustomValues != nil && c.CustomValues.Indent > 0 {
		return c.CustomValues.Indent
	}

	return service.DefaultYAMLIndent
}

const helmInstallPrefix = "helm install"

// CustomColumnState holds per-column widget state for an editable override column.
type CustomColumnState struct {
	// Values data
	CustomValues    *service.FlatValues
	CustomFilePaths []string
	MergedFileCount int

	// Override editors (one per default entry)
	OverrideEditors []widget.Editor

	// Cached per-entry override presence to avoid O(n) .Text() scans every frame.
	// Updated by MarkOverride; rebuilt by RebuildOverrideFlags.
	overrideFlags []bool
	overrideCount int // number of true entries in overrideFlags

	// Modification tracking
	ValuesModified bool
	// SuppressModifiedCount tracks how many editor change events to ignore
	// after a file load. Loading a file populates editors which triggers change
	// callbacks; this counter prevents those from marking the column as modified.
	SuppressModifiedCount int

	// Widget state
	PickFileButton     widget.Clickable
	SaveValuesButton   widget.Clickable
	CloseButton        widget.Clickable
	RemoveColumnButton widget.Clickable
	FileNameButton     widget.Clickable
	FileDropActive     bool

	// Editor parse error (shown when override YAML is invalid)
	EditorParseError string
}

// EnsureEditors grows the override editor slice only when data exceeds capacity.
// Uses bulk allocation to avoid repeated slice reallocations.
func (c *CustomColumnState) EnsureEditors(count int) {
	if len(c.OverrideEditors) >= count {
		return
	}

	editors := make([]widget.Editor, count)
	copy(editors, c.OverrideEditors)
	c.OverrideEditors = editors

	flags := make([]bool, count)
	copy(flags, c.overrideFlags)
	c.overrideFlags = flags
}

// HasOverrides returns true if any override editor in this column has non-empty text.
// Uses the cached override count for O(1) lookup.
func (c *CustomColumnState) HasOverrides() bool {
	return c.overrideCount > 0
}

// HasOverrideAt returns true if the entry at idx has a non-empty override.
func (c *CustomColumnState) HasOverrideAt(idx int) bool {
	return idx >= 0 && idx < len(c.overrideFlags) && c.overrideFlags[idx]
}

// MarkOverride sets the override flag for the given entry index.
func (c *CustomColumnState) MarkOverride(idx int, has bool) {
	if idx < 0 || idx >= len(c.overrideFlags) {
		return
	}

	was := c.overrideFlags[idx]
	if was == has {
		return
	}

	c.overrideFlags[idx] = has

	if has {
		c.overrideCount++
	} else {
		c.overrideCount--
	}
}

// RebuildOverrideFlags rescans all editors and rebuilds the override flags.
// Call after bulk SetText operations (e.g. file load).
func (c *CustomColumnState) RebuildOverrideFlags() {
	c.overrideCount = 0

	for i := range c.overrideFlags {
		has := i < len(c.OverrideEditors) && c.OverrideEditors[i].Text() != ""
		c.overrideFlags[i] = has

		if has {
			c.overrideCount++
		}
	}
}

// Reset clears all column state for reuse.
func (c *CustomColumnState) Reset() {
	c.CustomValues = nil
	c.CustomFilePaths = c.CustomFilePaths[:0]
	c.MergedFileCount = 0
	c.ValuesModified = false
	c.SuppressModifiedCount = 0
	c.FileDropActive = false
	c.EditorParseError = ""

	for i := range c.OverrideEditors {
		c.OverrideEditors[i].SetText("")
	}

	clear(c.overrideFlags)
	c.overrideCount = 0
}

// ValuesPageState holds all widget state for the values viewer page.
type ValuesPageState struct {
	// Data
	DefaultValues       *service.FlatValues
	DefaultValueEditors []widget.Editor
	ChartPath           string
	ChartName           string
	RepoName            string
	Loading             bool

	// Multi-column override state
	Columns     [MaxCustomColumns]CustomColumnState
	ColumnCount int // 1-3 active columns

	// Unified table: single list with search and filtering
	OverrideList    widget.List
	SearchEditor    widget.Editor
	FilteredIndices []int
	FocusSearch     bool

	// Cell navigation state
	FocusedRow int
	FocusedCol int

	// +Values button
	AddColumnButton widget.Clickable

	// File drop zone state (native OS drop targets first column)
	DropSupported bool

	// Save chart button (breadcrumb)
	SaveChartButton widget.Clickable

	// Recent values files
	RecentValuesFiles        []domain.RecentValuesFile
	RecentValuesClicks       []widget.Clickable
	RecentValuesRemoveClicks []widget.Clickable

	// Recent values dropdown
	RecentDropdownOpen    bool
	RecentDropdownToggle  widget.Clickable
	RecentDropdownDismiss widget.Clickable

	// Template rendering buttons
	RenderDefaultsButton  widget.Clickable
	RenderOverridesButton widget.Clickable
	RenderLoading         bool

	// Helm install command (cached, rebuilt on chart/file changes)
	HelmInstallCmd    string
	CopyInstallButton widget.Clickable
}

// HasUnsavedChanges returns true if any active column has unsaved modifications.
func (s *ValuesPageState) HasUnsavedChanges() bool {
	for c := range s.ColumnCount {
		if s.Columns[c].ValuesModified {
			return true
		}
	}

	return false
}

// EnsureColumnEditors ensures the override editor slice for the given column
// can hold count entries.
func (s *ValuesPageState) EnsureColumnEditors(colIdx, count int) {
	if colIdx < 0 || colIdx >= MaxCustomColumns {
		return
	}

	s.Columns[colIdx].EnsureEditors(count)
}

// EnsureDefaultEditors grows the default value editor slice only when data exceeds capacity.
func (s *ValuesPageState) EnsureDefaultEditors(count int) {
	if len(s.DefaultValueEditors) >= count {
		return
	}

	editors := make([]widget.Editor, count)
	copy(editors, s.DefaultValueEditors)
	s.DefaultValueEditors = editors
}

// CanAddColumn returns true if another custom column can be added.
func (s *ValuesPageState) CanAddColumn() bool {
	return s.ColumnCount < MaxCustomColumns
}

// HasOverrideInAnyColumn returns true if any column has a non-empty override
// editor at the given entry index. Uses cached flags for O(1) per-column lookup.
func (s *ValuesPageState) HasOverrideInAnyColumn(entryIdx int) bool {
	for c := range s.ColumnCount {
		if s.Columns[c].HasOverrideAt(entryIdx) {
			return true
		}
	}

	return false
}

// EnsureRecentValuesClickables grows recent values clickable slices.
func (s *ValuesPageState) EnsureRecentValuesClickables(count int) {
	for len(s.RecentValuesClicks) < count {
		s.RecentValuesClicks = append(s.RecentValuesClicks, widget.Clickable{})
		s.RecentValuesRemoveClicks = append(s.RecentValuesRemoveClicks, widget.Clickable{})
	}
}

// RebuildHelmInstallCmd reconstructs the cached helm install command from
// the current chart reference and loaded values files.
func (s *ValuesPageState) RebuildHelmInstallCmd() {
	var chartRef, releaseName string

	if s.RepoName != "" {
		chartRef = s.RepoName + "/" + s.ChartName
		releaseName = s.ChartName
	} else {
		chartRef = s.ChartPath
		releaseName = filepath.Base(s.ChartPath)
	}

	if chartRef == "" {
		s.HelmInstallCmd = ""

		return
	}

	var cmd strings.Builder
	cmd.WriteString(helmInstallPrefix + " " + releaseName + " " + chartRef)

	for c := range s.ColumnCount {
		for _, fp := range s.Columns[c].CustomFilePaths {
			cmd.WriteString(" -f " + fp)
		}
	}

	s.HelmInstallCmd = cmd.String()
}
