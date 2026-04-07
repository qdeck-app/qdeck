package domain

import "gopkg.in/yaml.v3"

// ValuesFile represents a parsed YAML values file as a flat key-value list.
type ValuesFile struct {
	Source    string         // file path or "default"
	Entries   []ValuesEntry  // pre-flattened, sorted by key
	RawValues map[string]any // original nested map (nil for default values)
	Indent    int            // detected YAML indentation spaces (0 = use default)
	NodeTree  *yaml.Node     // parsed yaml.Node tree for comment-preserving serialization (nil for defaults)
}

// ValuesEntry is a single key-value pair from a flattened YAML structure.
type ValuesEntry struct {
	Key     FlatKey
	Value   string // string representation of the value
	Type    string // "string", "int", "float", "bool", "null", "list", "map"
	Comment string // YAML comment describing this value
}

// DiffLine represents a single line in the diff output.
type DiffLine struct {
	Key          FlatKey
	DefaultValue string
	CustomValue  string
	Status       DiffStatus
}
