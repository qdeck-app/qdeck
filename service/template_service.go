package service

import (
	"context"
	"fmt"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
)

const templateReleaseName = "preview"

// TemplateService renders Helm chart templates.
type TemplateService struct{}

func NewTemplateService() *TemplateService {
	return &TemplateService{}
}

// RenderTemplate loads a chart from chartPath, merges the given vals,
// and returns the rendered YAML manifest string.
// Uses action.Install with DryRun + ClientOnly so no cluster is needed.
func (s *TemplateService) RenderTemplate(
	ctx context.Context,
	chartPath string,
	vals map[string]any,
) (string, error) {
	if ctx.Err() != nil {
		return "", fmt.Errorf("render template: %w", ctx.Err())
	}

	ch, err := loader.Load(chartPath)
	if err != nil {
		return "", fmt.Errorf("load chart for template render %s: %w", chartPath, err)
	}

	cfg := &action.Configuration{}
	client := action.NewInstall(cfg)
	client.DryRun = true
	client.ReleaseName = templateReleaseName
	client.Replace = true
	client.ClientOnly = true

	if vals == nil {
		vals = make(map[string]any)
	}

	rel, err := client.Run(ch, vals)
	if err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}

	if rel == nil {
		return "", nil
	}

	return rel.Manifest, nil
}
