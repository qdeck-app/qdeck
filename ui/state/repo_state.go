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

	// Widget state: repo list
	RepoList   widget.List
	RepoClicks []widget.Clickable

	// Widget state: add repo form
	AddNameEditor  widget.Editor
	AddURLEditor   widget.Editor
	AddButton      widget.Clickable
	SubmitButton   widget.Clickable
	AddFormVisible bool

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

	// Chart file picker button (inside compact drop zone)
	ChartPickerButton widget.Clickable

	// FileDropActive is true when files are being dragged over the window.
	FileDropActive bool

	// DropSupported is true when the platform supports drag-and-drop.
	DropSupported bool
}

// EnsureClickables grows clickable slices to match repo count.
// Only allocates when the repo list grows beyond current capacity.
func (s *RepoPageState) EnsureClickables(count int) {
	for len(s.RepoClicks) < count {
		s.RepoClicks = append(s.RepoClicks, widget.Clickable{})
		s.DeleteClicks = append(s.DeleteClicks, widget.Clickable{})
		s.UpdateClicks = append(s.UpdateClicks, widget.Clickable{})
	}
}

// EnsureRecentClickables grows recent chart clickable slices.
func (s *RepoPageState) EnsureRecentClickables(count int) {
	for len(s.RecentClicks) < count {
		s.RecentClicks = append(s.RecentClicks, widget.Clickable{})
		s.RecentRemoveClicks = append(s.RecentRemoveClicks, widget.Clickable{})
	}
}
