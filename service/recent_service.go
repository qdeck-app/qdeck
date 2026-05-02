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
	if err := entry.IsValid(); err != nil {
		return fmt.Errorf("add recent chart: %w", err)
	}

	entry.OpenedAt = time.Now()

	return addRecent(ctx, s.store, "add recent chart", maxRecentCharts,
		func(data *storage.AppData) *[]domain.RecentChart { return &data.RecentCharts },
		entry,
		func(existing domain.RecentChart) bool {
			// Drop duplicates and invalid entries (non-local with empty RepoName).
			if !existing.IsLocal() && existing.RepoName == "" {
				return true
			}

			return matchesChart(existing, entry)
		},
	)
}

// RemoveRecentChart removes a chart at the given index.
//
//nolint:dupl // same shape as RemoveRecentValues{,Entry} but a different AppData slice
func (s *RecentService) RemoveRecentChart(ctx context.Context, idx int) error {
	return removeAt(ctx, s.store, "remove recent chart", idx,
		func(data *storage.AppData) *[]domain.RecentChart { return &data.RecentCharts },
	)
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
	if path == "" {
		return errors.New("add recent values: path required")
	}

	entry := domain.RecentValuesFile{Path: path, OpenedAt: time.Now()}

	return addRecent(ctx, s.store, "add recent values", maxRecentValues,
		func(data *storage.AppData) *[]domain.RecentValuesFile { return &data.RecentValues },
		entry,
		func(existing domain.RecentValuesFile) bool { return existing.Path == path },
	)
}

// RemoveRecentValues removes a values file at the given index.
//
//nolint:dupl // same shape as RemoveRecentChart / RemoveRecentValuesEntry but a different AppData slice
func (s *RecentService) RemoveRecentValues(ctx context.Context, idx int) error {
	return removeAt(ctx, s.store, "remove recent values", idx,
		func(data *storage.AppData) *[]domain.RecentValuesFile { return &data.RecentValues },
	)
}

// LoadShowDocs returns the persisted "show docs" preference.
// Returns false when the preference has never been saved.
func (s *RecentService) LoadShowDocs(ctx context.Context) (bool, error) {
	data, err := s.store.Load(ctx)
	if err != nil {
		return false, fmt.Errorf("load show docs: %w", err)
	}

	if data.ShowDocs == nil {
		return false, nil
	}

	return *data.ShowDocs, nil
}

// SaveShowDocs persists the "show docs" preference.
func (s *RecentService) SaveShowDocs(ctx context.Context, show bool) error {
	if err := s.store.Update(ctx, func(data *storage.AppData) error {
		data.ShowDocs = &show

		return nil
	}); err != nil {
		return fmt.Errorf("save show docs: %w", err)
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
	if entry.ValuesPath == "" {
		return errors.New("add recent values entry: valuesPath required")
	}

	entry.OpenedAt = time.Now()

	return addRecent(ctx, s.store, "add recent values entry", maxRecentValuesEntries,
		func(data *storage.AppData) *[]domain.RecentValuesEntry { return &data.RecentValuesEntries },
		entry,
		func(existing domain.RecentValuesEntry) bool {
			return existing.ValuesPath == entry.ValuesPath &&
				matchesChart(existing.ChartRef(), entry.ChartRef())
		},
	)
}

// RemoveRecentValuesEntry removes a values entry at the given index.
//
//nolint:dupl // same shape as RemoveRecentChart / RemoveRecentValues but a different AppData slice
func (s *RecentService) RemoveRecentValuesEntry(ctx context.Context, idx int) error {
	return removeAt(ctx, s.store, "remove recent values entry", idx,
		func(data *storage.AppData) *[]domain.RecentValuesEntry { return &data.RecentValuesEntries },
	)
}

// addRecent runs the standard "prepend, dedupe, cap" update against a list
// inside AppData, extracting the operation label into error messages for
// uniform wrapping across the Add* methods.
func addRecent[T any](
	ctx context.Context,
	store *storage.JSONStore,
	op string,
	maxLen int,
	pick func(*storage.AppData) *[]T,
	entry T,
	isDup func(existing T) bool,
) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", op, ctx.Err())
	}

	err := store.Update(ctx, func(data *storage.AppData) error {
		list := pick(data)
		*list = slices.DeleteFunc(*list, isDup)
		*list = slices.Insert(*list, 0, entry)

		if len(*list) > maxLen {
			*list = (*list)[:maxLen]
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

// removeAt deletes the entry at idx from a list inside AppData, returning a
// uniform out-of-range error when idx isn't in bounds.
func removeAt[T any](
	ctx context.Context,
	store *storage.JSONStore,
	op string,
	idx int,
	pick func(*storage.AppData) *[]T,
) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%s: %w", op, ctx.Err())
	}

	err := store.Update(ctx, func(data *storage.AppData) error {
		list := pick(data)
		if idx < 0 || idx >= len(*list) {
			return fmt.Errorf("%s: index %d out of range [0, %d)", op, idx, len(*list))
		}

		*list = slices.Delete(*list, idx, idx+1)

		return nil
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

// matchesChart returns true when a and b refer to the same chart. Local paths
// compare byte-equal; remote charts compare repo/chart case-insensitively plus
// exact version. Used both for Recent-chart dedup and for Values-entry dedup
// via the embedded chart reference.
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
