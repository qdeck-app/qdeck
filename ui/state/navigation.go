package state

// Page identifies the current top-level view.
type Page uint8

const (
	PageRepos  Page = iota // Repository management
	PageCharts             // Chart browser for selected repo
	PageValues             // Values viewer for selected chart
)

// NavigationState tracks which page is active and breadcrumb context.
type NavigationState struct {
	CurrentPage     Page
	SelectedRepo    string
	SelectedChart   string
	SelectedVersion string

	// Local/OCI chart support (skip charts page)
	IsLocalChart   bool
	IsOCIChart     bool
	LocalChartPath string
	LocalChartName string

	// PendingValuesPath holds a values file dropped on the repos page.
	// It is auto-loaded into column 0 when the user navigates to the values page.
	PendingValuesPath string
}

// ClearLocalChart resets all local and OCI chart navigation fields.
func (n *NavigationState) ClearLocalChart() {
	n.IsLocalChart = false
	n.IsOCIChart = false
	n.LocalChartPath = ""
	n.LocalChartName = ""
}
