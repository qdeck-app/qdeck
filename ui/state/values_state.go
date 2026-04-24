package state

import (
	"image"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gioui.org/widget"

	"github.com/qdeck-app/qdeck/domain"
	"github.com/qdeck-app/qdeck/service"
)

// AnchorOpMode selects the anchor dialog body. Zero means no dialog is open.
// Declared here (not in ui/widget) so state types don't need to import the
// widget package, avoiding a cycle with notificationbar.
type AnchorOpMode uint8

const (
	AnchorOpNone AnchorOpMode = iota
	AnchorOpCreate
	AnchorOpAlias
	AnchorOpAliasesOf
	AnchorOpRename
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

	// Entries is the unified, UI-facing entry list: chart defaults merged with
	// any keys found only in custom files loaded into active columns. All
	// layout, filtering, collapse, save, and render code reads from here
	// rather than DefaultValues.Entries so that custom-only keys remain
	// visible in the table and are preserved on save. Sorted by flat key.
	// Rebuilt by RebuildUnifiedEntries whenever a source changes.
	Entries []service.FlatValueEntry

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

	// Anchor operation state.
	//
	// AnchorOp is non-zero while a modal dialog prompts for an anchor name
	// (Create) or anchor selection (Alias). AnchorOpCol and AnchorOpKey
	// identify the target cell. AnchorMenuOpen tracks an active right-click
	// context menu; the menu and the dialog are mutually exclusive — opening
	// one dismisses the other. The actual widget instances (dialog, menu)
	// live on ValuesPage to avoid a state -> ui/widget import cycle.
	AnchorOp       AnchorOpMode
	AnchorOpCol    int
	AnchorOpKey    string
	AnchorOpName   string // anchor name for AliasesOf mode, otherwise unused
	AnchorMenuOpen bool
	AnchorMenuPos  image.Point
	AnchorMenuCol  int
	AnchorMenuKey  string

	// Unlock confirmation: set when the user types into a locked (anchored)
	// cell. PendingUnlockCol/Key identify the cell awaiting a confirm
	// decision. UnlockDialogOpen drives the ConfirmDialog overlay.
	UnlockDialogOpen bool
	PendingUnlockCol int
	PendingUnlockKey string

	// Delete-anchor confirmation: opened from the "Aliases of &name" dialog's
	// Delete button. Stores the target column + anchor name for the handler
	// to consume on confirm. Mirrors the UnlockDialogOpen pattern.
	DeleteAnchorDialogOpen  bool
	PendingDeleteAnchorCol  int
	PendingDeleteAnchorName string
}

// ChartKey returns a stable identifier for the currently loaded chart, used as
// the persistence key for per-chart UI state. Returns "" if no chart is loaded.
// Delegates to domain.ChartKey so the shape stays in sync with other callers
// (e.g. RecentChart.ChartKey) that need to address the same entry.
func (s *ValuesPageState) ChartKey() string {
	return domain.ChartKey(s.RepoName, s.ChartName, s.OciRef, s.ChartPath, s.Version)
}

// FocusedEntryKey resolves the flat key of the currently focused row, or "" if
// no row is focused or the focused index is out of bounds.
func (s *ValuesPageState) FocusedEntryKey() string {
	if s.FocusedRow < 0 || s.FocusedRow >= len(s.FilteredIndices) {
		return ""
	}

	idx := s.FilteredIndices[s.FocusedRow]
	if idx >= len(s.Entries) {
		return ""
	}

	return s.Entries[idx].Key
}

// RebuildUnifiedEntries recomputes Entries as the union of DefaultValues.Entries
// and any keys present only in active columns' CustomValues. Entries are sorted
// by flat key. Returns the previous Entries slice (for caller-side editor
// re-alignment) along with whether the list actually changed.
//
// Custom-only entries are copied with a blank Value so the default-side cell
// renders empty; their Type, Depth, and Comment are preserved. A custom-only
// map or list entry stays a section header (IsSection == true) because Type is
// "map"/"list" and Value is "".
func (s *ValuesPageState) RebuildUnifiedEntries() (prev []service.FlatValueEntry, changed bool) {
	prev = s.Entries

	if s.DefaultValues == nil {
		if len(prev) == 0 {
			return prev, false
		}

		s.Entries = nil

		return prev, true
	}

	defaults := s.DefaultValues.Entries

	// Fast path: no active column has loaded custom values, so entries == defaults.
	// Avoids allocating defaultKeys and customOnly on every editor-parse result
	// for the common case of editing within keys that already exist in defaults.
	if !s.anyColumnHasCustomValues() {
		if entriesEqual(prev, defaults) {
			return prev, false
		}

		s.Entries = slices.Clone(defaults)

		return prev, true
	}

	defaultKeys := make(map[string]struct{}, len(defaults))
	for i := range defaults {
		defaultKeys[defaults[i].Key] = struct{}{}
	}

	// Walk active columns in order; first occurrence of a custom-only key wins
	// (Type/Comment come from that column). Stable across columns: subsequent
	// columns don't overwrite an already-seen key.
	customOnly := make(map[string]service.FlatValueEntry)

	for c := range s.ColumnCount {
		cv := s.Columns[c].CustomValues
		if cv == nil {
			continue
		}

		for _, e := range cv.Entries {
			if _, isDefault := defaultKeys[e.Key]; isDefault {
				continue
			}

			if _, seen := customOnly[e.Key]; seen {
				continue
			}

			customOnly[e.Key] = service.FlatValueEntry{
				Key:     e.Key,
				Type:    e.Type,
				Depth:   e.Depth,
				Comment: e.Comment,
			}
		}
	}

	// Second fast path: custom files only override keys that already exist in
	// defaults, so the unified list equals defaults. Skip the merge+sort.
	if len(customOnly) == 0 {
		if entriesEqual(prev, defaults) {
			return prev, false
		}

		s.Entries = slices.Clone(defaults)

		return prev, true
	}

	merged := make([]service.FlatValueEntry, 0, len(defaults)+len(customOnly))
	merged = append(merged, defaults...)

	for _, e := range customOnly {
		merged = append(merged, e)
	}

	slices.SortFunc(merged, func(a, b service.FlatValueEntry) int {
		return strings.Compare(a.Key, b.Key)
	})

	if entriesEqual(prev, merged) {
		return prev, false
	}

	s.Entries = merged

	return prev, true
}

// anyColumnHasCustomValues reports whether any active column has loaded
// CustomValues. Used to short-circuit RebuildUnifiedEntries in the common case
// where no custom file is loaded yet.
func (s *ValuesPageState) anyColumnHasCustomValues() bool {
	for c := range s.ColumnCount {
		if s.Columns[c].CustomValues != nil {
			return true
		}
	}

	return false
}

// entriesEqual reports whether two unified entry slices describe the same
// ordered list. Relies on FlatValueEntry being comparable (all string/int).
func entriesEqual(a, b []service.FlatValueEntry) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// RealignEditorTextByKey shifts editor text from its old index (addressed by
// oldEntries[i].Key) to its new index (addressed by s.Entries[j].Key, matched
// on key). Text for keys that dropped out of Entries is discarded; slots
// corresponding to keys with no prior text are cleared. Call after
// RebuildUnifiedEntries returns changed == true for every editor slice that
// was index-aligned with the old entry list.
//
// Skips SetText when the desired text is already present so a user actively
// typing in a cell doesn't get their caret snapped back to the start by a
// background parse-rebuild.
func (s *ValuesPageState) RealignEditorTextByKey(oldEntries []service.FlatValueEntry, editors []widget.Editor) {
	if len(editors) == 0 {
		return
	}

	snapshot := make(map[string]string, len(oldEntries))

	for i := range oldEntries {
		if i >= len(editors) {
			break
		}

		if text := editors[i].Text(); text != "" {
			snapshot[oldEntries[i].Key] = text
		}
	}

	for i := range editors {
		var desired string
		if i < len(s.Entries) {
			desired = snapshot[s.Entries[i].Key]
		}

		if editors[i].Text() != desired {
			editors[i].SetText(desired)
		}
	}
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

func (s *ValuesPageState) EnsureRecentValuesClickables(count int) {
	growClickables(count, &s.RecentValuesClicks, &s.RecentValuesRemoveClicks)
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
