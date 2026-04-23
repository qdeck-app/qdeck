package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/qdeck-app/qdeck/domain"
	"github.com/qdeck-app/qdeck/infrastructure/storage"
)

const (
	maxRecentCharts = 20
	maxRecentValues = 10
)

// RecentService manages recent charts and values files.
// All mutating methods use JSONStore.Update for atomic read-modify-write,
// eliminating the need for a service-level mutex.
type RecentService struct {
	store *storage.JSONStore
}

func NewRecentService(store *storage.JSONStore) *RecentService {
	return &RecentService{store: store}
}

// ListRecentCharts returns all recent charts sorted by most recently opened.
//
//nolint:dupl // Structurally similar to ListRecentValues but operates on different slice.
func (s *RecentService) ListRecentCharts(ctx context.Context) ([]domain.RecentChart, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("list recent charts: %w", ctx.Err())
	}

	data, err := s.store.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load recent charts: %w", err)
	}

	return data.RecentCharts, nil
}

// AddRecentChart adds a chart to the recent list, deduplicating and enforcing max limit.
func (s *RecentService) AddRecentChart(ctx context.Context, entry domain.RecentChart) error {
	if ctx.Err() != nil {
		return fmt.Errorf("add recent chart: %w", ctx.Err())
	}

	if err := entry.IsValid(); err != nil {
		return fmt.Errorf("add recent chart: %w", err)
	}

	if err := s.store.Update(ctx, func(data *storage.AppData) error {
		entry.OpenedAt = time.Now()

		// Remove duplicates and invalid entries (non-local with empty RepoName).
		data.RecentCharts = slices.DeleteFunc(data.RecentCharts, func(existing domain.RecentChart) bool {
			if !existing.IsLocal() && existing.RepoName == "" {
				return true
			}

			return matchesChart(existing, entry)
		})

		// Prepend new entry
		data.RecentCharts = slices.Insert(data.RecentCharts, 0, entry)

		// Enforce max limit
		if len(data.RecentCharts) > maxRecentCharts {
			data.RecentCharts = data.RecentCharts[:maxRecentCharts]
		}

		return nil
	}); err != nil {
		return fmt.Errorf("add recent chart: %w", err)
	}

	return nil
}

// RemoveRecentChart removes a chart at the given index.
//
//nolint:dupl // Structurally similar to RemoveRecentValues but operates on different slice.
func (s *RecentService) RemoveRecentChart(ctx context.Context, idx int) error {
	if ctx.Err() != nil {
		return fmt.Errorf("remove recent chart: %w", ctx.Err())
	}

	if err := s.store.Update(ctx, func(data *storage.AppData) error {
		if idx < 0 || idx >= len(data.RecentCharts) {
			return fmt.Errorf("remove recent chart: index %d out of range [0, %d)", idx, len(data.RecentCharts))
		}

		data.RecentCharts = slices.Delete(data.RecentCharts, idx, idx+1)

		return nil
	}); err != nil {
		return fmt.Errorf("remove recent chart: %w", err)
	}

	return nil
}

// ListRecentValues returns all recent values files sorted by most recently opened.
//
//nolint:dupl // Structurally similar to ListRecentCharts but operates on different slice.
func (s *RecentService) ListRecentValues(ctx context.Context) ([]domain.RecentValuesFile, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("list recent values: %w", ctx.Err())
	}

	data, err := s.store.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load recent values: %w", err)
	}

	return data.RecentValues, nil
}

// AddRecentValues adds a values file path to the recent list.
func (s *RecentService) AddRecentValues(ctx context.Context, path string) error {
	if ctx.Err() != nil {
		return fmt.Errorf("add recent values: %w", ctx.Err())
	}

	if path == "" {
		return errors.New("add recent values: path required")
	}

	if err := s.store.Update(ctx, func(data *storage.AppData) error {
		entry := domain.RecentValuesFile{
			Path:     path,
			OpenedAt: time.Now(),
		}

		data.RecentValues = slices.DeleteFunc(data.RecentValues, func(existing domain.RecentValuesFile) bool {
			return existing.Path == path
		})

		// Prepend new entry
		data.RecentValues = slices.Insert(data.RecentValues, 0, entry)

		// Enforce max limit
		if len(data.RecentValues) > maxRecentValues {
			data.RecentValues = data.RecentValues[:maxRecentValues]
		}

		return nil
	}); err != nil {
		return fmt.Errorf("add recent values: %w", err)
	}

	return nil
}

// RemoveRecentValues removes a values file at the given index.
//
//nolint:dupl // Structurally similar to RemoveRecentChart but operates on different slice.
func (s *RecentService) RemoveRecentValues(ctx context.Context, idx int) error {
	if ctx.Err() != nil {
		return fmt.Errorf("remove recent values: %w", ctx.Err())
	}

	if err := s.store.Update(ctx, func(data *storage.AppData) error {
		if idx < 0 || idx >= len(data.RecentValues) {
			return fmt.Errorf("remove recent values: index %d out of range [0, %d)", idx, len(data.RecentValues))
		}

		data.RecentValues = slices.Delete(data.RecentValues, idx, idx+1)

		return nil
	}); err != nil {
		return fmt.Errorf("remove recent values: %w", err)
	}

	return nil
}

// LoadShowComments returns the persisted "show comments" preference.
// Returns false when the preference has never been saved.
func (s *RecentService) LoadShowComments(ctx context.Context) (bool, error) {
	data, err := s.store.Load(ctx)
	if err != nil {
		return false, fmt.Errorf("load show comments: %w", err)
	}

	if data.ShowComments == nil {
		return false, nil
	}

	return *data.ShowComments, nil
}

// SaveShowComments persists the "show comments" preference.
func (s *RecentService) SaveShowComments(ctx context.Context, show bool) error {
	if err := s.store.Update(ctx, func(data *storage.AppData) error {
		data.ShowComments = &show

		return nil
	}); err != nil {
		return fmt.Errorf("save show comments: %w", err)
	}

	return nil
}

// LoadChartUIState returns the persisted UI state for the given chart key.
// The bool is false when no state has been saved for the key yet.
func (s *RecentService) LoadChartUIState(ctx context.Context, key string) (domain.ChartUIState, bool, error) {
	if key == "" {
		return domain.ChartUIState{}, false, nil
	}

	data, err := s.store.Load(ctx)
	if err != nil {
		return domain.ChartUIState{}, false, fmt.Errorf("load chart ui state: %w", err)
	}

	st, ok := data.ChartUIStates[key]

	return st, ok, nil
}

// maxChartUIStates caps the per-chart UI state map. The user can only
// meaningfully come back to ~maxRecentCharts charts anyway; keeping ~5x
// headroom covers charts that fell out of Recents but get reopened soon.
const maxChartUIStates = 100

// SaveChartUIState persists the UI state for the given chart key, stamping
// LastTouchedAt with the current time. When the map grows past
// maxChartUIStates, the entry with the oldest LastTouchedAt is evicted —
// approximate LRU, so the most-recently-used charts survive. One eviction per
// Save call is sufficient because the cap can only be exceeded by one entry.
func (s *RecentService) SaveChartUIState(ctx context.Context, key string, st domain.ChartUIState) error {
	if key == "" {
		return nil
	}

	st.LastTouchedAt = time.Now()

	if err := s.store.Update(ctx, func(data *storage.AppData) error {
		if data.ChartUIStates == nil {
			data.ChartUIStates = make(map[string]domain.ChartUIState)
		}

		data.ChartUIStates[key] = st

		if len(data.ChartUIStates) > maxChartUIStates {
			var (
				oldestKey string
				oldestAt  time.Time
				seeded    bool
			)

			for k, v := range data.ChartUIStates {
				if k == key {
					continue
				}

				if !seeded || v.LastTouchedAt.Before(oldestAt) {
					oldestKey = k
					oldestAt = v.LastTouchedAt
					seeded = true
				}
			}

			if seeded {
				delete(data.ChartUIStates, oldestKey)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("save chart ui state: %w", err)
	}

	return nil
}

const maxRecentValuesEntries = 10

// ListRecentValuesEntries returns all recent values+chart pairs.
//
//nolint:dupl // Structurally similar to ListRecentCharts but operates on different slice.
func (s *RecentService) ListRecentValuesEntries(ctx context.Context) ([]domain.RecentValuesEntry, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("list recent values entries: %w", ctx.Err())
	}

	data, err := s.store.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load recent values entries: %w", err)
	}

	return data.RecentValuesEntries, nil
}

// AddRecentValuesEntry adds a values+chart pair, deduplicating and enforcing max limit.
func (s *RecentService) AddRecentValuesEntry(ctx context.Context, entry domain.RecentValuesEntry) error {
	if ctx.Err() != nil {
		return fmt.Errorf("add recent values entry: %w", ctx.Err())
	}

	if entry.ValuesPath == "" {
		return errors.New("add recent values entry: valuesPath required")
	}

	if err := s.store.Update(ctx, func(data *storage.AppData) error {
		entry.OpenedAt = time.Now()

		data.RecentValuesEntries = slices.DeleteFunc(data.RecentValuesEntries, func(existing domain.RecentValuesEntry) bool {
			return matchesValuesEntry(existing, entry)
		})

		data.RecentValuesEntries = slices.Insert(data.RecentValuesEntries, 0, entry)

		if len(data.RecentValuesEntries) > maxRecentValuesEntries {
			data.RecentValuesEntries = data.RecentValuesEntries[:maxRecentValuesEntries]
		}

		return nil
	}); err != nil {
		return fmt.Errorf("add recent values entry: %w", err)
	}

	return nil
}

// RemoveRecentValuesEntry removes a values entry at the given index.
//
//nolint:dupl // Structurally similar to RemoveRecentChart but operates on different slice.
func (s *RecentService) RemoveRecentValuesEntry(ctx context.Context, idx int) error {
	if ctx.Err() != nil {
		return fmt.Errorf("remove recent values entry: %w", ctx.Err())
	}

	if err := s.store.Update(ctx, func(data *storage.AppData) error {
		if idx < 0 || idx >= len(data.RecentValuesEntries) {
			return fmt.Errorf("remove recent values entry: index %d out of range [0, %d)", idx, len(data.RecentValuesEntries))
		}

		data.RecentValuesEntries = slices.Delete(data.RecentValuesEntries, idx, idx+1)

		return nil
	}); err != nil {
		return fmt.Errorf("remove recent values entry: %w", err)
	}

	return nil
}

func matchesValuesEntry(a, b domain.RecentValuesEntry) bool {
	if a.ValuesPath != b.ValuesPath {
		return false
	}

	return matchesChart(a.ChartRef(), b.ChartRef())
}

func matchesChart(a, b domain.RecentChart) bool {
	if a.IsLocal() && b.IsLocal() {
		return a.LocalPath == b.LocalPath
	}

	if !a.IsLocal() && !b.IsLocal() {
		return strings.EqualFold(a.RepoName, b.RepoName) &&
			strings.EqualFold(a.ChartName, b.ChartName) &&
			a.Version == b.Version
	}

	return false
}
