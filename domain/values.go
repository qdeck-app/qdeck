package domain

import "gopkg.in/yaml.v3"

// ValuesFile represents a parsed YAML values file as a flat key-value list.
type ValuesFile struct {
	Source    string         // file path or "default"
	Entries   []ValuesEntry  // pre-flattened, sorted by key
	RawValues map[string]any // original nested map (nil for default values)
	Indent    int            // detected YAML indentation spaces (0 = use default)
	NodeTree  *yaml.Node     // parsed yaml.Node tree for comment-preserving serialization (nil for defaults)

	// Encoding and LineEnding describe the source bytes — sniffed once at read
	// time by service.DetectFileEncoding and preserved on save through
	// service.EncodeForFile (BOM prepended for "UTF-8 BOM", '\n' rewritten to
	// '\r\n' for "CRLF"). Empty when the source wasn't a real file on disk
	// (e.g. chart defaults parsed from in-memory bytes, editor content);
	// LineEnding is also empty when the file contains no newline.
	Encoding   string
	LineEnding string

	// DocHeadComment / DocFootComment carry yaml.v3's DocumentNode-level head
	// and foot comments — file banners and trailers that aren't attached to any
	// leaf key. Cleaned (no "# " prefix). Empty when the source had none.
	DocHeadComment string
	DocFootComment string

	// FootComments maps a leaf flat key to its trailing foot-comment block —
	// `valNode.FootComment` in yaml.v3 terms. These are orphan blocks that sit
	// after a value but before the next sibling, often blank-line separated.
	// Used to synthesize comment-only rows in the flattened entry list, and to
	// round-trip the text on save. nil when the source had none.
	FootComments map[string]string
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
