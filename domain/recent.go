package domain

import (
	"errors"
	"time"
)

// RecentChart represents a recently viewed chart (either from a repo or local disk).
type RecentChart struct {
	// For repo-sourced charts
	RepoName  string `json:"repoName,omitempty"`
	ChartName string `json:"chartName,omitempty"`
	Version   string `json:"version,omitempty"`

	// For OCI registry charts
	OciURL string `json:"ociUrl,omitempty"`

	// For disk-sourced charts
	LocalPath string `json:"localPath,omitempty"`

	// Display metadata
	DisplayName string    `json:"displayName"`
	OpenedAt    time.Time `json:"openedAt"`
}

// IsLocal returns true if this chart was opened from disk.
func (r RecentChart) IsLocal() bool {
	return r.LocalPath != ""
}

// IsOCI returns true if this chart was opened from an OCI registry.
func (r RecentChart) IsOCI() bool {
	return r.OciURL != ""
}

// IsValid validates that the RecentChart has all required fields.
func (r RecentChart) IsValid() error {
	if r.DisplayName == "" {
		return errors.New("displayName required")
	}

	switch {
	case r.IsOCI():
		if r.Version == "" {
			return errors.New("version required for OCI charts")
		}
	case !r.IsLocal():
		if r.RepoName == "" || r.ChartName == "" {
			return errors.New("repoName and chartName required for remote charts")
		}
	}

	return nil
}

// RecentValuesFile represents a recently opened custom values file.
type RecentValuesFile struct {
	Path     string    `json:"path"`
	OpenedAt time.Time `json:"openedAt"`
}
