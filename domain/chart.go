package domain

import "time"

// Chart represents a Helm chart available in a repository.
type Chart struct {
	Name        string
	Description string
	RepoName    string
	Versions    []ChartVersion
}

// LatestVersion returns the version string of the first (newest) version,
// or empty string if none exist.
func (c *Chart) LatestVersion() string {
	if len(c.Versions) > 0 {
		return c.Versions[0].Version
	}

	return ""
}

// ChartVersion represents a specific version of a chart.
type ChartVersion struct {
	Version    string
	AppVersion string
	Created    time.Time
	Digest     string
	URLs       []string
}

// ChartKey builds a stable identifier for a chart from its identifying fields.
// It is the persistence key for per-chart state (see AppData.ChartUIStates),
// so the exact string must be reproducible from whatever data the caller has:
// the UI state layer, a RecentChart entry, etc.
//
// Discrimination (first matching case wins):
//   - remote (Helm repo): repoName AND chartName set → "repo/chart@version"
//   - OCI registry:       otherwise, ociURL set      → "ociURL@version"
//   - on-disk:            otherwise                  → localPath
//
// Returns "" when none of the identifying fields are populated (i.e. no
// chart is currently selected); callers use that to skip persistence.
func ChartKey(repoName, chartName, ociURL, localPath, version string) string {
	switch {
	case repoName != "" && chartName != "":
		return repoName + "/" + chartName + "@" + version
	case ociURL != "":
		return ociURL + "@" + version
	default:
		return localPath
	}
}

// ChartUIState captures per-chart UI state that should survive app restarts,
// keyed by a stable chart identifier (see ValuesPageState.ChartKey).
//
// FocusedKey is the entry's flat key (e.g. "image.repository"), not a row
// index: indices depend on the current search filter and would drift between
// sessions. An empty FocusedKey means "no saved focus"; the column alone
// should not be restored in that case.
//
// LastTouchedAt records the last SaveChartUIState time; SaveChartUIState
// evicts the oldest entries when the state map exceeds its cap.
type ChartUIState struct {
	FocusedKey    string    `json:"focusedKey,omitempty"`
	FocusedCol    int       `json:"focusedCol,omitempty"`
	LastTouchedAt time.Time `json:"lastTouchedAt,omitempty"`
}
