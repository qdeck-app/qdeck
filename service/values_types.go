package service

import (
	"gopkg.in/yaml.v3"

	"github.com/qdeck-app/qdeck/domain"
)

// AnchorRole identifies whether a YAML node at a flat key is a source anchor
// definition (`&name`), an alias usage (`*name`), or neither.
type AnchorRole uint8

const (
	AnchorRoleNone AnchorRole = iota
	AnchorRoleAnchor
	AnchorRoleAlias
)

// AnchorInfo describes YAML anchor metadata attached to one flat key.
type AnchorInfo struct {
	Role AnchorRole
	Name string
}

type FlatValues struct {
	Entries   []FlatValueEntry
	RawValues map[string]any        // original nested map for smart matching (nil for defaults)
	Indent    int                   // detected YAML indentation spaces (0 = use default)
	NodeTree  *yaml.Node            // parsed yaml.Node tree for comment-preserving serialization (nil for defaults)
	Anchors   map[string]AnchorInfo // flat key -> anchor/alias annotation for rendering badges; nil when no anchors present
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
