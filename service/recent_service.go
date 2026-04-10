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
