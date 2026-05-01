package page

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/x/explorer"
	"gopkg.in/yaml.v3"

	"github.com/qdeck-app/qdeck/config"
	"github.com/qdeck-app/qdeck/domain"
	gitadapter "github.com/qdeck-app/qdeck/infrastructure/git"
	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/async"
	"github.com/qdeck-app/qdeck/ui/platform/revealer"
	"github.com/qdeck-app/qdeck/ui/state"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

// saveKind distinguishes between chart .tgz saves and values file saves.
type saveKind uint8

const (
	saveNone   saveKind = iota
	saveTgz             // chart .tgz export
	saveValues          // values file save
)

// renderMode remembers the last render type so we can re-render on values save.
type renderMode uint8

const (
	renderNone      renderMode = iota
	renderDefaults             // render with default values only
	renderOverrides            // render with user overrides applied
)

// filePickerResult bundles the path with context about which picker opened it.
type filePickerResult struct {
	path           string
	isChartPicker  bool
	isValuesPicker bool
	columnIdx      int
}

// ValuesController owns all values/column/render/save/recent-values concerns.
type ValuesController struct {
	Window     *app.Window
	NavState   *state.NavigationState
	State      *state.ValuesPageState
	ChartState *state.ChartPageState
	NotifState *state.NotificationState
	Explorer   *explorer.Explorer

	// Services
	ValuesService   *service.ValuesService
	TemplateService *service.TemplateService
	RecentService   *service.RecentService
	ChartService    *service.ChartService

	// Async runners
	DefaultValuesRunner  *async.Runner[*service.FlatValues]
	CustomValuesRunners  [state.MaxCustomColumns]*async.Runner[*service.FlatValues]
	EditorParseRunners   [state.MaxCustomColumns]*async.Runner[*service.FlatValues]
	GitCompareRunners    [state.MaxCustomColumns]*async.Runner[map[string]domain.GitChangeStatus]
	RenderTemplateRunner *async.Runner[string]
	ExportRunner         *async.Runner[string]
	RecentValuesRunner   *async.Runner[[]domain.RecentValuesFile]
	FilePickerRunner     *async.Runner[filePickerResult]
	ChartUIStateRunner   *async.Runner[chartUIStateResult]

	// CustomDecor indicates that windows should use custom decorations (Linux/Windows).
	CustomDecor bool

	// OnOpenLocalChart is called when a chart file picker result is received.
	OnOpenLocalChart func(path string)

	// OnPendingValuesFileSelected is called when a values file picker on the repos page yields a result.
	OnPendingValuesFileSelected func(path string)

	// OnPendingValuesConsumed is called after a pending values file is auto-loaded,
	// so the caller can record the values+chart pairing.
	OnPendingValuesConsumed func(path string)

	// pendingSave tracks the kind of save for the current ExportRunner operation.
	// Safe to overwrite: ExportRunner.dispatch cancels prior ops, so pendingSave
	// always corresponds to the latest (and only deliverable) export result.
	pendingSave       saveKind
	saveColumnIdx     int
	chartSaveInFlight bool // true when ExportRunner was started by OnSaveChartVersion
	viewerLink        *customwidget.ViewerLink
	lastRenderMode    renderMode

	// Overwrite confirmation dialog (shown when file changed on disk).
	overwriteDialog       customwidget.ConfirmDialog
	overwriteDialogYes    widget.Clickable
	overwriteDialogNo     widget.Clickable
	overwriteDialogActive bool
	overwritePendingCol   int
	overwritePendingYAML  string

	// focusSaver debounces per-chart cell-focus writes to avoid rewriting the
	// whole AppData JSON on every arrow-key frame.
	focusSaver *async.Debouncer[chartFocusJob]
}

// chartFocusJob is the payload coalesced by focusSaver: the most recent
// (chartKey, state) pair observed during the debounce window.
type chartFocusJob struct {
	chartKey string
	state    domain.ChartUIState
}

// chartUIStateResult is the async result of a per-chart UI-state load. The
// chartKey pins the result to a specific chart so late deliveries after a
// fast repo switch can be discarded (Runner generation counter already drops
// cancelled results, but pinning is cheap and makes intent explicit).
type chartUIStateResult struct {
	chartKey string
	state    domain.ChartUIState
	found    bool
}

// cellFocusSaveDelay is the quiet window after the last focus change before
// the state is written. Long enough to coalesce held-arrow-key bursts, short
// enough that the last position hits disk before the user closes the app.
const cellFocusSaveDelay = 300 * time.Millisecond

func NewValuesController(
	w *app.Window,
	navState *state.NavigationState,
	valuesState *state.ValuesPageState,
	chartState *state.ChartPageState,
	notifState *state.NotificationState,
	expl *explorer.Explorer,
	valuesSvc *service.ValuesService,
	templateSvc *service.TemplateService,
	recentSvc *service.RecentService,
	chartSvc *service.ChartService,
) *ValuesController {
	vc := &ValuesController{
		Window:          w,
		NavState:        navState,
		State:           valuesState,
		ChartState:      chartState,
		NotifState:      notifState,
		Explorer:        expl,
		ValuesService:   valuesSvc,
		TemplateService: templateSvc,
		RecentService:   recentSvc,
		ChartService:    chartSvc,
	}

	vc.DefaultValuesRunner = async.NewRunner[*service.FlatValues](w, 1)
	vc.RenderTemplateRunner = async.NewRunner[string](w, 1)
	vc.ExportRunner = async.NewRunner[string](w, 1)
	vc.RecentValuesRunner = async.NewRunner[[]domain.RecentValuesFile](w, 1)
	vc.FilePickerRunner = async.NewRunner[filePickerResult](w, 1)
	vc.ChartUIStateRunner = async.NewRunner[chartUIStateResult](w, 1)

	for i := range vc.CustomValuesRunners {
		vc.CustomValuesRunners[i] = async.NewRunner[*service.FlatValues](w, 1)
		vc.EditorParseRunners[i] = async.NewRunner[*service.FlatValues](w, 1)
		vc.GitCompareRunners[i] = async.NewRunner[map[string]domain.GitChangeStatus](w, 1)
	}

	vc.overwriteDialog.YesButton = &vc.overwriteDialogYes
	vc.overwriteDialog.NoButton = &vc.overwriteDialogNo

	vc.focusSaver = async.NewDebouncer(cellFocusSaveDelay, func(j chartFocusJob) {
		if err := recentSvc.SaveChartUIState(context.Background(), j.chartKey, j.state); err != nil {
			slog.Error("save chart ui state", "error", err, "key", j.chartKey)
		}
	})

	return vc
}

// Shutdown flushes any debounced writes so the last scheduled state reaches
// disk before the process exits. Call once from the app's shutdown path.
func (vc *ValuesController) Shutdown() {
	vc.focusSaver.Flush()
}

func (vc *ValuesController) Callbacks() ValuesPageCallbacks {
	return ValuesPageCallbacks{
		OnColumnFilesSelected:   vc.OnColumnFilesSelected,
		OnOpenColumnFile:        vc.onOpenColumnFile,
		OnRevealFile:            vc.onRevealColumnFile,
		OnOpenInEditor:          vc.onOpenInEditor,
		OnSaveChart:             vc.onSaveCurrentChart,
		OnColumnOverrideChanged: vc.onColumnOverrideChanged,
		OnSaveColumnValues:      vc.onSaveColumnValues,
		OnAddColumn:             vc.onAddColumn,
		OnClearColumn:           vc.onClearColumn,
		OnRemoveColumn:          vc.onRemoveColumn,
		OnSelectRecentValues:    vc.onSelectRecentValues,
		OnRemoveRecentValues:    vc.onRemoveRecentValues,
		OnRenderDefaults:        vc.onRenderDefaults,
		OnRenderOverrides:       vc.onRenderOverrides,
		OnKeyCopied:             vc.onKeyCopied,
		OnShowCommentsChanged:   vc.onShowCommentsChanged,
		OnCellFocusChanged:      vc.onCellFocusChanged,
		OnCollapseChanged:       vc.onCollapseChanged,
		OnAnchorCreate:          vc.onAnchorCreate,
		OnAnchorAlias:           vc.onAnchorAlias,
		OnUnlockCell:            vc.onUnlockCell,
		OnAnchorRename:          vc.onAnchorRename,
		OnAnchorDelete:          vc.onAnchorDelete,
	}
}

// PollAsync consolidates all per-frame polling for values-related runners.
// Note: PullChartRunner is polled by Application directly, not here.
func (vc *ValuesController) PollAsync() {
	vc.pollDefaultValues()
	vc.pollCustomValues()
	vc.pollEditorParse()
	vc.pollGitCompare()
	vc.pollRecentValues()
	vc.pollRenderRunner()
	vc.pollExportRunner()
	vc.pollFilePicker()
	vc.pollChartUIState()
}

func (vc *ValuesController) pollDefaultValues() {
	res, ok := vc.DefaultValuesRunner.Poll()
	if !ok {
		return
	}

	vc.State.Loading = false

	state.RecordLoadOutcome(res.Err, &vc.State.LoadError, vc.NotifState)

	if res.Err != nil {
		return
	}

	vc.State.DefaultValues = res.Value

	// Restore the previously focused cell for this chart so the user
	// resumes where they left off across app restarts. Load is async
	// to avoid blocking the frame loop on a file read that may
	// contend with the debounced save mutex.
	vc.loadSavedCellFocusAsync()

	vc.rebuildEntries(-1)

	// Auto-load a values file queued from the repos page drop zone.
	if vc.NavState.PendingValuesPath != "" {
		path := vc.NavState.PendingValuesPath
		vc.NavState.PendingValuesPath = ""
		vc.OnColumnFilesSelected(0, []string{path})

		if vc.OnPendingValuesConsumed != nil {
			vc.OnPendingValuesConsumed(path)
		}
	}
}

// rebuildEntries recomputes the unified entry list (chart defaults merged with
// custom-only keys from active columns), resizes and re-aligns the default and
// per-column editor slices by flat key, and refreshes the read-only default-
// value cells. No-op when nothing changed, so it's cheap to call from any
// source-mutation point.
//
// skipRealignCol (-1 for none) suppresses the by-key realign pass for one
// column — used by callers that will authoritatively repopulate that column's
// editors from its CustomValues immediately afterward (see pollCustomValues),
// so shifting stale text around would just be wasted work.
func (vc *ValuesController) rebuildEntries(skipRealignCol int) {
	oldEntries, changed := vc.State.RebuildUnifiedEntries()
	if !changed {
		return
	}

	newLen := len(vc.State.Entries)

	// Pre-allocate editors for every column (active + inactive) so +Values is
	// instant and arrow-key nav never races an editor realloc.
	vc.State.EnsureDefaultEditors(newLen)

	for c := range vc.State.Columns {
		vc.State.Columns[c].EnsureEditors(newLen)
	}

	// Default-side is read-only and purely derived from entry.Value; re-apply
	// configuration so freshly grown slots behave as read-only selectable
	// cells too. Slots past newLen are cleared so leftover text from a prior
	// (longer) entry list never bleeds into the DOM.
	for i := range vc.State.DefaultValueEditors {
		ed := &vc.State.DefaultValueEditors[i]
		ed.ReadOnly = true
		ed.SingleLine = true
		ed.Alignment = text.End

		if i < newLen {
			ed.SetText(vc.State.Entries[i].Value)
		} else {
			ed.SetText("")
		}
	}

	// Realign per-column override editors by flat key so user-entered text
	// survives when the unified list grows or shrinks (e.g. loading a custom
	// file into column 2 introduces keys that shift column 1's indices).
	for c := range vc.State.Columns {
		if c == skipRealignCol {
			continue
		}

		col := &vc.State.Columns[c]
		vc.State.RealignEditorTextByKey(oldEntries, col.OverrideEditors)
		col.RebuildOverrideFlags()

		if len(col.OverrideEditors) > 0 {
			col.DrainPendingChanges = true
		}
	}
}

func (vc *ValuesController) pollCustomValues() {
	for i := range vc.CustomValuesRunners {
		if res, ok := vc.CustomValuesRunners[i].Poll(); ok {
			col := &vc.State.Columns[i]

			if res.Err != nil {
				vc.NotifState.Show(res.Err.Error(), state.NotificationError, time.Now())
			} else {
				col.CustomValues = res.Value

				// Rebuild the unified entry list first so custom-only keys
				// get rows and editor slots before populateColumnOverrides
				// tries to set their text. Skip realign for column i because
				// populateColumnOverrides is about to authoritatively rewrite
				// every editor in that column from the just-loaded file.
				vc.rebuildEntries(i)
				vc.populateColumnOverrides(i)
				vc.triggerGitCompare(i)
			}
		}
	}
}

func (vc *ValuesController) triggerGitCompare(colIdx int) {
	col := &vc.State.Columns[colIdx]

	// Only compare single-file columns; merged multi-file columns have ambiguous baselines.
	if len(col.CustomFilePaths) != 1 {
		col.GitChanges = nil

		return
	}

	filePath := col.CustomFilePaths[0]

	vc.GitCompareRunners[colIdx].RunWithTimeout(config.GitCompareOperation, func(ctx context.Context) (map[string]domain.GitChangeStatus, error) {
		headContent, err := gitadapter.ShowHEAD(ctx, filePath)
		if err != nil {
			return nil, fmt.Errorf("git show HEAD for %s: %w", filePath, err)
		}

		return vc.ValuesService.CompareWithBaseline(ctx, filePath, headContent)
	})
}

func (vc *ValuesController) pollGitCompare() {
	for i := range vc.GitCompareRunners {
		if res, ok := vc.GitCompareRunners[i].Poll(); ok {
			if res.Err != nil {
				// Silently skip — git not available or file untracked.
				vc.State.Columns[i].GitChanges = nil
			} else {
				vc.State.Columns[i].GitChanges = res.Value
			}
		}
	}
}

func (vc *ValuesController) pollEditorParse() {
	for i := range vc.EditorParseRunners {
		if res, ok := vc.EditorParseRunners[i].Poll(); ok {
			col := &vc.State.Columns[i]

			if res.Err != nil {
				col.EditorParseError = res.Err.Error()
			} else {
				col.EditorParseError = ""

				// Preserve detected YAML indent from the originally loaded file.
				// Also preserve the parsed yaml.Node tree: it represents the
				// on-disk structure (anchors, aliases, comments, styles) and
				// stays authoritative across edits — ParseEditorContent does
				// not reparse it, and PatchNodeTree works against a deep copy
				// so successive saves start from the same original tree.
				// Doc-level orphan comments (banner/trailer/per-leaf foots)
				// are similarly load-time metadata: ParseEditorContent only
				// sees the editor's value text, so it can't recover them —
				// carry them across so the banner strip and comment-row
				// renderers don't lose their data on every keystroke.
				if col.CustomValues != nil {
					if col.CustomValues.Indent > 0 {
						res.Value.Indent = col.CustomValues.Indent
					}

					if col.CustomValues.NodeTree != nil {
						res.Value.NodeTree = col.CustomValues.NodeTree
					}

					if col.CustomValues.Anchors != nil {
						res.Value.Anchors = col.CustomValues.Anchors
					}

					if col.CustomValues.DocHeadComment != "" {
						res.Value.DocHeadComment = col.CustomValues.DocHeadComment
					}

					if col.CustomValues.DocFootComment != "" {
						res.Value.DocFootComment = col.CustomValues.DocFootComment
					}

					if col.CustomValues.FootComments != nil {
						res.Value.FootComments = col.CustomValues.FootComments
					}
				}

				col.CustomValues = res.Value

				// Rebuild the unified entry list: cell edits can introduce
				// keys that weren't in defaults (e.g. pasting a nested map
				// expands into new flat keys). Realign the typing column too
				// (no skip) — its editor text is the source of truth here, and
				// realign's text-equality guard keeps the active caret intact.
				vc.rebuildEntries(-1)
			}
		}
	}
}

func (vc *ValuesController) pollRecentValues() {
	if res, ok := vc.RecentValuesRunner.Poll(); ok {
		if res.Err != nil {
			vc.NotifState.Show(res.Err.Error(), state.NotificationError, time.Now())
		} else {
			vc.State.RecentValuesFiles = res.Value
		}
	}
}

func (vc *ValuesController) pollRenderRunner() {
	res, ok := vc.RenderTemplateRunner.Poll()
	if !ok {
		return
	}

	vc.State.RenderLoading = false

	if res.Err != nil {
		errText := res.Err.Error()

		if vc.viewerLink != nil && vc.viewerLink.Send(errText) {
			return
		}

		title := vc.NavState.SelectedChart + " - Render Error"
		vc.viewerLink = customwidget.OpenViewerWindow(title, errText, "", vc.CustomDecor)

		return
	}

	// If a viewer window is already open, send updated content to it.
	if vc.viewerLink != nil && vc.viewerLink.Send(res.Value) {
		return
	}

	title := vc.NavState.SelectedChart + " - Rendered Template"
	now := time.Now().Format("20060102-150405")
	saveFileName := vc.NavState.SelectedChart + "-" + vc.NavState.SelectedVersion + "-" + now + ".yaml"
	vc.viewerLink = customwidget.OpenViewerWindow(title, res.Value, saveFileName, vc.CustomDecor)
}

func (vc *ValuesController) pollExportRunner() {
	res, ok := vc.ExportRunner.Poll()
	if !ok {
		return
	}

	if vc.chartSaveInFlight {
		vc.ChartState.Loading = false
		vc.chartSaveInFlight = false
	}

	if res.Err != nil {
		vc.NotifState.Show(res.Err.Error(), state.NotificationError, time.Now())
	} else {
		vc.NotifState.Show("Saved to "+res.Value, state.NotificationSuccess, time.Now())

		switch vc.pendingSave {
		case saveValues:
			if vc.saveColumnIdx >= 0 && vc.saveColumnIdx < vc.State.ColumnCount {
				col := &vc.State.Columns[vc.saveColumnIdx]
				col.ValuesModified = false
				col.CustomFilePaths = []string{res.Value}

				if info, err := os.Stat(res.Value); err == nil {
					col.FileModTime = info.ModTime()
				}

				vc.State.RebuildHelmInstallCmd()
				vc.triggerGitCompare(vc.saveColumnIdx)
			}

			vc.reRenderIfViewerOpen()
		case saveTgz:
			revealer.RevealFile(res.Value)
		case saveNone:
			// No-op.
		}
	}

	vc.pendingSave = saveNone
}

func (vc *ValuesController) pollFilePicker() {
	res, ok := vc.FilePickerRunner.Poll()
	if !ok {
		return
	}

	switch {
	case res.Err != nil:
		if !errors.Is(res.Err, explorer.ErrUserDecline) {
			vc.NotifState.Show(res.Err.Error(), state.NotificationError, time.Now())
		}
	case res.Value.isChartPicker:
		if vc.OnOpenLocalChart != nil {
			vc.OnOpenLocalChart(res.Value.path)
		}
	case res.Value.isValuesPicker:
		if vc.OnPendingValuesFileSelected != nil {
			vc.OnPendingValuesFileSelected(res.Value.path)
		}
	default:
		vc.OnColumnFilesSelected(res.Value.columnIdx, []string{res.Value.path})
	}
}

// Public methods called by Application

func (vc *ValuesController) LoadDefaultValues(chartPath string) {
	vc.State.Loading = true
	vc.DefaultValuesRunner.RunWithTimeout(config.ValuesLoadOperation, func(ctx context.Context) (*service.FlatValues, error) {
		return vc.ValuesService.LoadDefaultValues(ctx, chartPath)
	})
}

func (vc *ValuesController) LoadRecentValues() {
	vc.RecentValuesRunner.RunWithTimeout(config.RecentValuesLoadOperation, func(ctx context.Context) ([]domain.RecentValuesFile, error) {
		return vc.RecentService.ListRecentValues(ctx)
	})
}

func (vc *ValuesController) ResetState() {
	// Stop all in-flight runners to prevent stale results from a previous
	// chart being applied after state is cleared.
	vc.DefaultValuesRunner.Stop()
	vc.RenderTemplateRunner.Stop()
	vc.ExportRunner.Stop()
	vc.ChartUIStateRunner.Stop()

	for i := range vc.CustomValuesRunners {
		vc.CustomValuesRunners[i].Stop()
		vc.EditorParseRunners[i].Stop()
		vc.GitCompareRunners[i].Stop()
	}

	vc.pendingSave = saveNone
	vc.chartSaveInFlight = false
	vc.viewerLink = nil
	vc.lastRenderMode = renderNone
	vc.State.Loading = true
	vc.State.LoadError = ""
	vc.State.DefaultValues = nil
	vc.State.Entries = nil
	vc.State.PendingFocusKey = ""
	vc.State.PendingFocusHighlight = false
	vc.State.FocusHighlightAttempts = 0
	vc.State.FocusedRow = 0
	vc.State.FocusedCol = 0
	vc.State.CollapsedKeys = nil
	vc.State.CollapsedPreSearch = nil
	vc.State.SearchCollapseActive = false
	vc.State.SearchEditor.SetText("")

	for i := range vc.State.DefaultValueEditors {
		vc.State.DefaultValueEditors[i].SetText("")
	}

	vc.NotifState.Clear()
	vc.State.ColumnCount = 1
	vc.State.RecentDropdownOpen = false
	vc.State.ChartName = ""
	vc.State.RepoName = ""
	vc.State.HelmInstallCmd = ""
	vc.State.RenderLoading = false

	for i := range vc.State.Columns {
		vc.State.Columns[i].Reset()
	}
}

func (vc *ValuesController) populateColumnOverrides(colIdx int) {
	col := &vc.State.Columns[colIdx]

	if vc.State.DefaultValues == nil || col.CustomValues == nil {
		return
	}

	// Build lookup from custom values (flat key match).
	customMap := make(map[string]string, len(col.CustomValues.Entries))
	commentMap := make(map[string]string, len(col.CustomValues.Entries))

	for _, e := range col.CustomValues.Entries {
		if e.Value != "" {
			customMap[e.Key] = e.Value
		}

		if e.Comment != "" {
			commentMap[e.Key] = e.Comment
		}
	}

	col.EnsureEditors(len(vc.State.Entries))

	rawValues := col.CustomValues.RawValues
	nodeTree := col.CustomValues.NodeTree
	indent := col.YAMLIndent()

	// populateColumnOverrides is authoritative for this column: the loaded file
	// is the source of truth, so slots whose keys are absent from the new file
	// get cleared. Without this, loading a replacement file leaves behind text
	// for keys that existed only in the previous file.
	//
	// Comment rows reuse the same per-entry editor slot but are populated with
	// the cleaned (no "# " prefix) text of the corresponding banner / trailer /
	// foot block, so the user can edit the prose directly. Only the v1 source
	// column (column 0) populates these — see commentSourceCol in widget pkg.
	for i, entry := range vc.State.Entries {
		ed := &col.OverrideEditors[i]

		var val string

		switch {
		case entry.IsComment():
			val = commentInitialText(colIdx, col.CustomValues, entry, vc.State.Entries, i)
		case entry.IsSection():
			// Sections have no value editor; their slot in the column's editor
			// pool is repurposed for the section-row comment editor (v1 only
			// in commentSourceCol). entry.Comment was narrowed to custom-side
			// only in RebuildUnifiedEntries, so this is the user's section
			// comment text from their values file (cleaned, no "#" prefixes).
			if colIdx == commentSourceColIdx {
				val = entry.Comment
			}
		default:
			if v, ok := resolveCustomValueWithComments(customMap, commentMap, rawValues, nodeTree, entry.Key, indent); ok {
				val = v
			}
		}

		if ed.Text() != val {
			ed.SetText(val)
		}
	}

	col.RebuildOverrideFlags()
	col.DrainPendingChanges = true
	col.ValuesModified = false
}

// commentSourceColIdx is the column whose editor pool backs both orphan
// comment rows and section comment edits — kept in sync with the widget-
// package commentSourceCol constant. v1 sources comments from column 0
// only; multi-column lanes are a follow-up.
const commentSourceColIdx = 0

// commentInitialText returns the cleaned text to seed a comment-row editor
// with on column load. Banner and trailer rows have empty FootAfterKey;
// banner is identified positionally as the first comment row before any
// non-comment entry, trailer as a comment row after at least one
// non-comment entry. Foot-block rows source from cv.FootComments[FootAfterKey].
//
// V1 sources comments only from the comment-source column (column 0). Other
// columns get blank text in their comment-row editor slots so editing in a
// non-source column is a no-op rather than a confusing duplicate input.
func commentInitialText(
	colIdx int,
	cv *service.FlatValues,
	entry service.FlatValueEntry,
	allEntries []service.FlatValueEntry,
	entryIdx int,
) string {
	if cv == nil || colIdx != 0 {
		return ""
	}

	if entry.FootAfterKey != "" {
		raw, ok := cv.FootComments[entry.FootAfterKey]
		if !ok {
			return ""
		}

		return service.CleanCommentForDisplay(raw)
	}

	// Banner vs trailer: scan upward — if any earlier entry is non-comment,
	// this row sits after the data block, so it's the trailer.
	seenNonComment := false

	for i := 0; i < entryIdx && i < len(allEntries); i++ {
		if !allEntries[i].IsComment() {
			seenNonComment = true

			break
		}
	}

	if seenNonComment {
		return service.CleanCommentForDisplay(cv.DocFootComment)
	}

	return service.CleanCommentForDisplay(cv.DocHeadComment)
}

// resolveCustomValueWithComments resolves a custom value for editor display, preserving YAML comments.
// For scalar values found in customMap, it prepends any associated comment from commentMap.
// For map/list values, it tries yaml.Node-based serialization first (preserving inline comments),
// then falls back to plain SerializeValue.
func resolveCustomValueWithComments(
	customMap, commentMap map[string]string,
	rawValues map[string]any,
	nodeTree *yaml.Node,
	key string,
	indent int,
) (string, bool) {
	// Scalar value with direct flat key match.
	if val, ok := customMap[key]; ok {
		return formatCommentForEditor(commentMap[key], val), true
	}

	if rawValues == nil {
		return "", false
	}

	rawVal, found, err := service.LookupRawValue(rawValues, key)
	if err != nil || !found {
		return "", false
	}

	// Try yaml.Node serialization first to preserve comments.
	if nodeVal, ok := service.SerializeNodeSubtree(nodeTree, key, indent); ok {
		return nodeVal, true
	}

	// Fall back to plain serialization (no comments).
	return formatCommentForEditor(commentMap[key], service.SerializeValue(rawVal)), true
}

// formatCommentForEditor prepends YAML comment lines before the value.
// Each comment line is prefixed with "# ". Returns just the value if comment is empty.
func formatCommentForEditor(comment, value string) string {
	if comment == "" {
		return value
	}

	var b strings.Builder

	lines := strings.Split(comment, "\n")

	for _, line := range lines {
		b.WriteString("# ")
		b.WriteString(line)
		b.WriteByte('\n')
	}

	b.WriteString(value)

	return b.String()
}

// reRenderIfViewerOpen re-triggers the last render when a viewer window is open.
func (vc *ValuesController) reRenderIfViewerOpen() {
	if vc.viewerLink == nil {
		return
	}

	switch vc.lastRenderMode {
	case renderDefaults:
		vc.onRenderDefaults()
	case renderOverrides:
		vc.onRenderOverrides()
	case renderNone:
		// No previous render to repeat.
	}
}

// persistedCollapsedKeys returns the authoritative collapsed-keys set that
// should be written to disk. During an active search the user's intent lives
// in CollapsedPreSearch (CollapsedKeys has been transiently mutated by the
// search auto-uncollapse); otherwise CollapsedKeys is the source of truth.
func (vc *ValuesController) persistedCollapsedKeys() map[string]bool {
	if vc.State.SearchCollapseActive {
		return vc.State.CollapsedPreSearch
	}

	return vc.State.CollapsedKeys
}

// loadSavedCellFocusAsync dispatches a background load of this chart's
// persisted UI state. Results are applied by pollChartUIState so the frame
// loop is never blocked on disk I/O. Safe to call only after
// State.RepoName/ChartName/etc. are set — the key is captured at dispatch
// time so a concurrent ResetState cannot race with the goroutine.
func (vc *ValuesController) loadSavedCellFocusAsync() {
	key := vc.State.ChartKey()
	if key == "" {
		return
	}

	recentSvc := vc.RecentService

	vc.ChartUIStateRunner.RunWithTimeout(config.ChartUIStateLoadOperation, func(ctx context.Context) (chartUIStateResult, error) {
		st, ok, err := recentSvc.LoadChartUIState(ctx, key)
		if err != nil {
			return chartUIStateResult{}, fmt.Errorf("load chart ui state: %w", err)
		}

		return chartUIStateResult{chartKey: key, state: st, found: ok}, nil
	})
}

// pollChartUIState applies a completed UI-state load to the page state. The
// result is discarded when the user has navigated to a different chart
// mid-flight (the captured key no longer matches). Keyboard focus is only
// grabbed into an editor when saved state was actually found — first-time
// chart loads leave focus free so unmodified arrow keys aren't captured by
// the editor the user hasn't chosen yet.
func (vc *ValuesController) pollChartUIState() {
	res, ok := vc.ChartUIStateRunner.Poll()
	if !ok {
		return
	}

	if res.Err != nil {
		slog.Error("load chart ui state", "error", res.Err)

		return
	}

	// Drop stale deliveries: the chart changed while the load was in flight.
	if res.Value.chartKey != vc.State.ChartKey() {
		return
	}

	if !res.Value.found {
		return
	}

	vc.State.PendingFocusKey = res.Value.state.FocusedKey
	vc.State.FocusedCol = res.Value.state.FocusedCol
	vc.State.PendingFocusHighlight = true
	vc.State.FocusHighlightAttempts = 0
	vc.State.CollapsedKeys = collapsedSliceToMap(res.Value.state.CollapsedKeys)
}

// collapsedSliceToMap builds the runtime map view from the persisted slice.
// Returns nil when the slice is empty so absence stays cheap to check.
func collapsedSliceToMap(keys []string) map[string]bool {
	if len(keys) == 0 {
		return nil
	}

	m := make(map[string]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}

	return m
}

// collapsedMapToSlice produces a sorted slice for stable JSON serialization.
func collapsedMapToSlice(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}

	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	slices.Sort(out)

	return out
}

// cellValueAndType returns the user-visible value and YAML type for the cell
// at flatKey in column colIdx. The editor's current text wins when non-empty
// (user override), falling back to the unified entry's default value. Type
// comes from the unified entry so numeric/bool cells stay typed in YAML
// after being inserted via EnsurePath.
func (vc *ValuesController) cellValueAndType(colIdx int, flatKey string) (string, string) {
	if colIdx < 0 || colIdx >= state.MaxCustomColumns {
		return "", ""
	}

	col := &vc.State.Columns[colIdx]

	for i, entry := range vc.State.Entries {
		if entry.Key != flatKey {
			continue
		}

		if i < len(col.OverrideEditors) {
			if text := col.OverrideEditors[i].Text(); text != "" {
				return text, entry.Type
			}
		}

		return entry.Value, entry.Type
	}

	return "", ""
}

// syncEditorToAliasedValue updates the editor text for flatKey in col to
// reflect the scalar value the newly-created alias now resolves to. Without
// this the editor would keep the pre-alias text and PatchNodeTree would
// break the alias on save.
func (vc *ValuesController) syncEditorToAliasedValue(col *state.CustomColumnState, flatKey string) {
	if col.CustomValues == nil || col.CustomValues.NodeTree == nil {
		return
	}

	for i, entry := range vc.State.Entries {
		if entry.Key != flatKey {
			continue
		}

		if i >= len(col.OverrideEditors) {
			return
		}

		resolved, ok := service.EffectiveScalarAt(col.CustomValues.NodeTree, flatKey)
		if !ok {
			return
		}

		col.OverrideEditors[i].SetText(resolved)
		col.MarkOverride(i, resolved != "")

		return
	}
}
