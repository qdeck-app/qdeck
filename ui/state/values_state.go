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

// DocCommentsForSave returns the doc-level orphan comments — banner, trailer,
// and per-leaf foot blocks — that the save path writes back via the
// DocumentNode wrapper and applyDocFoots. The fields are kept in their
// "# "-prefixed verbatim form on CustomValues, so this call is just a
// pass-through.
//
// On initial load these fields hold the raw bytes parseOrphanComments
// captured. As the user edits comment rows, onCommentChanged formats their
// plain text back to "# "-prefixed form and writes it here, so subsequent
// saves emit the user's prose with stable yaml comment markers. Returns a
// zero DocComments when no custom values are loaded — the save path skips
// banner/trailer/foot writes in that case.
func (c *CustomColumnState) DocCommentsForSave() service.DocComments {
	if c.CustomValues == nil {
		return service.DocComments{}
	}

	return service.DocComments{
		Head:         c.CustomValues.DocHeadComment,
		Foot:         c.CustomValues.DocFootComment,
		Foots:        c.CustomValues.FootComments,
		SectionHeads: c.CustomValues.SectionHeads,
	}
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

// OverrideCount returns the number of non-empty override entries in this column.
// O(1) — reads the cached count maintained by MarkOverride / RebuildOverrideFlags.
func (c *CustomColumnState) OverrideCount() int {
	return c.overrideCount
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

	// LoadError is the message from the most recent failed chart-pull or
	// default-values load. Cleared on success and on ResetState. The values
	// page renders this in the empty-state branch so a failed load is visible
	// after the transient error toast fades. Why: previously a failure left
	// the page on "No chart selected", which was misleading because the user
	// had selected a chart.
	LoadError string

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
	// after dispatch. FocusHighlightAttempts caps the retry budget so a row
	// whose tag never registers (collapsed ancestor, scrolled-away list,
	// non-focusable kind) cannot pin the UI in a forced-repaint loop.
	FocusedRow             int
	FocusedCol             int
	PendingFocusKey        string
	PendingFocusHighlight  bool
	FocusHighlightAttempts int

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
	ShowDocs              widget.Bool

	// ExtrasOnly is the toggle state for the "✚ extras-only" filter pill
	// in the search bar. When true, FilterEntriesWithMultiOverrides only
	// returns entries with IsCustomOnly == true (keys defined only in
	// the overlay file with no chart-defaults counterpart).
	ExtrasOnly        bool
	ExtrasFilterClick widget.Clickable

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

// firstCustomValues returns the first column with a loaded custom file, or
// nil. Comments are sourced from this column only — the chart-defaults file
// is excluded by design, since the user is editing the values file and only
// wants to see THEIR annotations interleaved with the data.
//
// Multi-column scenarios surface only column 0's comments in v1; per-column
// comment lanes are tracked as a follow-up.
func (s *ValuesPageState) firstCustomValues() *service.FlatValues {
	for c := range s.ColumnCount {
		if cv := s.Columns[c].CustomValues; cv != nil {
			return cv
		}
	}

	return nil
}

// decorateWithCustomComments prepends a banner row, appends a trailer row,
// and splices a foot-block row after every leaf that has a foot comment in
// the user's custom values file. All three classes are EntryKindComment rows
// commentEntryType is the synthetic Type tag used for orphan-comment rows
// surfaced in the unified entry list. Distinct from the "string"/"bool"/etc
// types of real chart values so the table can route comment rows through
// layoutCommentRow rather than the leaf-cell path.
const commentEntryType = "comment"

// rendered as muted captions. Banner/trailer have empty FootAfterKey so
// layoutCommentRow renders them unclamped; foot-block rows carry the leaf
// key in FootAfterKey for save-time round-trip.
//
// Returns entries unchanged when no custom file is loaded or it has no
// orphan comments — the chart-defaults file's comments are intentionally
// not surfaced here.
func (s *ValuesPageState) decorateWithCustomComments(entries []service.FlatValueEntry) []service.FlatValueEntry {
	cv := s.firstCustomValues()
	if cv == nil {
		return entries
	}

	hasBanner := cv.DocHeadComment != ""
	hasTrailer := cv.DocFootComment != ""
	hasFoots := len(cv.FootComments) > 0

	if !hasBanner && !hasTrailer && !hasFoots {
		return entries
	}

	out := make([]service.FlatValueEntry, 0, len(entries)+len(cv.FootComments)+2) //nolint:mnd // banner + footer slots

	if hasBanner {
		out = append(out, service.FlatValueEntry{
			Kind:    service.EntryKindComment,
			Type:    commentEntryType,
			Comment: service.CleanCommentForDisplay(cv.DocHeadComment),
		})
	}

	for _, e := range entries {
		out = append(out, e)

		if !hasFoots {
			continue
		}

		if rawFoot, ok := cv.FootComments[e.Key]; ok && rawFoot != "" {
			out = append(out, service.FlatValueEntry{
				Kind:         service.EntryKindComment,
				Type:         commentEntryType,
				Depth:        e.Depth,
				Comment:      service.CleanCommentForDisplay(rawFoot),
				FootAfterKey: e.Key,
			})
		}
	}

	if hasTrailer {
		out = append(out, service.FlatValueEntry{
			Kind:    service.EntryKindComment,
			Type:    commentEntryType,
			Comment: service.CleanCommentForDisplay(cv.DocFootComment),
		})
	}

	return out
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
//
// When a key exists in both defaults and a custom column, the defaults-side
// Comment is upgraded to the custom file's Comment when the custom one is
// non-empty. Users annotate keys in their overrides file specifically to
// document their own intent; dropping that annotation just because the chart
// happens to declare the same key is surprising.
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

	// Fast path: no active column has loaded custom values. Strip every entry's
	// Comment (defaults-side documentation noise is hidden by design — only the
	// user's custom file contributes visible comments) and skip the customOnly
	// merge since there's nothing to merge.
	if !s.anyColumnHasCustomValues() {
		stripped := stripCommentsFrom(defaults)
		service.SortByFilePositions(stripped, s.DefaultValues.KeyPositions)
		decorated := s.decorateWithCustomComments(stripped)

		if entriesEqual(prev, decorated) {
			return prev, false
		}

		s.Entries = decorated

		return prev, true
	}

	defaultKeys := make(map[string]struct{}, len(defaults))
	for i := range defaults {
		// Synthetic comment rows share an empty Key — don't seed defaultKeys
		// with them or every custom-side comment row would be mis-classified
		// as overlapping a default key.
		if defaults[i].IsComment() {
			continue
		}

		defaultKeys[defaults[i].Key] = struct{}{}
	}

	// Collect three things from overlay entries:
	//   - customOnly:        keys absent from defaults (will become extras rows)
	//   - customComments:    keys in defaults that the overlay annotates
	//   - populatedSections: defaults keys that the overlay populates as a
	//                        non-empty mapping/sequence header. When defaults
	//                        declared the key as an empty container (`{}` /
	//                        `[]`), the unified row needs to be promoted from
	//                        a leaf-shaped placeholder to a real section so
	//                        the section-comment editor and section-row
	//                        layout kick in. Without this promotion, an
	//                        overlay-side annotation on the key has nowhere
	//                        to render and the children appear orphaned.
	// First occurrence across columns wins for customOnly/customComments.
	customOnly := make(map[string]service.FlatValueEntry)
	customComments := make(map[string]string)
	populatedSections := make(map[string]struct{})

	for c := range s.ColumnCount {
		cv := s.Columns[c].CustomValues
		if cv == nil {
			continue
		}

		for _, e := range cv.Entries {
			// Custom-side orphan-comment rows aren't integrated into the
			// unified table — defaults' comment rows are the source of truth
			// for what the table renders. Custom files' foot comments still
			// round-trip on save via the NodeTree.
			if e.IsComment() {
				continue
			}

			if _, isDefault := defaultKeys[e.Key]; isDefault {
				if e.Comment != "" {
					if _, seen := customComments[e.Key]; !seen {
						customComments[e.Key] = e.Comment
					}
				}

				if e.IsSection() {
					populatedSections[e.Key] = struct{}{}
				}

				continue
			}

			if _, seen := customOnly[e.Key]; seen {
				continue
			}

			customOnly[e.Key] = service.FlatValueEntry{
				Key:          e.Key,
				Type:         e.Type,
				Depth:        e.Depth,
				Comment:      e.Comment,
				IsCustomOnly: true,
			}
		}
	}

	// Second fast path: custom files only override keys that already exist in
	// defaults, so the unified list equals defaults. Skip the merge+sort but
	// still apply custom-side comment upgrades to the cloned entries.
	if len(customOnly) == 0 {
		withComments := applyCustomComments(defaults, customComments)
		service.SortByFilePositions(withComments, s.DefaultValues.KeyPositions)
		decorated := s.decorateWithCustomComments(withComments)

		if entriesEqual(prev, decorated) {
			return prev, false
		}

		s.Entries = decorated

		return prev, true
	}

	// applyCustomComments strips defaults' Comment field and rewrites it from
	// the customComments map; customOnly entries already carry their own
	// custom-side Comment from the source file's parseComments. Together this
	// guarantees every entry's Comment field reflects only the user's values
	// file annotations — defaults-side comments are never visible.
	//
	// promoteEmptyContainersToSections then flips defaults entries from leaf-
	// shaped placeholders ({} / []) to true sections when the overlay
	// populates them. Must run before merge+sort so the row's IsSection()
	// status is right by the time SortByFilePositions runs.
	defaultsWithComments := applyCustomComments(defaults, customComments)
	promoteEmptyContainersToSections(defaultsWithComments, populatedSections)

	merged := make([]service.FlatValueEntry, 0, len(defaults)+len(customOnly))
	merged = append(merged, defaultsWithComments...)

	for _, e := range customOnly {
		merged = append(merged, e)
	}

	// Sort by chart-defaults file position so the table mirrors values.yaml
	// layout. customOnly entries have no defaults position and sort to the
	// end alphabetically (handled by SortByFilePositions's tiebreak).
	service.SortByFilePositions(merged, s.DefaultValues.KeyPositions)

	decorated := s.decorateWithCustomComments(merged)

	if entriesEqual(prev, decorated) {
		return prev, false
	}

	s.Entries = decorated

	return prev, true
}

// applyCustomComments returns a copy of defaults whose Comment field reflects
// only the user's custom values file: keys present in customComments take that
// value, every other entry has its Comment cleared. The chart-defaults file's
// own comments — copyright headers, section dividers, leaf docs from helm
// charts — are never carried into the unified table; only the user's
// annotations on their values file appear.
//
// Callers compare the returned slice by value via entriesEqual, so identity
// doesn't matter; an empty customComments still returns a clone with every
// Comment stripped.
func applyCustomComments(defaults []service.FlatValueEntry, customComments map[string]string) []service.FlatValueEntry {
	out := slices.Clone(defaults)

	for i := range out {
		if c, ok := customComments[out[i].Key]; ok {
			out[i].Comment = c
		} else {
			out[i].Comment = ""
		}
	}

	return out
}

// promoteEmptyContainersToSections rewrites Value to "" on entries whose
// key is in the populated set AND whose current shape is the empty-
// container placeholder ({} / []). The chart-side flattener emits "{}" /
// "[]" for empty maps and lists; an overlay that populates the key emits
// a true section header (Value=""). When the two disagree, the unified
// row would otherwise inherit the chart's leaf shape — losing the
// section-comment editor, the chevron, and the indent grouping for the
// overlay's children. This pass aligns the row to the populated shape.
//
// Mutates entries in place — callers pass the cloned slice from
// applyCustomComments, not defaults directly.
func promoteEmptyContainersToSections(entries []service.FlatValueEntry, populated map[string]struct{}) {
	if len(populated) == 0 {
		return
	}

	for i := range entries {
		if !entries[i].IsEmptyContainer() {
			continue
		}

		if _, ok := populated[entries[i].Key]; ok {
			entries[i].Value = ""
		}
	}
}

// stripCommentsFrom returns a clone of entries with every Comment field
// cleared. Used in the no-custom-file fast path where there's no custom
// source to override defaults' comments — and we don't want to surface
// defaults' comments either.
func stripCommentsFrom(entries []service.FlatValueEntry) []service.FlatValueEntry {
	out := slices.Clone(entries)
	for i := range out {
		out[i].Comment = ""
	}

	return out
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
