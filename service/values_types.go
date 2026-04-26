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

	// DocHeadComment holds the file-level banner — yaml.v3's DocumentNode head
	// comment, which sits before the first key in the source. Cleaned (no "# "
	// prefix, multi-line lines joined by "\n", blank lines dropped).
	// Empty when the source file has no banner.
	DocHeadComment string

	// DocFootComment holds the file-level trailer — yaml.v3's DocumentNode foot
	// comment that sits after the last key in the source. Cleaned the same way
	// as DocHeadComment. Empty when the source file has no trailer.
	DocFootComment string

	// FootComments maps a leaf flat key to the cleaned foot block that trails
	// it in the source. Populated by parseOrphanComments and consumed both for
	// rendering (one synthetic comment-row per entry) and for save round-trip
	// (re-emitted as that leaf's valNode.FootComment via DocComments.Foots).
	// nil when the source had no per-leaf foot blocks.
	FootComments map[string]string

	// SectionHeads holds user-edited section head-comment text, keyed by the
	// section's flat key, in "# "-prefixed verbatim form. Empty (nil) on load
	// — the deep-copied yaml.Node tree carries unedited section comments
	// through unchanged. Populated as the user types in section-row comment
	// editors; consumed by the save path to overwrite the corresponding
	// section key node's HeadComment.
	SectionHeads map[string]string

	// KeyPositions maps each flat key (sections + leaves) to its DFS-order
	// index in the source YAML node tree, so the UI can render entries in
	// file order rather than the alphabetical collation flattenValues
	// produces. Built once at load via buildFlatKeyPositions; nil when
	// NodeTree was unavailable. Domain-layer Entries stays alphabetical so
	// ComputeDiff's two-pointer merge keeps working.
	KeyPositions map[string]int
}

// EntryKind identifies the role of a FlatValueEntry. Most entries are leaves
// (Key→Value pair) or sections (mapping/sequence headers). Comment entries are
// synthetic rows inserted at the position of an orphan YAML comment block —
// e.g. a foot comment that sits between two siblings in the source file but
// isn't attached to either's key/value pair.
type EntryKind uint8

const (
	EntryKindLeaf    EntryKind = iota // ordinary scalar leaf (Key + Value + Type)
	EntryKindSection                  // mapping or sequence header (Key, empty Value, Type "map"/"list")
	EntryKindComment                  // orphan-comment row (empty Key, Comment carries text, FootAfterKey trails the leaf)
)

type FlatValueEntry struct {
	Key     string
	Value   string
	Type    string
	Depth   int
	Comment string
	Kind    EntryKind

	// FootAfterKey is set only for EntryKindComment rows. It holds the flat key
	// of the leaf whose value the comment block sits *after* in the source
	// file. The serializer uses this to write the text back as that leaf's
	// `valNode.FootComment`. Empty when the comment is the file's banner or
	// trailer (those round-trip via FlatValues.DocHeadComment/DocFootComment).
	FootAfterKey string

	// IsCustomOnly is true when this entry exists only in an override file
	// with no defaults counterpart. Set during ValuesPageState.RebuildUnifiedEntries
	// so the table can paint these rows with a distinct background — they
	// introduce a new key rather than overriding an existing chart default.
	IsCustomOnly bool
}

// IsSection returns true for entries that are section headers (non-leaf map/list nodes).
func (e FlatValueEntry) IsSection() bool {
	return e.Kind == EntryKindSection || (e.Value == "" && (e.Type == "map" || e.Type == "list"))
}

// IsComment returns true for synthetic orphan-comment rows. These rows have no
// editable value, no anchor badge, and are excluded from search/save paths.
func (e FlatValueEntry) IsComment() bool {
	return e.Kind == EntryKindComment
}

// IsFocusable returns true for rows that the table can focus (arrow-key
// navigation, programmatic FocusCmd). Section and comment rows are skipped —
// neither hosts a value editor.
func (e FlatValueEntry) IsFocusable() bool {
	return !e.IsSection() && !e.IsComment()
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
