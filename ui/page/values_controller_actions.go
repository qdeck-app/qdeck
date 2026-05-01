package page

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gioui.org/layout"
	"gioui.org/widget/material"
	"gopkg.in/yaml.v3"

	"github.com/qdeck-app/qdeck/config"
	"github.com/qdeck-app/qdeck/domain"
	"github.com/qdeck-app/qdeck/infrastructure/executil"
	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/async"
	"github.com/qdeck-app/qdeck/ui/platform/revealer"
	"github.com/qdeck-app/qdeck/ui/state"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

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

	for _, p := range paths {
		cleaned := filepath.Clean(p)

		for c := range vc.State.ColumnCount {
			if c == colIdx {
				continue
			}

			for _, existing := range vc.State.Columns[c].CustomFilePaths {
				if filepath.Clean(existing) != cleaned {
					continue
				}

				msg := fmt.Sprintf("Already open in column %d: %s — selection skipped", c+1, filepath.Base(p))
				vc.NotifState.Show(msg, state.NotificationError, time.Now())

				return
			}
		}
	}

	col := &vc.State.Columns[colIdx]
	col.CustomFilePaths = paths
	col.MergedFileCount = len(paths)

	if info, err := os.Stat(paths[0]); err == nil {
		col.FileModTime = info.ModTime()
	}

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

// OpenValuesFilePicker opens a file picker for values files on the repos page.
func (vc *ValuesController) OpenValuesFilePicker() {
	vc.openFilePicker(filePickerResult{isValuesPicker: true}, ".yaml", ".yml")
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
	executil.HideWindow(cmd)

	if err := cmd.Start(); err != nil {
		slog.Error("failed to open file in VS Code", "path", col.CustomFilePaths[0], "error", err)
	}
}

func (vc *ValuesController) onSelectRecentValues(path string) {
	// Route to the first empty column so users can stack a recent file into a
	// freshly-added column. Falls back to the focused column when every column
	// is populated — overwriting where the user is actively working matches
	// "load this here" intent better than clobbering column 0, and the
	// duplicate guard in OnColumnFilesSelected still rejects a re-pick of a
	// file already loaded in a different column.
	target := vc.State.FocusedCol
	for c := range vc.State.ColumnCount {
		if len(vc.State.Columns[c].CustomFilePaths) == 0 {
			target = c

			break
		}
	}

	vc.OnColumnFilesSelected(target, []string{path})
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

	var (
		tree *yaml.Node
		docs service.DocComments
	)

	if col.CustomValues != nil {
		tree = col.CustomValues.NodeTree
		docs = col.DocCommentsForSave()
	}

	yamlText, err := state.OverridesToYAML(vc.State.Entries, col.OverrideEditors, col.YAMLIndent(), tree, docs)
	if err != nil {
		vc.NotifState.Show(err.Error(), state.NotificationError, time.Now())

		return
	}

	// If a file is already loaded, overwrite it directly without a save dialog.
	if len(col.CustomFilePaths) > 0 {
		path := col.CustomFilePaths[0]

		// Check if the file was modified externally since we loaded it.
		if !col.FileModTime.IsZero() {
			if info, err := os.Stat(path); err == nil && info.ModTime().After(col.FileModTime) {
				vc.overwriteDialogActive = true
				vc.overwritePendingCol = colIdx
				vc.overwritePendingYAML = yamlText

				return
			}
		}

		vc.saveToFile(yamlText, path)

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

func (vc *ValuesController) saveToFile(yamlText, path string) {
	vc.ExportRunner.RunWithTimeout(config.FileExportOperation, func(ctx context.Context) (string, error) {
		if err := vc.ValuesService.SaveValuesFile(ctx, yamlText, path); err != nil {
			return "", fmt.Errorf("save values: %w", err)
		}

		return path, nil
	})
}

// IsOverwriteDialogActive reports whether the overwrite confirmation dialog is visible.
func (vc *ValuesController) IsOverwriteDialogActive() bool {
	return vc.overwriteDialogActive
}

// DismissOverwriteDialog hides the overwrite confirmation dialog without saving.
func (vc *ValuesController) DismissOverwriteDialog() {
	vc.overwriteDialogActive = false
	vc.overwritePendingYAML = ""
}

// HandleOverwriteDialog processes confirm/cancel clicks on the overwrite-changes dialog.
func (vc *ValuesController) HandleOverwriteDialog(gtx layout.Context) {
	if !vc.overwriteDialogActive {
		return
	}

	switch vc.overwriteDialog.Update(gtx) {
	case customwidget.ConfirmYes:
		vc.overwriteDialogActive = false

		col := &vc.State.Columns[vc.overwritePendingCol]
		if len(col.CustomFilePaths) > 0 {
			vc.pendingSave = saveValues
			vc.saveColumnIdx = vc.overwritePendingCol
			vc.saveToFile(vc.overwritePendingYAML, col.CustomFilePaths[0])
		}

		vc.overwritePendingYAML = ""
	case customwidget.ConfirmNo:
		vc.DismissOverwriteDialog()
	}
}

// LayoutOverwriteDialog renders the overwrite confirmation dialog if active.
func (vc *ValuesController) LayoutOverwriteDialog(gtx layout.Context, th *material.Theme) layout.Dimensions {
	if !vc.overwriteDialogActive {
		return layout.Dimensions{}
	}

	return vc.overwriteDialog.Layout(gtx, th, "This file has been modified externally. Overwrite changes?")
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
	vc.GitCompareRunners[colIdx].Stop()
	vc.State.Columns[colIdx].Reset()
	vc.State.RebuildHelmInstallCmd()
	// Cleared custom file may have contributed custom-only keys to the
	// unified entries; drop them and re-align remaining columns' editors.
	vc.rebuildEntries(-1)
}

func (vc *ValuesController) onRemoveColumn(colIdx int) {
	if colIdx < 1 || colIdx >= vc.State.ColumnCount {
		return
	}

	// Cancel in-flight work for the removed column.
	vc.CustomValuesRunners[colIdx].Stop()
	vc.EditorParseRunners[colIdx].Stop()
	vc.GitCompareRunners[colIdx].Stop()

	// Shift columns and their corresponding runners left.
	for i := colIdx; i < state.MaxCustomColumns-1; i++ {
		vc.State.Columns[i] = vc.State.Columns[i+1]
		vc.CustomValuesRunners[i] = vc.CustomValuesRunners[i+1]
		vc.EditorParseRunners[i] = vc.EditorParseRunners[i+1]
		vc.GitCompareRunners[i] = vc.GitCompareRunners[i+1]
	}

	vc.State.Columns[state.MaxCustomColumns-1].Reset()
	vc.CustomValuesRunners[state.MaxCustomColumns-1] = async.NewRunner[*service.FlatValues](vc.Window, 1)
	vc.EditorParseRunners[state.MaxCustomColumns-1] = async.NewRunner[*service.FlatValues](vc.Window, 1)
	vc.GitCompareRunners[state.MaxCustomColumns-1] = async.NewRunner[map[string]domain.GitChangeStatus](vc.Window, 1)
	vc.State.ColumnCount--
	vc.State.RebuildHelmInstallCmd()

	// A removed column may have contributed custom-only keys to the unified
	// entries; drop them now and shift remaining columns' editors by key so
	// their in-flight text survives the realignment.
	vc.rebuildEntries(-1)

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
	entries := vc.State.Entries
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

func (vc *ValuesController) onShowCommentsChanged(show bool) {
	go func() {
		if err := vc.RecentService.SaveShowComments(context.Background(), show); err != nil {
			slog.Error("save show comments preference", "error", err)
		}
	}()
}

func (vc *ValuesController) onCellFocusChanged(entryKey string, col int) {
	chartKey := vc.State.ChartKey()
	if chartKey == "" {
		return
	}

	// Debounce the disk write: arrow-key nav can fire focus changes per frame,
	// and each save rewrites the whole AppData JSON. focusSaver coalesces
	// rapid changes into a single write after the user settles.
	vc.focusSaver.Schedule(chartFocusJob{
		chartKey: chartKey,
		state: domain.ChartUIState{
			FocusedKey:    entryKey,
			FocusedCol:    col,
			CollapsedKeys: collapsedMapToSlice(vc.persistedCollapsedKeys()),
		},
	})
}

// onCollapseChanged debounces persistence of the collapsed-keys set onto the
// chart's UIState. Reuses focusSaver so one debounced write covers both focus
// and collapse changes; each job carries the full snapshot the page is in.
func (vc *ValuesController) onCollapseChanged() {
	chartKey := vc.State.ChartKey()
	if chartKey == "" {
		return
	}

	entryKey := vc.State.FocusedEntryKey()

	vc.focusSaver.Schedule(chartFocusJob{
		chartKey: chartKey,
		state: domain.ChartUIState{
			FocusedKey:    entryKey,
			FocusedCol:    vc.State.FocusedCol,
			CollapsedKeys: collapsedMapToSlice(vc.persistedCollapsedKeys()),
		},
	})
}

// ensureBlankCustomValues initializes col.CustomValues with an empty mapping
// when entirely missing, so anchor/alias mutations work on a freshly-created
// column whose file has not been loaded or saved yet. The tree is materialized
// lazily here rather than at column creation so columns that stay empty don't
// get marked as synthetic values.
//
// Returns false when CustomValues already exists but NodeTree is nil — i.e.
// entries loaded successfully but yaml.Unmarshal failed on the raw bytes.
// Overwriting with a fresh empty mapping in that state would silently drop
// every loaded entry on the next save, so the caller must surface an error
// instead. Truly-empty columns return true.
func ensureBlankCustomValues(col *state.CustomColumnState) bool {
	if col.CustomValues == nil {
		col.CustomValues = &service.FlatValues{
			NodeTree: &yaml.Node{Kind: yaml.MappingNode},
			Indent:   service.DefaultYAMLIndent,
		}

		return true
	}

	return col.CustomValues.NodeTree != nil
}

// onAnchorCreate declares a YAML anchor on the cell at flatKey in column
// colIdx. Mutates the column's NodeTree directly so the next save preserves
// the anchor. Refreshes the Anchors map so the badge updates, and marks the
// column modified to enable the Save button.
func (vc *ValuesController) onAnchorCreate(colIdx int, flatKey, anchorName string) {
	if colIdx < 0 || colIdx >= state.MaxCustomColumns {
		return
	}

	col := &vc.State.Columns[colIdx]
	if !ensureBlankCustomValues(col) {
		vc.NotifState.Show("Cannot declare anchor: column YAML failed to parse.", state.NotificationError, time.Now())

		return
	}

	// Ensure the cell exists physically in the tree — SetNodeAnchor needs a
	// node to attach .Anchor to, and a cell that's only present as a default
	// or as an unsaved editor override isn't in the tree yet. EnsurePath
	// materializes it using the editor's current text so the user's visible
	// value is what gets anchored.
	val, typ := vc.cellValueAndType(colIdx, flatKey)
	if err := service.EnsurePath(col.CustomValues.NodeTree, flatKey, val, typ); err != nil {
		vc.NotifState.Show("Create anchor: "+err.Error(), state.NotificationError, time.Now())

		return
	}

	if err := service.SetNodeAnchor(col.CustomValues.NodeTree, flatKey, anchorName); err != nil {
		vc.NotifState.Show("Create anchor: "+err.Error(), state.NotificationError, time.Now())

		return
	}

	col.CustomValues.Anchors = service.ExtractAnchors(col.CustomValues.NodeTree)
	col.ValuesModified = true

	if vc.Window != nil {
		vc.Window.Invalidate()
	}
}

// onAnchorAlias converts the cell at flatKey to an alias pointing at an
// existing anchor. Mutates NodeTree in place. Resets the editor text to the
// aliased target's effective value so PatchNodeTree's equality check keeps
// the alias intact on the next save.
func (vc *ValuesController) onAnchorAlias(colIdx int, flatKey, anchorName string) {
	if colIdx < 0 || colIdx >= state.MaxCustomColumns {
		return
	}

	col := &vc.State.Columns[colIdx]
	if !ensureBlankCustomValues(col) {
		vc.NotifState.Show("Cannot create alias: column YAML failed to parse.", state.NotificationError, time.Now())

		return
	}

	// Same reasoning as onAnchorCreate — the cell must physically exist in
	// the tree so SetNodeAlias can find its parent mapping slot to replace.
	val, typ := vc.cellValueAndType(colIdx, flatKey)
	if err := service.EnsurePath(col.CustomValues.NodeTree, flatKey, val, typ); err != nil {
		vc.NotifState.Show("Alias: "+err.Error(), state.NotificationError, time.Now())

		return
	}

	if err := service.SetNodeAlias(col.CustomValues.NodeTree, flatKey, anchorName); err != nil {
		vc.NotifState.Show("Alias: "+err.Error(), state.NotificationError, time.Now())

		return
	}

	col.CustomValues.Anchors = service.ExtractAnchors(col.CustomValues.NodeTree)
	col.ValuesModified = true

	vc.syncEditorToAliasedValue(col, flatKey)

	if vc.Window != nil {
		vc.Window.Invalidate()
	}
}

// onUnlockCell removes whichever anchor or alias annotation lives on the
// cell at flatKey so the user can edit it. Dispatched when the unlock
// confirm dialog is accepted. Anchors are cleared via ClearNodeAnchor;
// aliases are severed via ClearNodeAlias (deep-copy of the target into the
// alias slot). Refreshes Anchors so the badge disappears next frame.
func (vc *ValuesController) onUnlockCell(colIdx int, flatKey string) {
	if colIdx < 0 || colIdx >= state.MaxCustomColumns {
		return
	}

	col := &vc.State.Columns[colIdx]
	if col.CustomValues == nil || col.CustomValues.NodeTree == nil {
		return
	}

	info, ok := col.CustomValues.Anchors[flatKey]
	if !ok || info.Role == service.AnchorRoleNone {
		return
	}

	var err error

	switch info.Role {
	case service.AnchorRoleAnchor:
		err = service.ClearNodeAnchor(col.CustomValues.NodeTree, flatKey)
	case service.AnchorRoleAlias:
		err = service.ClearNodeAlias(col.CustomValues.NodeTree, flatKey)
	case service.AnchorRoleNone:
	}

	if err != nil {
		vc.NotifState.Show("Unlock cell: "+err.Error(), state.NotificationError, time.Now())

		return
	}

	col.CustomValues.Anchors = service.ExtractAnchors(col.CustomValues.NodeTree)
	col.ValuesModified = true

	if vc.Window != nil {
		vc.Window.Invalidate()
	}
}

// onAnchorRename renames an existing anchor on the column's NodeTree,
// updating every alias that referenced it. Refreshes Anchors so both the
// anchor badge and all alias badges re-render with the new name.
func (vc *ValuesController) onAnchorRename(colIdx int, oldName, newName string) {
	if colIdx < 0 || colIdx >= state.MaxCustomColumns {
		return
	}

	col := &vc.State.Columns[colIdx]
	if col.CustomValues == nil || col.CustomValues.NodeTree == nil {
		return
	}

	if err := service.RenameAnchor(col.CustomValues.NodeTree, oldName, newName); err != nil {
		vc.NotifState.Show("Rename anchor: "+err.Error(), state.NotificationError, time.Now())

		return
	}

	col.CustomValues.Anchors = service.ExtractAnchors(col.CustomValues.NodeTree)
	col.ValuesModified = true

	if vc.Window != nil {
		vc.Window.Invalidate()
	}
}

// onAnchorDelete removes an anchor and severs every alias pointing at it.
// Editor texts for those cells remain unchanged (PatchNodeTree's equality
// check keeps them when they match the now-literal tree value).
func (vc *ValuesController) onAnchorDelete(colIdx int, name string) {
	if colIdx < 0 || colIdx >= state.MaxCustomColumns {
		return
	}

	col := &vc.State.Columns[colIdx]
	if col.CustomValues == nil || col.CustomValues.NodeTree == nil {
		return
	}

	if err := service.DeleteAnchor(col.CustomValues.NodeTree, name); err != nil {
		vc.NotifState.Show("Delete anchor: "+err.Error(), state.NotificationError, time.Now())

		return
	}

	col.CustomValues.Anchors = service.ExtractAnchors(col.CustomValues.NodeTree)
	col.ValuesModified = true

	if vc.Window != nil {
		vc.Window.Invalidate()
	}
}
