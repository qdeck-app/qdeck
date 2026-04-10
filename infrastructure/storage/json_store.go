package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/qdeck-app/qdeck/domain"
)

// AppData is the top-level JSON structure persisted to disk.
type AppData struct {
	RecentCharts []domain.RecentChart      `json:"recentCharts"`
	RecentValues []domain.RecentValuesFile `json:"recentValues"`
	ShowComments *bool                     `json:"showComments,omitempty"`
}

// JSONStore reads and writes AppData to a JSON file.
type JSONStore struct {
	filePath string
	mu       sync.Mutex
}

func NewJSONStore() (*JSONStore, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve config dir: %w", err)
	}

	dir := filepath.Join(configDir, configDirName)
	if err := os.MkdirAll(dir, configDirPerm); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	return &JSONStore{
		filePath: filepath.Join(dir, dataFileName),
	}, nil
}

func (s *JSONStore) Load(ctx context.Context) (*AppData, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("load: %w", ctx.Err())
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.loadLocked()
}

func (s *JSONStore) loadLocked() (*AppData, error) {
	data, err := os.ReadFile(s.filePath) //nolint:gosec // path constructed from UserConfigDir, not user input
	if err != nil {
		if os.IsNotExist(err) {
			return &AppData{}, nil
		}

		return nil, fmt.Errorf("read data file: %w", err)
	}

	var appData AppData
	if err := json.Unmarshal(data, &appData); err != nil {
		return nil, fmt.Errorf("unmarshal data file: %w", err)
	}

	return &appData, nil
}

func (s *JSONStore) Save(ctx context.Context, appData *AppData) error {
	if ctx.Err() != nil {
		return fmt.Errorf("save: %w", ctx.Err())
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.saveLocked(appData)
}

// Update atomically loads the persisted data, passes it to fn for modification,
// and saves the result. The store lock is held for the entire read-modify-write
// cycle, preventing interleaved updates from concurrent callers.
func (s *JSONStore) Update(ctx context.Context, fn func(*AppData) error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("update: %w", ctx.Err())
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.loadLocked()
	if err != nil {
		return err
	}

	if err := fn(data); err != nil {
		return err
	}

	return s.saveLocked(data)
}

func (s *JSONStore) saveLocked(appData *AppData) error {
	data, err := json.MarshalIndent(appData, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal data: %w", err)
	}

	if err := os.WriteFile(s.filePath, data, dataFilePerm); err != nil {
		return fmt.Errorf("write data file: %w", err)
	}

	return nil
}
