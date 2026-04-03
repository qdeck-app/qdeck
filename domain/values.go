package domain

// ValuesFile represents a parsed YAML values file as a flat key-value list.
type ValuesFile struct {
	Source    string         // file path or "default"
	Entries   []ValuesEntry  // pre-flattened, sorted by key
	RawValues map[string]any // original nested map (nil for default values)
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
