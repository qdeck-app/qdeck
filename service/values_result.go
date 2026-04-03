package service

import "github.com/qdeck-app/qdeck/domain"

type FlatValues struct {
	Entries   []FlatValueEntry
	RawValues map[string]any // original nested map for smart matching (nil for defaults)
}

type FlatValueEntry struct {
	Key     string
	Value   string
	Type    string
	Depth   int
	Comment string
}

// IsSection returns true for entries that are section headers (non-leaf map/list nodes).
func (e FlatValueEntry) IsSection() bool {
	return e.Value == "" && (e.Type == "map" || e.Type == "list")
}

type DiffResult struct {
	Lines []DiffLine
	Stats DiffStats
}

type DiffLine struct {
	Key          string
	DefaultValue string
	CustomValue  string
	Status       domain.DiffStatus
}

type DiffStats struct {
	Added     int
	Removed   int
	Changed   int
	Unchanged int
}
