package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/repo"

	"github.com/qdeck-app/qdeck/domain"
)

// NOTE: Services check ctx.Err() early but do not pass context to underlying Helm library calls,
// as the Helm v3 API does not support context parameters. Timeout enforcement happens
// at the async.Runner layer (ui/async/runner.go) with operation-type-specific timeouts.

const pullDirPerm = 0o750

// LocalChartResult holds metadata about a chart loaded from disk.
type LocalChartResult struct {
	Name      string
	Version   string
	ChartPath string
}

type ChartService struct {
	settings   *cli.EnvSettings
	mu         sync.RWMutex
	chartCache map[string][]domain.Chart
}

func NewChartService(settings *cli.EnvSettings) *ChartService {
	return &ChartService{
		settings:   settings,
		chartCache: make(map[string][]domain.Chart),
	}
}

func (s *ChartService) ListCharts(ctx context.Context, repoName string) ([]domain.Chart, error) {
	s.mu.RLock()
	cached, ok := s.chartCache[repoName]
	s.mu.RUnlock()

	if ok {
		return cached, nil
	}

	if ctx.Err() != nil {
		return nil, fmt.Errorf("list charts: %w", ctx.Err())
	}

	idx, err := s.loadIndex(repoName)
	if err != nil {
		return nil, fmt.Errorf("list charts for repo %s: %w", repoName, err)
	}

	charts := make([]domain.Chart, 0, len(idx.Entries))
	for name, versions := range idx.Entries {
		c := domain.Chart{
			Name:     name,
			RepoName: repoName,
		}
		if len(versions) > 0 {
			c.Description = versions[0].Description
		}

		vlist := make([]domain.ChartVersion, len(versions))
		for i, v := range versions {
			vlist[i] = domain.ChartVersion{
				Version:    v.Version,
				AppVersion: v.AppVersion,
				Created:    v.Created,
				Digest:     v.Digest,
				URLs:       v.URLs,
			}
		}

		c.Versions = vlist
		charts = append(charts, c)
	}

	slices.SortFunc(charts, func(a, b domain.Chart) int {
		return strings.Compare(a.Name, b.Name)
	})

	s.mu.Lock()
	s.chartCache[repoName] = charts
	s.mu.Unlock()

	return charts, nil
}

// InvalidateChartCache removes the cached chart list for a repository.
func (s *ChartService) InvalidateChartCache(repoName string) {
	s.mu.Lock()
	delete(s.chartCache, repoName)
	s.mu.Unlock()
}

func (s *ChartService) ListVersions(ctx context.Context, repoName, chartName string) ([]domain.ChartVersion, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("list versions: %w", ctx.Err())
	}

	idx, err := s.loadIndex(repoName)
	if err != nil {
		return nil, fmt.Errorf("list versions for %s/%s: %w", repoName, chartName, err)
	}

	versions, ok := idx.Entries[chartName]
	if !ok {
		return nil, fmt.Errorf("chart %q not found in repo %q", chartName, repoName)
	}

	result := make([]domain.ChartVersion, len(versions))
	for i, v := range versions {
		result[i] = domain.ChartVersion{
			Version:    v.Version,
			AppVersion: v.AppVersion,
			Created:    v.Created,
			Digest:     v.Digest,
			URLs:       v.URLs,
		}
	}

	return result, nil
}

// chartVersionMatches checks if a cached chart's version matches the requested version
// by reading only Chart.yaml, avoiding expensive full chart loading.
func chartVersionMatches(chartDir, expectedVersion string) bool {
	chartYamlPath := filepath.Join(chartDir, "Chart.yaml")

	ch, err := chartutil.LoadChartfile(chartYamlPath)
	if err != nil {
		return false
	}

	return ch.Version == expectedVersion
}

func (s *ChartService) PullChart(ctx context.Context, repoName, chartName, version string) (string, error) {
	if ctx.Err() != nil {
		return "", fmt.Errorf("pull chart: %w", ctx.Err())
	}

	f, err := repo.LoadFile(s.settings.RepositoryConfig)
	if err != nil {
		return "", fmt.Errorf("pull chart %s/%s@%s: load repo file: %w", repoName, chartName, version, err)
	}

	entry := f.Get(repoName)
	if entry == nil {
		return "", fmt.Errorf("repo %q not found", repoName)
	}

	destDir := filepath.Join(s.settings.RepositoryCache, "pulled")
	if err := os.MkdirAll(destDir, pullDirPerm); err != nil {
		return "", fmt.Errorf("create pull dir: %w", err)
	}

	chartDir := filepath.Join(destDir, chartName)

	if chartVersionMatches(chartDir, version) {
		return chartDir, nil
	}

	if err := os.RemoveAll(chartDir); err != nil {
		return "", fmt.Errorf("clean previous pull: %w", err)
	}

	pull := action.NewPullWithOpts(action.WithConfig(&action.Configuration{}))
	pull.Settings = s.settings
	pull.RepoURL = entry.URL
	pull.Username = entry.Username
	pull.Password = entry.Password
	pull.Version = version
	pull.Untar = true
	pull.UntarDir = destDir
	pull.DestDir = destDir

	output, err := pull.Run(chartName)
	if err != nil {
		return "", fmt.Errorf("pull chart %s/%s@%s: %w (output: %s)", repoName, chartName, version, err, output)
	}

	return chartDir, nil
}

// LoadLocalChart loads a chart from a local directory or .tgz file.
func (s *ChartService) LoadLocalChart(ctx context.Context, path string) (LocalChartResult, error) {
	if ctx.Err() != nil {
		return LocalChartResult{}, fmt.Errorf("load local chart: %w", ctx.Err())
	}

	// Resolve Chart.yaml / Chart.yml to parent directory.
	base := filepath.Base(path)
	if strings.EqualFold(base, "Chart.yaml") || strings.EqualFold(base, "Chart.yml") {
		path = filepath.Dir(path)
	} else if strings.HasSuffix(strings.ToLower(base), ".yaml") || strings.HasSuffix(strings.ToLower(base), ".yml") {
		return LocalChartResult{}, fmt.Errorf("load local chart: expected Chart.yaml or Chart.yml, got %s", base)
	}

	ch, err := loader.Load(path)
	if err != nil {
		return LocalChartResult{}, fmt.Errorf("load local chart from %s: %w", path, err)
	}

	if ch.Metadata == nil {
		return LocalChartResult{}, fmt.Errorf("load local chart from %s: chart has no metadata", path)
	}

	name := ch.Metadata.Name
	version := ch.Metadata.Version

	return LocalChartResult{
		Name:      name,
		Version:   version,
		ChartPath: path,
	}, nil
}

// SaveChartAsTarGz packages a chart directory into a .tgz file.
func (s *ChartService) SaveChartAsTarGz(ctx context.Context, chartPath string) (string, error) {
	if ctx.Err() != nil {
		return "", fmt.Errorf("save chart as tar.gz: %w", ctx.Err())
	}

	ch, err := loader.Load(chartPath)
	if err != nil {
		return "", fmt.Errorf("load chart for export %s: %w", chartPath, err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}

	destDir := filepath.Join(homeDir, "Downloads")
	if err := os.MkdirAll(destDir, pullDirPerm); err != nil {
		return "", fmt.Errorf("create downloads dir: %w", err)
	}

	outPath, err := chartutil.Save(ch, destDir)
	if err != nil {
		return "", fmt.Errorf("save chart as tar.gz: %w", err)
	}

	return outPath, nil
}

func (s *ChartService) loadIndex(repoName string) (*repo.IndexFile, error) {
	cachePath := filepath.Join(s.settings.RepositoryCache, repoName+"-index.yaml")

	idx, err := repo.LoadIndexFile(cachePath)
	if err != nil {
		return nil, fmt.Errorf("load index for %s: %w", repoName, err)
	}

	return idx, nil
}
