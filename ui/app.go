package ui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"

	"github.com/qdeck-app/qdeck/config"
	"github.com/qdeck-app/qdeck/domain"
	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui/async"
	"github.com/qdeck-app/qdeck/ui/page"
	"github.com/qdeck-app/qdeck/ui/platform/closeguard"
	"github.com/qdeck-app/qdeck/ui/platform/nativedrop"
	"github.com/qdeck-app/qdeck/ui/state"
	"github.com/qdeck-app/qdeck/ui/theme"
	customwidget "github.com/qdeck-app/qdeck/ui/widget"
)

// pendingAction identifies what the user was trying to do when the
// unsaved-changes confirmation dialog appeared.
type pendingAction uint8

const (
	pendingNone         pendingAction = iota
	pendingNavigateBack               // ESC / back navigation
	pendingBreadcrumb                 // breadcrumb click
	pendingWindowClose                // window close (DestroyEvent)
)

type Application struct {
	window     *app.Window
	theme      *material.Theme
	explorer   *explorer.Explorer
	nativeDrop *nativedrop.Target
	closeGuard *closeguard.CloseGuard

	// Services
	repoService   *service.RepoService
	chartService  *service.ChartService
	recentService *service.RecentService

	// Navigation
	navState state.NavigationState

	// Async runners (repo/chart navigation)
	repoListRunner            *async.Runner[[]domain.HelmRepository]
	chartListRunner           *async.Runner[[]domain.Chart]
	versionListRunner         *async.Runner[[]domain.ChartVersion]
	pullChartRunner           *async.Runner[string]
	localChartRunner          *async.Runner[service.LocalChartResult]
	ociChartRunner            *async.Runner[service.OCIChartResult]
	recentChartsRunner        *async.Runner[[]domain.RecentChart]
	recentValuesEntriesRunner *async.Runner[[]domain.RecentValuesEntry]

	// Notification
	notificationState state.NotificationState
	notificationBar   customwidget.NotificationBar

	// Navigation breadcrumb
	breadcrumb customwidget.Breadcrumb

	// Pages
	reposPage  page.ReposPage
	chartsPage page.ChartsPage
	valuesPage *page.ValuesPage

	// Page states (pre-allocated)
	repoState   state.RepoPageState
	chartState  state.ChartPageState
	valuesState state.ValuesPageState

	// Values controller owns values/render/save concerns.
	valuesCtrl *page.ValuesController

	// preloadCancel cancels any in-flight chart preloading goroutine.
	preloadCancel context.CancelFunc

	// Unsaved-changes confirmation dialog
	unsavedDialog       customwidget.ConfirmDialog
	unsavedDialogYes    widget.Clickable
	unsavedDialogNo     widget.Clickable
	unsavedDialogActive bool
	unsavedPending      pendingAction
	unsavedBreadcrumb   int // breadcrumb segment index when pendingBreadcrumb

	// Custom window decorations (used when the platform cannot provide
	// server-side decorations, e.g. Wayland without xdg-decoration).
	// When enabled, the breadcrumb bar acts as a drag handle and
	// window control buttons appear on its right side.
	customDecor bool
	winButtons  customwidget.WinButtons

	// ops reused across frames
	ops op.Ops
}

func NewApplication(
	w *app.Window,
	repoSvc *service.RepoService,
	chartSvc *service.ChartService,
	valuesSvc *service.ValuesService,
	recentSvc *service.RecentService,
	templateSvc *service.TemplateService,
	customDecor bool,
) *Application {
	th := theme.NewTheme()

	expl := explorer.NewExplorer(w)

	a := &Application{
		window:        w,
		theme:         th,
		explorer:      expl,
		nativeDrop:    nativedrop.New(w),
		closeGuard:    closeguard.New(w),
		repoService:   repoSvc,
		chartService:  chartSvc,
		recentService: recentSvc,
		customDecor:   customDecor,
	}

	a.breadcrumb.MoveArea = customDecor

	// Initialize async runners (repo/chart navigation)
	a.repoListRunner = async.NewRunner[[]domain.HelmRepository](w, 1)
	a.chartListRunner = async.NewRunner[[]domain.Chart](w, 1)
	a.versionListRunner = async.NewRunner[[]domain.ChartVersion](w, 1)
	a.pullChartRunner = async.NewRunner[string](w, 1)
	a.localChartRunner = async.NewRunner[service.LocalChartResult](w, 1)
	a.ociChartRunner = async.NewRunner[service.OCIChartResult](w, 1)
	a.recentChartsRunner = async.NewRunner[[]domain.RecentChart](w, 1)
	a.recentValuesEntriesRunner = async.NewRunner[[]domain.RecentValuesEntry](w, 1)

	a.valuesCtrl = page.NewValuesController(
		w, &a.navState, &a.valuesState, &a.chartState, &a.notificationState,
		expl, valuesSvc, templateSvc, recentSvc, chartSvc,
	)
	a.valuesCtrl.CustomDecor = customDecor
	a.valuesCtrl.OnOpenLocalChart = a.onOpenLocalChart
	a.valuesCtrl.OnPendingValuesFileSelected = a.onValuesFileDropped
	a.valuesCtrl.OnPendingValuesConsumed = a.onPendingValuesConsumed

	// Wire pages
	a.reposPage = page.ReposPage{
		Theme:                     th,
		State:                     &a.repoState,
		OnSelectRepo:              a.onSelectRepo,
		OnAddRepo:                 a.onAddRepo,
		OnRemoveRepo:              a.onRemoveRepo,
		OnRenameRepo:              a.onRenameRepo,
		OnUpdateRepo:              a.onUpdateRepo,
		OnOpenLocalChart:          a.onOpenLocalChart,
		OnSelectRecentChart:       a.onSelectRecentChart,
		OnRemoveRecentChart:       a.onRemoveRecentChart,
		OnOpenChartFilePicker:     a.valuesCtrl.OnOpenChartFilePicker,
		OnDirectLinkSubmit:        a.onDirectLinkSubmit,
		OnValuesFileDropped:       a.onValuesFileDropped,
		OnOpenValuesFilePicker:    a.onOpenValuesFilePicker,
		OnSelectRecentValuesEntry: a.onSelectRecentValuesEntry,
		OnRemoveRecentValuesEntry: a.onRemoveRecentValuesEntry,
	}
	a.chartsPage = page.ChartsPage{
		Theme:           th,
		State:           &a.chartState,
		OnSelectChart:   a.onSelectChart,
		OnSelectVersion: a.onSelectVersion,
		OnSaveChart:     a.valuesCtrl.OnSaveChartVersion,
	}
	a.loadShowComments()

	a.valuesPage = page.NewValuesPage(th, &a.valuesState, a.valuesCtrl.Callbacks())

	a.loadRepos()
	a.loadRecentCharts()
	a.loadRecentValuesEntries()

	return a
}

func (a *Application) Run() error {
	defer a.cancelPreload()
	defer a.nativeDrop.Close()
	defer a.closeGuard.Close()
	defer a.valuesCtrl.Shutdown()

	for {
		e := a.window.Event()
		a.explorer.ListenEvents(e)
		a.nativeDrop.ListenEvents(e)
		a.closeGuard.ListenEvents(e)

		switch e := e.(type) {
		case app.DestroyEvent:
			return e.Err
		case app.ConfigEvent:
			a.handleConfigEvent(e)
		case app.FrameEvent:
			gtx := app.NewContext(&a.ops, e)
			a.pollExternalEvents()
			a.pollAsyncResults()
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (a *Application) handleConfigEvent(e app.ConfigEvent) {
	a.winButtons.Maximized = e.Config.Mode == app.Maximized
}

func (a *Application) pollExternalEvents() {
	a.closeGuard.SetGuarded(
		a.navState.CurrentPage == state.PageValues && a.valuesState.HasUnsavedChanges(),
	)

	// Check if the user tried to close the window while guarded.
	if a.closeGuard.PollCloseAttempt() {
		a.showUnsavedDialog(pendingWindowClose, 0)
	}

	a.repoState.DropSupported = a.nativeDrop.DropSupported
	a.valuesState.DropSupported = a.nativeDrop.DropSupported

	if s, ok := a.nativeDrop.PollDragState(); ok {
		active := s == nativedrop.DragEntered
		a.repoState.FileDropActive = active

		// Native drop targets the first column.
		a.valuesState.Columns[0].FileDropActive = active
	}

	if ev, ok := a.nativeDrop.PollDrop(); ok {
		a.repoState.FileDropActive = false
		a.valuesState.Columns[0].FileDropActive = false

		if len(ev.Paths) > 0 {
			switch a.navState.CurrentPage {
			case state.PageValues:
				a.valuesCtrl.OnColumnFilesSelected(0, ev.Paths)
			case state.PageRepos:
				if int(ev.PositionY) >= a.repoState.ValuesSectionMinY && a.repoState.ValuesSectionMinY > 0 {
					a.onValuesFileDropped(ev.Paths[0])
				} else {
					a.onOpenLocalChart(ev.Paths[0])
				}
			default:
				a.onOpenLocalChart(ev.Paths[0])
			}
		}
	}
}

func pollRunner[T any](runner *async.Runner[T], loading *bool, notif *state.NotificationState, onSuccess func(T)) {
	res, ok := runner.Poll()
	if !ok {
		return
	}

	*loading = false

	if res.Err != nil {
		msg := res.Err.Error()
		if errors.Is(res.Err, context.DeadlineExceeded) {
			msg = "Operation timed out: " + msg
		}

		notif.Show(msg, state.NotificationError, time.Now())
	} else {
		onSuccess(res.Value)
	}
}

func (a *Application) pollAsyncResults() {
	pollRunner(a.repoListRunner, &a.repoState.Loading, &a.notificationState, func(v []domain.HelmRepository) {
		a.repoState.Repos = v
		a.preloadCharts(v)
	})

	pollRunner(a.chartListRunner, &a.chartState.Loading, &a.notificationState, func(v []domain.Chart) {
		a.chartState.Charts = v
		a.chartState.BuildChartSearchCache()
	})

	pollRunner(a.versionListRunner, &a.chartState.Loading, &a.notificationState, func(v []domain.ChartVersion) {
		a.chartState.Versions = v
		a.chartState.BuildVersionSearchCache()
	})

	pollRunner(a.pullChartRunner, &a.valuesState.Loading, &a.notificationState, func(v string) {
		a.valuesState.ChartPath = v
		a.valuesState.ChartName = a.navState.SelectedChart
		a.valuesState.RepoName = a.navState.SelectedRepo
		a.valuesState.OciRef = ""
		a.valuesState.Version = ""
		a.valuesState.RebuildHelmInstallCmd()
		a.valuesCtrl.LoadDefaultValues(v)
	})

	// Local chart loading
	pollRunner(a.localChartRunner, &a.valuesState.Loading, &a.notificationState, a.applyLocalChartResult)

	// OCI chart loading
	pollRunner(a.ociChartRunner, &a.valuesState.Loading, &a.notificationState, a.applyOCIChartResult)

	// Recent charts
	pollRunner(a.recentChartsRunner, &a.repoState.Loading, &a.notificationState, func(v []domain.RecentChart) {
		a.repoState.RecentCharts = v
	})

	pollRunner(a.recentValuesEntriesRunner, &a.repoState.Loading, &a.notificationState, func(v []domain.RecentValuesEntry) {
		a.repoState.RecentValuesEntries = v
	})

	// Values controller polls its own runners.
	a.valuesCtrl.PollAsync()
}

func (a *Application) applyLocalChartResult(res service.LocalChartResult) {
	a.navState.IsLocalChart = true
	a.navState.LocalChartPath = res.ChartPath
	a.navState.LocalChartName = res.Name
	a.navState.SelectedChart = res.Name
	a.navState.SelectedVersion = res.Version
	a.navState.CurrentPage = state.PageValues
	a.valuesState.ChartPath = res.ChartPath
	a.valuesState.ChartName = res.Name
	a.valuesState.RepoName = ""
	a.valuesState.OciRef = ""
	a.valuesState.Version = ""
	a.valuesState.RebuildHelmInstallCmd()
	a.valuesCtrl.LoadDefaultValues(res.ChartPath)
	a.addRecentChart(domain.RecentChart{
		LocalPath:   res.ChartPath,
		DisplayName: res.Name + "@" + res.Version,
	})
}

const (
	breadcrumbRootLabel    = "QDeck"
	breadcrumbLocalLabel   = "Local"
	breadcrumbOCILabel     = "OCI"
	breadcrumbIdxRoot      = 0
	breadcrumbIdxRepo      = 1
	breadcrumbIdxChart     = 2
	breadcrumbIdxVersion   = 3
	breadcrumbCountRoot    = 1
	breadcrumbCountRepo    = 2
	breadcrumbCountChart   = 3
	breadcrumbCountVersion = 4
)

func (a *Application) layout(gtx layout.Context) layout.Dimensions {
	// Fill the window with an opaque white background so that text anti-aliasing
	// composites against a solid backdrop on all platforms (fixes grey text
	// artefacts on Linux where the framebuffer starts transparent).
	paint.Fill(gtx.Ops, theme.Default.White)

	a.handleKeyEvents(gtx)
	a.handleUnsavedDialog(gtx)
	a.valuesCtrl.HandleOverwriteDialog(gtx)

	if clicked := a.breadcrumb.Clicked(gtx); clicked >= 0 {
		a.onBreadcrumbClick(clicked)
	}

	a.updateBreadcrumb()

	windowH := gtx.Constraints.Max.Y

	return layout.Stack{}.Layout(gtx,
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.layoutBreadcrumbRow(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					var idleHint layout.Widget

					switch a.navState.CurrentPage {
					case state.PageValues:
						idleHint = a.valuesPage.LayoutShortcutsHelp
					case state.PageCharts:
						idleHint = a.chartsPage.LayoutShortcutsHelp
					case state.PageRepos:
						idleHint = a.reposPage.LayoutShortcutsHelp
					}

					return a.notificationBar.Layout(gtx, a.theme, &a.notificationState, idleHint)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					a.repoState.PageContentTop = windowH - gtx.Constraints.Max.Y

					switch a.navState.CurrentPage {
					case state.PageCharts:
						return a.chartsPage.Layout(gtx)
					case state.PageValues:
						return a.valuesPage.Layout(gtx)
					default:
						return a.reposPage.Layout(gtx)
					}
				}),
			)
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			if !a.unsavedDialogActive {
				return layout.Dimensions{}
			}

			return a.unsavedDialog.Layout(gtx, a.theme, "You have unsaved changes. Discard and leave?")
		}),
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return a.valuesCtrl.LayoutOverwriteDialog(gtx, a.theme)
		}),
	)
}

// handleUnsavedDialog processes confirm/cancel clicks on the unsaved-changes dialog.
func (a *Application) handleUnsavedDialog(gtx layout.Context) {
	if !a.unsavedDialogActive {
		return
	}

	switch a.unsavedDialog.Update(gtx) {
	case customwidget.ConfirmYes:
		a.unsavedDialogActive = false
		a.executePendingAction()
	case customwidget.ConfirmNo:
		a.unsavedDialogActive = false
		a.unsavedPending = pendingNone
	}
}

func (a *Application) layoutBreadcrumbRow(gtx layout.Context) layout.Dimensions {
	a.handleWinButtonClicks(gtx)

	if !a.customDecor {
		return a.breadcrumb.Layout(gtx, a.theme)
	}

	return a.breadcrumb.LayoutWithAction(gtx, a.theme, func(gtx layout.Context) layout.Dimensions {
		return a.winButtons.Layout(gtx)
	})
}

func (a *Application) handleWinButtonClicks(gtx layout.Context) {
	if !a.customDecor {
		return
	}

	if a.breadcrumb.MoveDoubleClicked(gtx.Source) {
		a.toggleMaximize()
	}

	if a.winButtons.Minimize.Clicked(gtx) {
		a.window.Perform(system.ActionMinimize)
	}

	if a.winButtons.Maximize.Clicked(gtx) {
		a.toggleMaximize()
	}

	if a.winButtons.Close.Clicked(gtx) {
		a.window.Perform(system.ActionClose)
	}
}

func (a *Application) toggleMaximize() {
	if a.winButtons.Maximized {
		a.window.Perform(system.ActionUnmaximize)
	} else {
		a.window.Perform(system.ActionMaximize)
	}
}

func (a *Application) updateBreadcrumb() {
	a.breadcrumb.Segments[breadcrumbIdxRoot].Label = breadcrumbRootLabel

	switch a.navState.CurrentPage {
	case state.PageRepos:
		a.breadcrumb.Count = breadcrumbCountRoot

	case state.PageCharts:
		a.breadcrumb.Segments[breadcrumbIdxRepo].Label = a.navState.SelectedRepo

		if a.chartState.SelectedChart == "" {
			a.breadcrumb.Count = breadcrumbCountRepo
		} else {
			a.breadcrumb.Segments[breadcrumbIdxChart].Label = a.chartState.SelectedChart
			a.breadcrumb.Count = breadcrumbCountChart
		}

	case state.PageValues:
		if a.navState.IsLocalChart {
			label := breadcrumbLocalLabel
			if a.navState.IsOCIChart {
				label = breadcrumbOCILabel
			}

			a.breadcrumb.Segments[breadcrumbIdxRepo].Label = label
			a.breadcrumb.Segments[breadcrumbIdxChart].Label = a.navState.SelectedChart + "@" + a.navState.SelectedVersion
			a.breadcrumb.Count = breadcrumbCountChart
		} else {
			a.breadcrumb.Segments[breadcrumbIdxRepo].Label = a.navState.SelectedRepo
			a.breadcrumb.Segments[breadcrumbIdxChart].Label = a.navState.SelectedChart
			a.breadcrumb.Segments[breadcrumbIdxVersion].Label = a.navState.SelectedVersion
			a.breadcrumb.Count = breadcrumbCountVersion
		}
	}
}

func (a *Application) onBreadcrumbClick(segmentIdx int) {
	if a.guardUnsaved(pendingBreadcrumb, segmentIdx) {
		return
	}

	a.doBreadcrumbClick(segmentIdx)
}

func (a *Application) doBreadcrumbClick(segmentIdx int) {
	switch segmentIdx {
	case breadcrumbIdxRoot:
		a.navState.CurrentPage = state.PageRepos
		a.navState.ClearLocalChart()
		a.navState.PendingValuesPath = ""
	case breadcrumbIdxRepo:
		if a.navState.IsLocalChart {
			a.navState.CurrentPage = state.PageRepos
			a.navState.ClearLocalChart()
			a.navState.PendingValuesPath = ""
		} else {
			a.resetToChartList()
		}
	case breadcrumbIdxChart:
		if !a.navState.IsLocalChart {
			a.navigateToVersions()
		}
	}
}

func (a *Application) handleKeyEvents(gtx layout.Context) {
	area := clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops)
	event.Op(gtx.Ops, a)
	area.Pop()

	for {
		ev, ok := gtx.Event(
			key.Filter{Name: key.NameEscape},
			key.Filter{Name: "F", Required: key.ModShortcut},
			key.Filter{Name: "S", Required: key.ModShortcut},
			key.Filter{Name: key.NameF3},
			key.Filter{Name: key.NameF4},
			key.Filter{Name: "1", Required: key.ModCommand},
			key.Filter{Name: "2", Required: key.ModCommand},
		)
		if !ok {
			break
		}

		e, isKey := ev.(key.Event)
		if !isKey || e.State != key.Press {
			continue
		}

		switch {
		case e.Name == key.NameEscape:
			switch {
			case a.valuesCtrl.IsOverwriteDialogActive():
				a.valuesCtrl.DismissOverwriteDialog()
			case a.unsavedDialogActive:
				a.unsavedDialogActive = false
				a.unsavedPending = pendingNone
			case a.repoState.ConfirmActive:
				a.repoState.ConfirmActive = false
			case a.valuesState.RecentDropdownOpen:
				a.valuesState.RecentDropdownOpen = false
			case a.valuesState.UnlockDialogOpen:
				a.valuesState.UnlockDialogOpen = false
				a.valuesState.PendingUnlockCol = 0
				a.valuesState.PendingUnlockKey = ""
			case a.valuesState.DeleteAnchorDialogOpen:
				a.valuesState.DeleteAnchorDialogOpen = false
				a.valuesState.PendingDeleteAnchorCol = 0
				a.valuesState.PendingDeleteAnchorName = ""
			case a.valuesState.AnchorOp != state.AnchorOpNone:
				a.valuesState.AnchorOp = state.AnchorOpNone
				a.valuesState.AnchorOpCol = 0
				a.valuesState.AnchorOpKey = ""
				a.valuesState.AnchorOpName = ""
			case a.valuesState.AnchorMenuOpen:
				a.valuesState.AnchorMenuOpen = false
			default:
				a.navigateBack()
			}
		case e.Name == "F" && e.Modifiers.Contain(key.ModShortcut):
			a.handleFocusSearch()
		case e.Name == "S" && e.Modifiers.Contain(key.ModShortcut):
			a.handleSaveShortcut()
		case e.Name == key.NameF3 || (e.Name == "1" && e.Modifiers.Contain(key.ModCommand)):
			a.handleRenderDefaults()
		case e.Name == key.NameF4 || (e.Name == "2" && e.Modifiers.Contain(key.ModCommand)):
			a.handleRenderOverrides()
		}
	}
}

func (a *Application) handleFocusSearch() {
	switch a.navState.CurrentPage {
	case state.PageCharts:
		a.chartState.FocusSearch = true
	case state.PageValues:
		a.valuesState.FocusSearch = true
	}
}

func (a *Application) handleSaveShortcut() {
	if a.navState.CurrentPage == state.PageValues {
		a.valuesCtrl.HandleSaveShortcut()
	}
}

func (a *Application) handleRenderDefaults() {
	if a.navState.CurrentPage == state.PageValues {
		a.valuesCtrl.HandleRenderDefaults()
	}
}

func (a *Application) handleRenderOverrides() {
	if a.navState.CurrentPage == state.PageValues {
		a.valuesCtrl.HandleRenderOverrides()
	}
}

func (a *Application) navigateBack() {
	// Close recent dropdown if open — don't navigate.
	if a.valuesState.RecentDropdownOpen {
		a.valuesState.RecentDropdownOpen = false

		return
	}

	if a.guardUnsaved(pendingNavigateBack, 0) {
		return
	}

	a.doNavigateBack()
}

func (a *Application) doNavigateBack() {
	switch a.navState.CurrentPage {
	case state.PageValues:
		a.navState.PendingValuesPath = ""

		if a.navState.IsLocalChart {
			// Local charts have no version page — go straight to repos.
			a.navState.ClearLocalChart()
			a.onBackToRepos()
		} else {
			// Go to version selector (keep SelectedChart, reload versions).
			a.navigateToVersions()
		}
	case state.PageCharts:
		if a.chartState.SelectedChart != "" {
			// Version page → chart list.
			a.resetToChartList()
		} else {
			// Chart list → repos.
			a.onBackToRepos()
		}
	}
}

// guardUnsaved shows the unsaved-changes dialog if the user is on the values
// page with pending edits. Returns true when the guard blocked navigation.
func (a *Application) guardUnsaved(action pendingAction, breadcrumbIdx int) bool {
	if a.navState.CurrentPage == state.PageValues && a.valuesState.HasUnsavedChanges() {
		a.showUnsavedDialog(action, breadcrumbIdx)

		return true
	}

	return false
}

// showUnsavedDialog activates the confirmation dialog for unsaved changes.
func (a *Application) showUnsavedDialog(action pendingAction, breadcrumbIdx int) {
	a.unsavedDialog.YesButton = &a.unsavedDialogYes
	a.unsavedDialog.NoButton = &a.unsavedDialogNo
	a.unsavedDialogActive = true
	a.unsavedPending = action
	a.unsavedBreadcrumb = breadcrumbIdx
}

// executePendingAction runs the action the user confirmed through the unsaved dialog.
func (a *Application) executePendingAction() {
	switch a.unsavedPending {
	case pendingNavigateBack:
		a.doNavigateBack()
	case pendingBreadcrumb:
		a.doBreadcrumbClick(a.unsavedBreadcrumb)
	case pendingWindowClose:
		// Disable the guard so the close goes through, then request close.
		a.closeGuard.SetGuarded(false)
		a.window.Perform(system.ActionClose)
	}
}

// Callbacks

func (a *Application) loadRepos() {
	a.repoState.Loading = true
	a.repoListRunner.RunWithTimeout(config.RepoListOperation, func(ctx context.Context) ([]domain.HelmRepository, error) {
		return a.repoService.ListRepos(ctx)
	})
}

func (a *Application) cancelPreload() {
	if a.preloadCancel != nil {
		a.preloadCancel()
		a.preloadCancel = nil
	}
}

func (a *Application) preloadCharts(repos []domain.HelmRepository) {
	a.cancelPreload()

	ctx, cancel := context.WithTimeout(context.Background(), config.NetworkOperationTimeout)
	a.preloadCancel = cancel

	go func() {
		defer cancel()

		for _, r := range repos {
			if ctx.Err() != nil {
				return
			}

			if _, err := a.chartService.ListCharts(ctx, r.Name); err != nil {
				slog.Warn("chart cache prefetch failed", "repo", r.Name, "error", err)
			}
		}
	}()
}

func (a *Application) loadShowComments() {
	show, err := a.recentService.LoadShowComments(context.Background())
	if err != nil {
		slog.Error("load show comments preference", "error", err)

		return
	}

	a.valuesState.ShowComments.Value = show
}

func (a *Application) loadRecentCharts() {
	a.recentChartsRunner.RunWithTimeout(config.RecentChartsLoadOperation, func(ctx context.Context) ([]domain.RecentChart, error) {
		return a.recentService.ListRecentCharts(ctx)
	})
}

//nolint:dupl // addRecentChart and onRemoveRecentChart call different service methods.
func (a *Application) addRecentChart(entry domain.RecentChart) {
	a.recentChartsRunner.RunWithTimeout(config.RecentChartsLoadOperation, func(ctx context.Context) ([]domain.RecentChart, error) {
		if err := a.recentService.AddRecentChart(ctx, entry); err != nil {
			return nil, fmt.Errorf("add recent chart: %w", err)
		}

		return a.recentService.ListRecentCharts(ctx)
	})
}

func parseDirectLink(input string) (repo, chart, version string, ok bool) {
	input = strings.TrimSpace(input)

	repo, rest, found := strings.Cut(input, "/")
	if !found || repo == "" {
		return "", "", "", false
	}

	chart, version, found = strings.Cut(rest, ":")
	if !found || chart == "" || version == "" {
		return "", "", "", false
	}

	return repo, chart, version, true
}

const ociPrefix = "oci://"

func isOCIRef(input string) bool {
	return strings.HasPrefix(input, ociPrefix)
}

// parseOCIRef splits an OCI reference like "oci://registry.example.com/charts/nginx:1.2.0"
// into the base ref ("oci://registry.example.com/charts/nginx") and version ("1.2.0").
// Uses the last colon to correctly handle registry ports (e.g. "oci://host:5000/chart:1.0").
func parseOCIRef(input string) (ociRef, version string, ok bool) {
	input = strings.TrimSpace(input)

	lastColon := strings.LastIndex(input, ":")
	if lastColon < 0 {
		return "", "", false
	}

	ref := input[:lastColon]
	ver := input[lastColon+1:]

	if ref == "" || ver == "" || !strings.HasPrefix(ref, ociPrefix) {
		return "", "", false
	}

	return ref, ver, true
}

func (a *Application) onDirectLinkSubmit(text string) {
	text = strings.TrimSpace(text)

	if isOCIRef(text) {
		a.onOpenOCIChart(text)

		return
	}

	repo, chart, version, ok := parseDirectLink(text)
	if !ok {
		a.notificationState.Show(
			"Invalid format. Use repo/chart:version or oci://registry/chart:version",
			state.NotificationError,
			time.Now(),
		)

		return
	}

	repoFound := false

	for _, r := range a.repoState.Repos {
		if r.Name == repo {
			repoFound = true

			break
		}
	}

	if !repoFound {
		a.notificationState.Show(
			"Repository \""+repo+"\" is not configured. Add it first.",
			state.NotificationError,
			time.Now(),
		)

		return
	}

	a.navState.SelectedRepo = repo
	a.onSelectVersion(chart, version)
}

func (a *Application) onOpenOCIChart(text string) {
	ociRef, version, ok := parseOCIRef(text)
	if !ok {
		a.notificationState.Show(
			"Invalid OCI format. Use oci://registry/chart:version",
			state.NotificationError,
			time.Now(),
		)

		return
	}

	chartName := filepath.Base(ociRef)
	a.navState.SelectedChart = chartName
	a.navState.SelectedVersion = version
	a.navState.SelectedRepo = ""
	a.navState.CurrentPage = state.PageValues
	a.navState.ClearLocalChart()
	a.navState.IsLocalChart = true
	a.navState.IsOCIChart = true
	a.valuesCtrl.ResetState()
	a.ociChartRunner.RunWithTimeout(config.ChartPullOperation, func(ctx context.Context) (service.OCIChartResult, error) {
		return a.chartService.PullOCIChart(ctx, ociRef, version)
	})
	a.valuesCtrl.LoadRecentValues()
}

func (a *Application) applyOCIChartResult(res service.OCIChartResult) {
	a.navState.IsLocalChart = true
	a.navState.IsOCIChart = true
	a.navState.LocalChartPath = res.ChartPath
	a.navState.LocalChartName = res.Name
	a.navState.SelectedChart = res.Name
	a.navState.SelectedVersion = res.Version
	a.navState.CurrentPage = state.PageValues
	a.valuesState.ChartPath = res.ChartPath
	a.valuesState.ChartName = res.Name
	a.valuesState.RepoName = ""
	a.valuesState.OciRef = res.OciRef
	a.valuesState.Version = res.Version
	a.valuesState.RebuildHelmInstallCmd()
	a.valuesCtrl.LoadDefaultValues(res.ChartPath)
	a.addRecentChart(domain.RecentChart{
		OciURL:      res.OciRef,
		ChartName:   res.Name,
		Version:     res.Version,
		DisplayName: res.Name + "@" + res.Version + " (OCI)",
	})
}

func (a *Application) onSelectRepo(name string) {
	a.navState.SelectedRepo = name
	a.navState.CurrentPage = state.PageCharts
	a.navState.ClearLocalChart()
	a.chartState.Loading = true
	a.chartState.SelectedChart = ""
	a.chartState.Versions = nil
	a.chartState.FocusSearch = true
	a.chartListRunner.RunWithTimeout(config.ChartListOperation, func(ctx context.Context) ([]domain.Chart, error) {
		return a.chartService.ListCharts(ctx, name)
	})
}

func (a *Application) runRepoAction(action func(ctx context.Context) error) {
	a.repoState.Loading = true
	a.repoListRunner.RunWithTimeout(config.RepoListOperation, func(ctx context.Context) ([]domain.HelmRepository, error) {
		if err := action(ctx); err != nil {
			return nil, fmt.Errorf("repo action: %w", err)
		}

		return a.repoService.ListRepos(ctx)
	})
}

func (a *Application) onAddRepo(req service.AddRepoRequest) {
	a.runRepoAction(func(ctx context.Context) error { return a.repoService.AddRepo(ctx, req) })
}

func (a *Application) onRemoveRepo(name string) {
	a.chartService.InvalidateChartCache(name)
	a.runRepoAction(func(ctx context.Context) error { return a.repoService.RemoveRepo(ctx, name) })
}

func (a *Application) onRenameRepo(oldName, newName string) {
	a.chartService.InvalidateChartCache(oldName)
	a.runRepoAction(func(ctx context.Context) error {
		return a.repoService.RenameRepo(ctx, service.RenameRepoRequest{
			OldName: oldName, NewName: newName,
		})
	})
}

func (a *Application) onUpdateRepo(name string) {
	a.chartService.InvalidateChartCache(name)
	a.runRepoAction(func(ctx context.Context) error { return a.repoService.UpdateRepo(ctx, name) })
}

func (a *Application) onSelectChart(chartName string) {
	a.chartState.FocusedIndex = 0
	a.chartState.SearchEditor.SetText("")
	a.chartState.Loading = true
	a.versionListRunner.RunWithTimeout(config.ChartVersionListOperation, func(ctx context.Context) ([]domain.ChartVersion, error) {
		return a.chartService.ListVersions(ctx, a.navState.SelectedRepo, chartName)
	})
}

func (a *Application) onSelectVersion(chartName, version string) {
	a.navState.SelectedChart = chartName
	a.navState.SelectedVersion = version
	a.navState.CurrentPage = state.PageValues
	a.navState.ClearLocalChart()
	a.valuesCtrl.ResetState()
	a.pullChartRunner.RunWithTimeout(config.ChartPullOperation, func(ctx context.Context) (string, error) {
		return a.chartService.PullChart(ctx, a.navState.SelectedRepo, chartName, version)
	})
	a.addRecentChart(domain.RecentChart{
		RepoName:    a.navState.SelectedRepo,
		ChartName:   chartName,
		Version:     version,
		DisplayName: a.navState.SelectedRepo + "/" + chartName + "@" + version,
	})
	a.valuesCtrl.LoadRecentValues()
}

func (a *Application) onOpenLocalChart(path string) {
	a.valuesCtrl.ResetState()
	a.localChartRunner.RunWithTimeout(config.ChartLoadOperation, func(ctx context.Context) (service.LocalChartResult, error) {
		return a.chartService.LoadLocalChart(ctx, path)
	})
	a.valuesCtrl.LoadRecentValues()
}

func (a *Application) onSelectRecentChart(entry domain.RecentChart) {
	a.openChartByRef(entry)
}

// openChartByRef routes to the correct chart handler based on chart source type.
func (a *Application) openChartByRef(ref domain.RecentChart) {
	switch {
	case ref.IsLocal():
		a.onOpenLocalChart(ref.LocalPath)
	case ref.IsOCI():
		a.onOpenOCIChart(ref.OciURL + ":" + ref.Version)
	default:
		a.navState.SelectedRepo = ref.RepoName
		a.onSelectVersion(ref.ChartName, ref.Version)
	}
}

//nolint:dupl // addRecentChart and onRemoveRecentChart call different service methods.
func (a *Application) onRemoveRecentChart(idx int) {
	a.recentChartsRunner.RunWithTimeout(config.RecentChartsLoadOperation, func(ctx context.Context) ([]domain.RecentChart, error) {
		if err := a.recentService.RemoveRecentChart(ctx, idx); err != nil {
			return nil, fmt.Errorf("remove recent chart: %w", err)
		}

		return a.recentService.ListRecentCharts(ctx)
	})
}

func (a *Application) onValuesFileDropped(path string) {
	a.navState.PendingValuesPath = path
	a.notificationState.Show(
		"Values file queued: "+filepath.Base(path)+". Select a chart to continue.",
		state.NotificationSuccess,
		time.Now(),
	)
}

func (a *Application) onOpenValuesFilePicker() {
	a.valuesCtrl.OpenValuesFilePicker()
}

func (a *Application) onSelectRecentValuesEntry(entry domain.RecentValuesEntry) {
	a.navState.PendingValuesPath = entry.ValuesPath
	a.openChartByRef(entry.ChartRef())
}

//nolint:dupl // Structurally similar to onRemoveRecentChart but operates on different type.
func (a *Application) onRemoveRecentValuesEntry(idx int) {
	a.recentValuesEntriesRunner.RunWithTimeout(config.RecentValuesEntriesLoadOperation, func(ctx context.Context) ([]domain.RecentValuesEntry, error) {
		if err := a.recentService.RemoveRecentValuesEntry(ctx, idx); err != nil {
			return nil, fmt.Errorf("remove recent values entry: %w", err)
		}

		return a.recentService.ListRecentValuesEntries(ctx)
	})
}

func (a *Application) onPendingValuesConsumed(valuesPath string) {
	entry := domain.RecentValuesEntry{
		ValuesPath: valuesPath,
	}

	switch {
	case a.navState.IsLocalChart:
		entry.LocalPath = a.navState.LocalChartPath
		entry.ChartDisplayName = a.navState.LocalChartName
	case a.navState.IsOCIChart:
		entry.OciURL = a.navState.LocalChartPath // OCI charts store the registry URL in LocalChartPath.
		entry.ChartDisplayName = a.navState.LocalChartName
		entry.Version = a.navState.SelectedVersion
	default:
		entry.RepoName = a.navState.SelectedRepo
		entry.ChartName = a.navState.SelectedChart
		entry.Version = a.navState.SelectedVersion
		entry.ChartDisplayName = a.navState.SelectedChart + ":" + a.navState.SelectedVersion
	}

	a.addRecentValuesEntry(entry)
}

func (a *Application) loadRecentValuesEntries() {
	a.recentValuesEntriesRunner.RunWithTimeout(config.RecentValuesEntriesLoadOperation, func(ctx context.Context) ([]domain.RecentValuesEntry, error) {
		return a.recentService.ListRecentValuesEntries(ctx)
	})
}

//nolint:dupl // Structurally similar to addRecentChart but operates on different type.
func (a *Application) addRecentValuesEntry(entry domain.RecentValuesEntry) {
	a.recentValuesEntriesRunner.RunWithTimeout(config.RecentValuesEntriesLoadOperation, func(ctx context.Context) ([]domain.RecentValuesEntry, error) {
		if err := a.recentService.AddRecentValuesEntry(ctx, entry); err != nil {
			return nil, fmt.Errorf("add recent values entry: %w", err)
		}

		return a.recentService.ListRecentValuesEntries(ctx)
	})
}

func (a *Application) onBackToRepos() {
	a.navState.CurrentPage = state.PageRepos
	a.navState.PendingValuesPath = ""
	a.loadRepos()
	a.loadRecentValuesEntries()
}

func (a *Application) navigateToVersions() {
	a.navState.CurrentPage = state.PageCharts
	a.chartState.SelectedChart = a.navState.SelectedChart
	a.chartState.FocusedIndex = 0
	a.chartState.Loading = true
	a.versionListRunner.RunWithTimeout(config.ChartVersionListOperation, func(ctx context.Context) ([]domain.ChartVersion, error) {
		return a.chartService.ListVersions(ctx, a.navState.SelectedRepo, a.navState.SelectedChart)
	})
}

// resetToChartList navigates to the chart list, clearing selection and search state.
func (a *Application) resetToChartList() {
	a.navState.CurrentPage = state.PageCharts
	a.chartState.SelectedChart = ""
	a.chartState.Versions = nil
	a.chartState.FocusedIndex = 0
	a.chartState.SearchEditor.SetText("")
	a.chartState.Loading = true
	a.chartState.FocusSearch = true

	a.chartListRunner.RunWithTimeout(config.ChartListOperation, func(ctx context.Context) ([]domain.Chart, error) {
		return a.chartService.ListCharts(ctx, a.navState.SelectedRepo)
	})
}
