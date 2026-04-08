package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/text"
	"gioui.org/x/explorer"
	"gopkg.in/yaml.v3"

	"github.com/qdeck-app/qdeck/config"
	"github.com/qdeck-app/qdeck/domain"
	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/async"
	"github.com/qdeck-app/qdeck/ui/page"
	"github.com/qdeck-app/qdeck/ui/revealer"
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
	path          string
	isChartPicker bool
	columnIdx     int
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
	RenderTemplateRunner *async.Runner[string]
	ExportRunner         *async.Runner[string]
	RecentValuesRunner   *async.Runner[[]domain.RecentValuesFile]
	FilePickerRunner     *async.Runner[filePickerResult]

	// OnOpenLocalChart is called when a chart file picker result is received.
	OnOpenLocalChart func(path string)

	// pendingSave tracks the kind of save for the current ExportRunner operation.
	// Safe to overwrite: ExportRunner.dispatch cancels prior ops, so pendingSave
	// always corresponds to the latest (and only deliverable) export result.
	pendingSave       saveKind
	saveColumnIdx     int
	chartSaveInFlight bool // true when ExportRunner was started by OnSaveChartVersion
	viewerLink        *customwidget.ViewerLink
	lastRenderMode    renderMode
}

func newValuesController(
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

	for i := range vc.CustomValuesRunners {
		vc.CustomValuesRunners[i] = async.NewRunner[*service.FlatValues](w, 1)
		vc.EditorParseRunners[i] = async.NewRunner[*service.FlatValues](w, 1)
	}

	return vc
}

func (vc *ValuesController) Callbacks() page.ValuesPageCallbacks {
	return page.ValuesPageCallbacks{
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
	}
}

// PollAsync consolidates all per-frame polling for values-related runners.
// Note: PullChartRunner is polled by Application directly, not here.
func (vc *ValuesController) PollAsync() {
	vc.pollDefaultValues()
	vc.pollCustomValues()
	vc.pollEditorParse()
	vc.pollRecentValues()
	vc.pollRenderRunner()
	vc.pollExportRunner()
	vc.pollFilePicker()
}

func (vc *ValuesController) pollDefaultValues() {
	if res, ok := vc.DefaultValuesRunner.Poll(); ok {
		vc.State.Loading = false

		if res.Err != nil {
			vc.NotifState.Show(res.Err.Error(), state.NotificationError, time.Now())
		} else {
			vc.State.DefaultValues = res.Value

			// Pre-allocate editors for all columns so +Values is instant.
			entryCount := len(res.Value.Entries)
			for i := range vc.State.Columns {
				vc.State.Columns[i].EnsureEditors(entryCount)
			}

			// Pre-allocate read-only editors for default value selection/copy.
			vc.State.EnsureDefaultEditors(entryCount)

			for i, entry := range res.Value.Entries {
				ed := &vc.State.DefaultValueEditors[i]
				ed.ReadOnly = true
				ed.SingleLine = true
				ed.Alignment = text.End
				ed.SetText(entry.Value)
			}
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

				vc.populateColumnOverrides(i)
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
				if col.CustomValues != nil && col.CustomValues.Indent > 0 {
					res.Value.Indent = col.CustomValues.Indent
				}

				col.CustomValues = res.Value
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
		vc.viewerLink = customwidget.OpenViewerWindow(title, errText, "")

		return
	}

	// If a viewer window is already open, send updated content to it.
	if vc.viewerLink != nil && vc.viewerLink.Send(res.Value) {
		return
	}

	title := vc.NavState.SelectedChart + " - Rendered Template"
	now := time.Now().Format("20060102-150405")
	saveFileName := vc.NavState.SelectedChart + "-" + vc.NavState.SelectedVersion + "-" + now + ".yaml"
	vc.viewerLink = customwidget.OpenViewerWindow(title, res.Value, saveFileName)
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

				vc.State.RebuildHelmInstallCmd()
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

	for i := range vc.CustomValuesRunners {
		vc.CustomValuesRunners[i].Stop()
		vc.EditorParseRunners[i].Stop()
	}

	vc.pendingSave = saveNone
	vc.chartSaveInFlight = false
	vc.viewerLink = nil
	vc.lastRenderMode = renderNone
	vc.State.Loading = true
	vc.State.DefaultValues = nil

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

func (vc *ValuesController) HandleSaveShortcut() {
	for c := range vc.State.ColumnCount {
		if vc.State.Columns[c].ValuesModified {
			vc.onSaveColumnValues(c)

			return
		}
	}
}

func (vc *ValuesController) HandleRenderDefaults() {
	vc.onRenderDefaults()
}

func (vc *ValuesController) HandleRenderOverrides() {
	vc.onRenderOverrides()
}

// Callbacks

func (vc *ValuesController) OnColumnFilesSelected(colIdx int, paths []string) {
	if colIdx < 0 || colIdx >= vc.State.ColumnCount || len(paths) == 0 {
		return
	}

	col := &vc.State.Columns[colIdx]
	col.CustomFilePaths = paths
	col.MergedFileCount = len(paths)

	vc.State.RebuildHelmInstallCmd()

	vc.CustomValuesRunners[colIdx].RunWithTimeout(config.ValuesLoadOperation, func(ctx context.Context) (*service.FlatValues, error) {
		return vc.ValuesService.LoadAndMergeCustomValues(ctx, paths)
	})

	// Add all paths to recent values, then refresh the list.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), config.TimeoutForOperation(config.RecentValuesLoadOperation))
		defer cancel()

		for _, p := range paths {
			if ctx.Err() != nil {
				return
			}

			if err := vc.RecentService.AddRecentValues(ctx, p); err != nil {
				slog.Warn("failed to save recent values entry", "path", p, "error", err)
			}
		}

		vc.LoadRecentValues()
	}()
}

func (vc *ValuesController) openFilePicker(result filePickerResult, extensions ...string) {
	vc.FilePickerRunner.RunBlocking(func() (filePickerResult, error) {
		reader, err := vc.Explorer.ChooseFile(extensions...)
		if err != nil {
			return filePickerResult{}, fmt.Errorf("file picker: %w", err)
		}

		defer func() { _ = reader.Close() }()

		f, ok := reader.(*os.File)
		if !ok {
			return filePickerResult{}, errors.New("file picker: unsupported platform reader type")
		}

		result.path = f.Name()

		return result, nil
	})
}

func (vc *ValuesController) onOpenColumnFile(colIdx int) {
	vc.openFilePicker(filePickerResult{columnIdx: colIdx}, ".yaml", ".yml", ".json")
}

func (vc *ValuesController) OnOpenChartFilePicker() {
	vc.openFilePicker(filePickerResult{isChartPicker: true}, ".tgz", ".yaml", ".yml")
}

func (vc *ValuesController) onRevealColumnFile(colIdx int) {
	if colIdx < 0 || colIdx >= state.MaxCustomColumns {
		return
	}

	col := &vc.State.Columns[colIdx]
	if len(col.CustomFilePaths) > 0 {
		revealer.RevealFile(col.CustomFilePaths[0])
	}
}

func (vc *ValuesController) onOpenInEditor(colIdx int) {
	if colIdx < 0 || colIdx >= state.MaxCustomColumns {
		return
	}

	col := &vc.State.Columns[colIdx]
	if len(col.CustomFilePaths) == 0 {
		return
	}

	filePath := col.CustomFilePaths[0]

	cmd := exec.CommandContext(context.Background(), "code", filePath) //nolint:gosec // user-selected file path, not untrusted input
	if err := cmd.Start(); err != nil {
		slog.Error("failed to open file in VS Code", "path", col.CustomFilePaths[0], "error", err)
	}
}

func (vc *ValuesController) onSelectRecentValues(path string) {
	vc.OnColumnFilesSelected(0, []string{path})
}

//nolint:dupl // same Runner pattern as addRecentChart but calls different service method on different receiver.
func (vc *ValuesController) onRemoveRecentValues(idx int) {
	vc.RecentValuesRunner.RunWithTimeout(config.RecentValuesLoadOperation, func(ctx context.Context) ([]domain.RecentValuesFile, error) {
		if err := vc.RecentService.RemoveRecentValues(ctx, idx); err != nil {
			return nil, fmt.Errorf("remove recent values: %w", err)
		}

		return vc.RecentService.ListRecentValues(ctx)
	})
}

func (vc *ValuesController) onKeyCopied(key string) {
	vc.NotifState.Show("Copied: "+key, state.NotificationSuccess, time.Now())
}

func (vc *ValuesController) onColumnOverrideChanged(colIdx int, yamlText string, err error) {
	if colIdx < 0 || colIdx >= state.MaxCustomColumns {
		return
	}

	col := &vc.State.Columns[colIdx]

	col.ValuesModified = true

	if err != nil {
		col.EditorParseError = err.Error()

		return
	}

	if yamlText == "" {
		col.CustomValues = nil
		col.EditorParseError = ""

		return
	}

	vc.EditorParseRunners[colIdx].RunWithTimeout(config.ValuesParseOperation, func(ctx context.Context) (*service.FlatValues, error) {
		return vc.ValuesService.ParseEditorContent(ctx, yamlText)
	})
}

func (vc *ValuesController) onSaveColumnValues(colIdx int) {
	if colIdx < 0 || colIdx >= state.MaxCustomColumns || vc.State.DefaultValues == nil {
		return
	}

	vc.pendingSave = saveValues
	vc.saveColumnIdx = colIdx

	col := &vc.State.Columns[colIdx]

	yamlText, err := state.OverridesToYAML(vc.State.DefaultValues.Entries, col.OverrideEditors, col.YAMLIndent())
	if err != nil {
		vc.NotifState.Show(err.Error(), state.NotificationError, time.Now())

		return
	}

	// If a file is already loaded, overwrite it directly without a save dialog.
	if len(col.CustomFilePaths) > 0 {
		path := col.CustomFilePaths[0]

		vc.ExportRunner.RunWithTimeout(config.FileExportOperation, func(ctx context.Context) (string, error) {
			if err := vc.ValuesService.SaveValuesFile(ctx, yamlText, path); err != nil {
				return "", fmt.Errorf("save values: %w", err)
			}

			return path, nil
		})

		return
	}

	vc.ExportRunner.RunWithTimeout(config.FileExportOperation, func(ctx context.Context) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		writer, err := vc.Explorer.CreateFile("values.yaml")
		if err != nil {
			return "", fmt.Errorf("save dialog: %w", err)
		}

		defer func() { _ = writer.Close() }()

		if _, err := writer.Write([]byte(yamlText)); err != nil {
			return "", fmt.Errorf("write values: %w", err)
		}

		// Try to get the file path from the writer.
		if f, ok := writer.(*os.File); ok {
			return f.Name(), nil
		}

		return "values.yaml", nil
	})
}

func (vc *ValuesController) onAddColumn() {
	if vc.State.ColumnCount < state.MaxCustomColumns {
		vc.State.ColumnCount++
	}
}

func (vc *ValuesController) onClearColumn(colIdx int) {
	if colIdx < 0 || colIdx >= vc.State.ColumnCount {
		return
	}

	vc.CustomValuesRunners[colIdx].Stop()
	vc.EditorParseRunners[colIdx].Stop()
	vc.State.Columns[colIdx].Reset()
	vc.State.RebuildHelmInstallCmd()
}

func (vc *ValuesController) onRemoveColumn(colIdx int) {
	if colIdx < 1 || colIdx >= vc.State.ColumnCount {
		return
	}

	// Cancel in-flight work for the removed column.
	vc.CustomValuesRunners[colIdx].Stop()
	vc.EditorParseRunners[colIdx].Stop()

	// Shift columns and their corresponding runners left.
	for i := colIdx; i < state.MaxCustomColumns-1; i++ {
		vc.State.Columns[i] = vc.State.Columns[i+1]
		vc.CustomValuesRunners[i] = vc.CustomValuesRunners[i+1]
		vc.EditorParseRunners[i] = vc.EditorParseRunners[i+1]
	}

	vc.State.Columns[state.MaxCustomColumns-1].Reset()
	vc.CustomValuesRunners[state.MaxCustomColumns-1] = async.NewRunner[*service.FlatValues](vc.Window, 1)
	vc.EditorParseRunners[state.MaxCustomColumns-1] = async.NewRunner[*service.FlatValues](vc.Window, 1)
	vc.State.ColumnCount--
	vc.State.RebuildHelmInstallCmd()

	// Clamp focused column to remain within active columns.
	if vc.State.FocusedCol >= vc.State.ColumnCount {
		vc.State.FocusedCol = max(0, vc.State.ColumnCount-1)
	}
}

func (vc *ValuesController) onSaveCurrentChart() {
	if vc.State.ChartPath == "" {
		return
	}

	vc.pendingSave = saveTgz

	vc.ExportRunner.RunWithTimeout(config.FileExportOperation, func(ctx context.Context) (string, error) {
		return vc.ChartService.SaveChartAsTarGz(ctx, vc.State.ChartPath)
	})
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

	col.EnsureEditors(len(vc.State.DefaultValues.Entries))

	rawValues := col.CustomValues.RawValues
	nodeTree := col.CustomValues.NodeTree
	indent := col.YAMLIndent()

	populated := 0

	for i, entry := range vc.State.DefaultValues.Entries {
		if val, ok := resolveCustomValueWithComments(customMap, commentMap, rawValues, nodeTree, entry.Key, indent); ok {
			col.OverrideEditors[i].SetText(val)

			populated++
		}
	}

	col.RebuildOverrideFlags()
	col.DrainPendingChanges = true
	col.ValuesModified = false
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

func (vc *ValuesController) onRenderDefaults() {
	chartPath := vc.State.ChartPath
	if chartPath == "" {
		return
	}

	vc.lastRenderMode = renderDefaults
	vc.State.RenderLoading = true

	vc.RenderTemplateRunner.RunWithTimeout(config.TemplateRenderOperation, func(ctx context.Context) (string, error) {
		return vc.TemplateService.RenderTemplate(ctx, chartPath, nil)
	})
}

func (vc *ValuesController) onRenderOverrides() {
	chartPath := vc.State.ChartPath
	if chartPath == "" || vc.State.DefaultValues == nil {
		return
	}

	vc.lastRenderMode = renderOverrides
	vc.State.RenderLoading = true

	// Collect override keys and values from all active columns.
	entries := vc.State.DefaultValues.Entries
	keys := make([]string, 0, len(entries))
	values := make([]string, 0, len(entries))

	for c := range vc.State.ColumnCount {
		eds := vc.State.Columns[c].OverrideEditors

		for i, entry := range entries {
			if i >= len(eds) {
				break
			}

			val := eds[i].Text()
			if val == "" {
				continue
			}

			keys = append(keys, entry.Key)
			values = append(values, val)
		}
	}

	vc.RenderTemplateRunner.RunWithTimeout(config.TemplateRenderOperation, func(ctx context.Context) (string, error) {
		vals, err := vc.ValuesService.BuildOverrideMap(keys, values)
		if err != nil {
			return "", fmt.Errorf("build override map: %w", err)
		}

		return vc.TemplateService.RenderTemplate(ctx, chartPath, vals)
	})
}

func (vc *ValuesController) OnSaveChartVersion(chartName, version string) {
	vc.ChartState.Loading = true
	vc.chartSaveInFlight = true
	vc.pendingSave = saveTgz

	vc.ExportRunner.RunWithTimeout(config.FileExportOperation, func(ctx context.Context) (string, error) {
		chartPath, err := vc.ChartService.PullChart(ctx, vc.NavState.SelectedRepo, chartName, version)
		if err != nil {
			return "", fmt.Errorf("pull chart: %w", err)
		}

		return vc.ChartService.SaveChartAsTarGz(ctx, chartPath)
	})
}
