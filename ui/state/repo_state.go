package state

import (
	"gioui.org/widget"

	"github.com/qdeck-app/qdeck/domain"
)

// RepoSection identifies a keyboard-focusable section on the repos page.
type RepoSection uint8

const (
	RepoSectionRecent RepoSection = iota
	RepoSectionRepos
	RepoSectionValues
	RepoSectionAddForm
	RepoSectionCount // sentinel value for modular cycling
)

// RepoPageState holds all widget state for the repos page.
// All widget structs are pre-allocated; none are created during layout.
type RepoPageState struct {
	// Data
	Repos   []domain.HelmRepository
	Loading bool

	// Keyboard navigation
	FocusedSection RepoSection
	FocusedIndex   int

	// Page-level scroll
	PageList widget.List

	// SectionHeights[i] is the measured pixel height of the i-th top-level
	// page section (Charts=0, Repositories=1, Values=2). Populated during
	// layout; used to compute the Values section's window-y for native drop
	// routing when the page is scrolled.
	SectionHeights [3]int

	// Widget state: repo list
	RepoList   widget.List
	RepoClicks []widget.Clickable

	// Widget state: add repo form
	AddNameEditor  widget.Editor
	AddURLEditor   widget.Editor
	AddButton      widget.Clickable
	SubmitButton   widget.Clickable
	AddFormVisible bool
	PresetClicks   []widget.Clickable

	// Widget state: context actions
	DeleteClicks []widget.Clickable
	UpdateClicks []widget.Clickable

	// Rename
	RenameEditor widget.Editor
	RenameActive bool
	RenameOK     widget.Clickable
	RenameCancel widget.Clickable

	// Confirm delete
	ConfirmDeleteName string
	ConfirmActive     bool
	ConfirmYes        widget.Clickable
	ConfirmNo         widget.Clickable

	// Back button (for future nav)
	BackButton widget.Clickable

	// Recent charts
	RecentCharts       []domain.RecentChart
	RecentList         widget.List
	RecentClicks       []widget.Clickable
	RecentRemoveClicks []widget.Clickable

	// Recent values+chart paired entries
	RecentValuesEntries      []domain.RecentValuesEntry
	RecentValuesList         widget.List
	RecentValuesClicks       []widget.Clickable
	RecentValuesRemoveClicks []widget.Clickable

	// Direct link input (repo/chart:version)
	DirectLinkEditor widget.Editor

	// Chart file picker button (inside compact drop zone)
	ChartPickerButton widget.Clickable

	// Values file picker button (inside values drop zone)
	ValuesPickerButton widget.Clickable

	// FileDropActive is true when files are being dragged over the window.
	FileDropActive bool

	// DropSupported is true when the platform supports drag-and-drop.
	DropSupported bool

	// PageContentTop is the Y offset (in window pixels) of the page content area.
	// Set by the Application layout before the page is rendered.
	PageContentTop int

	// ValuesSectionMinY is the Y offset (in window pixels) where the Values section starts.
	// Used by the native drop handler to route drops by area.
	ValuesSectionMinY int
}

// EnsureClickables grows clickable slices to match repo count. Only allocates
// when the repo list grows beyond current capacity.
func (s *RepoPageState) EnsureClickables(count int) {
	growClickables(count, &s.RepoClicks, &s.DeleteClicks, &s.UpdateClicks)
}

func (s *RepoPageState) EnsurePresetClickables(count int) {
	growClickables(count, &s.PresetClicks)
}

func (s *RepoPageState) EnsureRecentClickables(count int) {
	growClickables(count, &s.RecentClicks, &s.RecentRemoveClicks)
}

func (s *RepoPageState) EnsureRecentValuesClickables(count int) {
	growClickables(count, &s.RecentValuesClicks, &s.RecentValuesRemoveClicks)
}
