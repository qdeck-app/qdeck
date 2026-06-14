package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"runtime"

	"gioui.org/app"
	"gioui.org/unit"
	"helm.sh/helm/v3/pkg/cli"

	"github.com/qdeck-app/qdeck/infrastructure/config"
	"github.com/qdeck-app/qdeck/infrastructure/storage"
	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui"
	"github.com/qdeck-app/qdeck/ui/platform/screen"
)

const (
	defaultWindowWidth  = 1200
	defaultWindowHeight = 800

	// preferredWindowWidth is used on a first launch
	preferredWindowWidth = 1600
	// windowScreenMargin is the gap left between the default window and the work-area edges
	windowScreenMargin = 80
)

func main() {
	slog.Info("starting QDeck", "version", config.AppVersion)

	go func() {
		settings := cli.New()

		jsonStore, err := storage.NewJSONStore()
		if err != nil {
			log.Fatal(err)
		}

		repoSvc := service.NewRepoService(settings)
		chartSvc := service.NewChartService(settings)
		valuesSvc := service.NewValuesService()
		appState := service.NewAppStateService(jsonStore)
		templateSvc := service.NewTemplateService()

		w := new(app.Window)
		w.Option(app.Title("QDeck - Helm Values Editor"))
		w.Option(windowGeometryOptions(appState)...)

		// On Linux and Windows, disable compositor decorations and draw our own
		// window control buttons in the breadcrumb bar.
		customDecor := runtime.GOOS == "linux" || runtime.GOOS == "windows"
		if customDecor {
			w.Option(app.Decorated(false))
		}

		application := ui.NewApplication(w, repoSvc, chartSvc, valuesSvc, appState, templateSvc, customDecor)
		if err := application.Run(); err != nil {
			log.Fatal(err)
		}

		os.Exit(0)
	}()

	app.Main()
}

// windowGeometryOptions restores the last persisted window size and maximized
// state, falling back to the default size when nothing has been saved yet (or
// the saved record is unreadable). Size is stored in Dp, so app.Size rescales
// it correctly for the current monitor's DPI.
func windowGeometryOptions(appState *service.AppStateService) []app.Option {
	geom, err := appState.LoadWindowGeometry(context.Background())
	if err != nil || geom == nil || geom.WidthDp <= 0 || geom.HeightDp <= 0 {
		w, h := defaultWindowSize()

		return []app.Option{app.Size(w, h)}
	}

	opts := []app.Option{app.Size(unit.Dp(geom.WidthDp), unit.Dp(geom.HeightDp))}
	if geom.Maximized {
		opts = append(opts, app.Maximized.Option())
	}

	return opts
}

// defaultWindowSize is the size used on a first launch (no saved geometry).
func defaultWindowSize() (unit.Dp, unit.Dp) {
	areaW, areaH, ok := screen.WorkAreaDp()

	return fitWindowSize(areaW, areaH, ok)
}

// fitWindowSize picks the first-launch window size from the primary work area
// (in Dp). Width grows toward preferredWindowWidth when the display allows it
// and shrinks below the base default on a narrow screen, so the window never
// opens wider or taller than the work area (less a margin). When the work area
// is unknown (ok == false), the fixed base default is used unchanged.
func fitWindowSize(areaW, areaH int, ok bool) (unit.Dp, unit.Dp) {
	w := unit.Dp(defaultWindowWidth)
	h := unit.Dp(defaultWindowHeight)

	if !ok {
		return w, h
	}

	if fit := unit.Dp(areaW) - windowScreenMargin; fit > 0 {
		w = min(unit.Dp(preferredWindowWidth), fit)
	}

	if fit := unit.Dp(areaH) - windowScreenMargin; fit > 0 {
		h = min(unit.Dp(defaultWindowHeight), fit)
	}

	return w, h
}
