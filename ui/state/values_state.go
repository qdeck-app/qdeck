package state

import (
	"path/filepath"
	"strings"
	"time"

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

	// File modification tracking
	FileModTime time.Time // mod time recorded at load/save for external-change detection

	// Modification tracking
	ValuesModified bool
	// DrainPendingChanges is set after a file load to signal that Layout should
	// eagerly consume all pending SetText ChangeEvents before user interaction.
	DrainPendingChanges bool

	// Widget state
	PickFileButton     widget.Clickable
	SaveValuesButton   widget.Clickable
	CloseButton        widget.Clickable
	RemoveColumnButton widget.Clickable
	FileNameButton     widget.Clickable
	OpenInEditorButton widget.Clickable
	FileDropActive     bool

	// Editor parse error (shown when override YAML is invalid)
	EditorParseError string

	// GitChanges maps flat keys to their git change status (added/modified vs HEAD).
	// nil when git comparison is not available or file is not tracked.
	GitChanges map[string]domain.GitChangeStatus
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
	c.FileModTime = time.Time{}
	c.ValuesModified = false
	c.FileDropActive = false
	c.EditorParseError = ""
	c.GitChanges = nil

	for i := range c.OverrideEditors {
		c.OverrideEditors[i].SetText("")
	}

	c.DrainPendingChanges = len(c.OverrideEditors) > 0

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
	OciRef              string
	Version             string
	Loading             bool

	// Multi-column override state
	Columns     [MaxCustomColumns]CustomColumnState
	ColumnCount int // 1-3 active columns

	// Unified table: single list with search and filtering
	OverrideList    widget.List
	SearchEditor    widget.Editor
	FilteredIndices []int
	FocusSearch     bool

	// Cell navigation state.
	// FocusedRow indexes FilteredIndices (not Entries); FocusedCol indexes
	// active override columns. PendingFocusKey holds a restored entry key that
	// has not yet been resolved to a row — Page.Layout consumes it after
	// FilteredIndices is computed and clears it. PendingFocusHighlight asks
	// Page.Layout to issue a key.FocusCmd on the highlighted editor on the
	// next frame (set by the controller on chart load); Layout clears it
	// after dispatch.
	FocusedRow            int
	FocusedCol            int
	PendingFocusKey       string
	PendingFocusHighlight bool

	// CollapsedKeys is the effective set of section flat keys whose descendants
	// are hidden on the values page. Map for O(1) ancestor-prefix checks during
	// filter rebuild. During a search the page may auto-uncollapse ancestors of
	// matches here, so this is NOT always the user's intent — see
	// CollapsedPreSearch.
	CollapsedKeys map[string]bool

	// CollapsedPreSearch holds the user's authoritative collapsed set captured
	// when search becomes active, so the view reverts to the user's intent when
	// the search is cleared. nil outside search mode. While SearchCollapseActive
	// is true, CollapsedPreSearch — not CollapsedKeys — is what gets persisted,
	// so temporary search-induced expansions don't overwrite the saved state.
	// onCollapseToggle mirrors explicit user toggles into this snapshot so
	// mid-search user actions still survive the search clear.
	CollapsedPreSearch   map[string]bool
	SearchCollapseActive bool

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
	ShowComments          widget.Bool

	// Helm install command (cached, rebuilt on chart/file changes)
	HelmInstallCmd    string
	CopyInstallButton widget.Clickable
}

// ChartKey returns a stable identifier for the currently loaded chart, used as
// the persistence key for per-chart UI state. Returns "" if no chart is loaded.
// Delegates to domain.ChartKey so the shape stays in sync with other callers
// (e.g. RecentChart.ChartKey) that need to address the same entry.
func (s *ValuesPageState) ChartKey() string {
	return domain.ChartKey(s.RepoName, s.ChartName, s.OciRef, s.ChartPath, s.Version)
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

	switch {
	case s.RepoName != "":
		chartRef = s.RepoName + "/" + s.ChartName
		releaseName = s.ChartName
	case s.OciRef != "":
		chartRef = s.OciRef + " --version " + s.Version
		releaseName = s.ChartName
	default:
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
