package main

import (
	"log"
	"log/slog"
	"os"

	"gioui.org/app"
	"gioui.org/unit"
	"helm.sh/helm/v3/pkg/cli"

	"github.com/qdeck-app/qdeck/infra/config"
	"github.com/qdeck-app/qdeck/infrastructure/storage"
	"github.com/qdeck-app/qdeck/service"
	"github.com/qdeck-app/qdeck/ui"
)

const (
	defaultWindowWidth  = 1200
	defaultWindowHeight = 800
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
		recentSvc := service.NewRecentService(jsonStore)
		templateSvc := service.NewTemplateService()

		w := new(app.Window)
		w.Option(app.Title("QDeck - Helm Values Editor"))
		w.Option(app.Size(unit.Dp(defaultWindowWidth), unit.Dp(defaultWindowHeight)))

		application := ui.NewApplication(w, repoSvc, chartSvc, valuesSvc, recentSvc, templateSvc)
		if err := application.Run(); err != nil {
			log.Fatal(err)
		}

		os.Exit(0)
	}()

	app.Main()
}
