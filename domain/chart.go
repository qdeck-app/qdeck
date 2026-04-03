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
