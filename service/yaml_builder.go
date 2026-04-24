package service

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	mergeTag = "!!merge"
	mergeKey = "<<"
)

const (
	DefaultYAMLIndent = 2
	maxYAMLIndent     = 8
	maxKeyIndex       = 10000
)

// DetectYAMLIndent scans raw YAML bytes and returns the smallest indentation
// found across all indented content lines. This avoids mis-detecting when the
// first indented line is deeply nested (e.g. 8 spaces at indent-4, depth-2).
// Tab-indented lines are ignored — only space indentation is considered.
// Returns DefaultYAMLIndent when no indentation is found (e.g. flat files or
// empty input). The result is clamped to [1, 8].
func DetectYAMLIndent(data []byte) int {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	minIndent := 0

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || line == "---" || line == "..." {
			continue
		}

		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" || trimmed[0] == '#' {
			continue
		}

		spaces := len(line) - len(trimmed)
		if spaces > 0 && (minIndent == 0 || spaces < minIndent) {
			minIndent = spaces
		}
	}

	if minIndent == 0 {
		return DefaultYAMLIndent
	}

	return min(max(minIndent, 1), maxYAMLIndent)
}

// OverrideEntry holds a single flat key-value pair for YAML reconstruction.
type OverrideEntry struct {
	Key   string
	Value string
	Type  string
}

// keySegment represents one segment of a parsed flat key.
// If isIndex is true, index holds the array position; otherwise name holds the map key.
type keySegment struct {
	name    string
	index   int
	isIndex bool
}

// FlatEntriesToYAML reconstructs nested YAML from flat dot-separated key-value pairs.
// indent controls the number of spaces per nesting level in the output.
// Returns empty string when entries is empty.
func FlatEntriesToYAML(entries []OverrideEntry, indent int) (string, error) {
	if len(entries) == 0 {
		return "", nil
	}

	root := make(map[string]any)

	for _, e := range entries {
		segments, err := parseKeySegments(e.Key)
		if err != nil {
			return "", fmt.Errorf("parse key %q: %w", e.Key, err)
		}

		if len(segments) == 0 {
			continue
		}

		val := convertValue(e.Value, e.Type)

		if err := setNestedValue(root, segments, val); err != nil {
			return "", fmt.Errorf("set key %q: %w", e.Key, err)
		}
	}

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)

	if err := enc.Encode(root); err != nil {
		return "", fmt.Errorf("marshal overrides to YAML: %w", err)
	}

	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("close YAML encoder: %w", err)
	}

	return buf.String(), nil
}

// parseKeySegments splits a flat key like "service.ports[0].name" into typed segments.
func parseKeySegments(key string) ([]keySegment, error) {
	if key == "" {
		return nil, nil
	}

	parts := strings.Split(key, ".")
	segments := make([]keySegment, 0, len(parts))

	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("empty segment in key %q", key)
		}

		bracketIdx := strings.IndexByte(part, '[')
		if bracketIdx < 0 {
			segments = append(segments, keySegment{name: part})

			continue
		}

		// Part before the first bracket is a map key (if non-empty).
		if bracketIdx > 0 {
			segments = append(segments, keySegment{name: part[:bracketIdx]})
		}

		// Extract all [N] indices from this part.
		rest := part[bracketIdx:]
		for len(rest) > 0 {
			if rest[0] != '[' {
				return nil, fmt.Errorf("trailing text %q after bracket in key segment %q", rest, part)
			}

			closeIdx := strings.IndexByte(rest, ']')
			if closeIdx < 0 {
				return nil, fmt.Errorf("unclosed bracket in key segment %q", part)
			}

			idx, err := strconv.Atoi(rest[1:closeIdx])
			if err != nil {
				return nil, fmt.Errorf("non-numeric bracket index in key segment %q", part)
			}

			if idx < 0 {
				return nil, fmt.Errorf("negative index %d in key segment %q", idx, part)
			}

			if idx > maxKeyIndex {
				return nil, fmt.Errorf("index %d exceeds maximum %d in key segment %q", idx, maxKeyIndex, part)
			}

			segments = append(segments, keySegment{index: idx, isIndex: true})
			rest = rest[closeIdx+1:]
		}
	}

	return segments, nil
}

// parentRef tracks the container holding the current node so that
// a reallocated slice can be written back without re-walking from root.
type parentRef struct {
	container any
	key       string
	index     int
	isIndex   bool
}

// setInParent writes val back into the parent container.
func setInParent(p parentRef, val any) error {
	if p.isIndex {
		slice, ok := p.container.([]any)
		if !ok {
			return fmt.Errorf("expected parent slice, got %T", p.container)
		}

		slice[p.index] = val
	} else {
		m, ok := p.container.(map[string]any)
		if !ok {
			return fmt.Errorf("expected parent map, got %T", p.container)
		}

		m[p.key] = val
	}

	return nil
}

// setNestedValue walks the nested structure, creating maps and slices as needed,
// and sets the final value. Tracks the parent reference inline so that
// reallocated slices can be written back in O(1) instead of re-walking from root.
func setNestedValue(root map[string]any, segments []keySegment, value any) error {
	var current any = root

	// parent tracks the container that holds current, so we can write back
	// a reallocated slice without re-walking from root.
	parent := parentRef{container: root}

	for i, seg := range segments {
		isLast := i == len(segments)-1

		if seg.isIndex {
			slice, ok := current.([]any)
			if !ok {
				return fmt.Errorf("expected slice at index segment %d, got %T", seg.index, current)
			}

			// Grow slice if needed and write back to parent.
			if len(slice) <= seg.index {
				needed := seg.index + 1 - len(slice)
				slice = append(slice, make([]any, needed)...)

				if err := setInParent(parent, slice); err != nil {
					return fmt.Errorf("write back grown slice at segment %d: %w", seg.index, err)
				}
			}

			if isLast {
				slice[seg.index] = value
			} else {
				next := segments[i+1]
				if slice[seg.index] == nil {
					if next.isIndex {
						slice[seg.index] = make([]any, 0)
					} else {
						slice[seg.index] = make(map[string]any)
					}
				}

				parent = parentRef{container: slice, index: seg.index, isIndex: true}
				current = slice[seg.index]
			}
		} else {
			m, ok := current.(map[string]any)
			if !ok {
				return fmt.Errorf("expected map at segment %q, got %T", seg.name, current)
			}

			if isLast {
				m[seg.name] = value
			} else {
				next := segments[i+1]

				if _, exists := m[seg.name]; !exists {
					if next.isIndex {
						m[seg.name] = make([]any, 0)
					} else {
						m[seg.name] = make(map[string]any)
					}
				}

				parent = parentRef{container: m, key: seg.name}
				current = m[seg.name]
			}
		}
	}

	return nil
}

// LookupRawValue navigates a nested map using a flat key (e.g., "auth.fernetKey" or "apiVersions[0]")
// and returns the value found at that path. Returns (nil, false, nil) when the key is empty or not found.
func LookupRawValue(raw map[string]any, flatKey string) (any, bool, error) {
	segments, err := parseKeySegments(flatKey)
	if err != nil {
		return nil, false, fmt.Errorf("lookup key %q: %w", flatKey, err)
	}

	if len(segments) == 0 {
		return nil, false, nil
	}

	var current any = raw

	for _, seg := range segments {
		if seg.isIndex {
			slice, ok := current.([]any)
			if !ok || seg.index >= len(slice) {
				return nil, false, nil
			}

			current = slice[seg.index]
		} else {
			m, ok := current.(map[string]any)
			if !ok {
				return nil, false, nil
			}

			v, exists := m[seg.name]
			if !exists {
				return nil, false, nil
			}

			current = v
		}
	}

	return current, true, nil
}

// SerializeValue converts a raw value to its string representation for editor display.
// Scalars are formatted with fmt, complex types (maps/slices) are marshaled as YAML.
func SerializeValue(val any) string {
	switch val.(type) {
	case map[string]any, []any:
		data, err := yaml.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}

		return strings.TrimRight(string(data), "\n")

	default:
		return fmt.Sprintf("%v", val)
	}
}

// SerializeNodeSubtree finds a yaml.Node subtree by dot-separated key path
// and marshals it, preserving YAML comments. Returns empty string and false
// if the node tree is nil or the key path is not found.
func SerializeNodeSubtree(nodeTree *yaml.Node, keyPath string, indent int) (string, bool) {
	if nodeTree == nil {
		return "", false
	}

	node := findNodeSubtree(nodeTree, keyPath)
	if node == nil {
		return "", false
	}

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)

	if err := enc.Encode(node); err != nil {
		return "", false
	}

	if err := enc.Close(); err != nil {
		return "", false
	}

	return strings.TrimRight(buf.String(), "\n"), true
}

// findNodeSubtree walks a yaml.Node tree to find the value node at a dot-separated key path.
// Supports array index segments like "servers[0].host" by navigating into SequenceNode children.
func findNodeSubtree(root *yaml.Node, keyPath string) *yaml.Node {
	segments := strings.Split(keyPath, ".")
	current := root

	for _, seg := range segments {
		// Handle array index segments like "items[0]" or bare "[0]".
		mapKey, idx, hasIndex := parseArraySegment(seg)

		if mapKey != "" {
			current = findMappingChild(current, mapKey)
			if current == nil {
				return nil
			}
		}

		if hasIndex {
			if current.Kind != yaml.SequenceNode || idx < 0 || idx >= len(current.Content) {
				return nil
			}

			current = current.Content[idx]
		} else if mapKey == "" {
			// Plain key segment — navigate into mapping.
			current = findMappingChild(current, seg)
			if current == nil {
				return nil
			}
		}
	}

	return current
}

// findMappingChild returns the value node for a key in a MappingNode, or nil.
func findMappingChild(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}

	return nil
}

// parseArraySegment splits "key[0]" into ("key", 0, true) or "[0]" into ("", 0, true).
// For plain keys like "host" it returns ("host", 0, false).
func parseArraySegment(seg string) (string, int, bool) {
	bracketIdx := strings.IndexByte(seg, '[')
	if bracketIdx < 0 {
		return seg, 0, false
	}

	closeIdx := strings.IndexByte(seg[bracketIdx:], ']')
	if closeIdx < 0 {
		return seg, 0, false
	}

	idxStr := seg[bracketIdx+1 : bracketIdx+closeIdx]

	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return seg, 0, false
	}

	return seg[:bracketIdx], idx, true
}

// AnchorNameRegex defines the legal character set for anchor names in this
// app: letters, digits, underscore, and hyphen. YAML itself is more permissive
// but a conservative subset avoids ambiguity and keeps user-facing display
// predictable.
var AnchorNameRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// SetNodeAnchor marks the node at flatKey as a YAML anchor named anchorName.
// Mutates tree in place. When the target is reached via an alias, the alias is
// broken before the anchor is set so the new anchor lives on a local copy, not
// on a node shared with other aliases. Returns an error if tree is nil, the
// key can't be reached as a physical location, or the name is invalid.
func SetNodeAnchor(tree *yaml.Node, flatKey, anchorName string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	if !AnchorNameRegex.MatchString(anchorName) {
		return fmt.Errorf("invalid anchor name %q: must match %s", anchorName, AnchorNameRegex.String())
	}

	segments, err := parseKeySegments(flatKey)
	if err != nil {
		return fmt.Errorf("parse key %q: %w", flatKey, err)
	}

	if len(segments) == 0 {
		return errors.New("empty key")
	}

	node, err := resolvePhysicalForMutation(tree, segments)
	if err != nil {
		return err
	}

	node.Anchor = anchorName

	return nil
}

// ClearNodeAnchor removes the anchor name from the node at flatKey. Aliases
// elsewhere in the tree that pointed at this anchor become dangling and will
// fail to marshal; callers should either also convert those aliases to
// literals first or accept the save failure. Returns nil if the target has no
// anchor set.
func ClearNodeAnchor(tree *yaml.Node, flatKey string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	segments, err := parseKeySegments(flatKey)
	if err != nil {
		return fmt.Errorf("parse key %q: %w", flatKey, err)
	}

	node, err := resolvePhysicalForMutation(tree, segments)
	if err != nil {
		return err
	}

	node.Anchor = ""

	return nil
}

// EnsurePath guarantees that flatKey exists in tree as a physical entry,
// creating the path with a scalar holding (value, typ) when missing. No-op
// when already present. Used by anchor/alias mutations so they don't fail
// on cells that only exist as editor overrides on top of default values.
// Because the tree is later re-patched against the overrides at save time,
// inserting the current editor value here doesn't commit to anything the
// user hasn't already shown on screen.
func EnsurePath(tree *yaml.Node, flatKey, value, typ string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	segments, err := parseKeySegments(flatKey)
	if err != nil {
		return fmt.Errorf("parse key %q: %w", flatKey, err)
	}

	if len(segments) == 0 {
		return nil
	}

	if _, err := resolvePhysicalForMutation(tree, segments); err == nil {
		return nil
	}

	if err := upsertPath(tree, segments, convertValue(value, typ)); err != nil {
		return fmt.Errorf("upsert path %q: %w", flatKey, err)
	}

	return nil
}

// RenameAnchor replaces the anchor name oldName with newName on the source
// node and on every alias that references it. Mutates tree in place. Returns
// an error if oldName is not defined anywhere, or if newName is not a legal
// anchor identifier.
//
// The .Alias pointer on each alias node continues to target the same source
// node, so no re-linking is needed — only the user-visible name changes on
// the source (.Anchor) and aliases (.Value).
func RenameAnchor(tree *yaml.Node, oldName, newName string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	if !AnchorNameRegex.MatchString(newName) {
		return fmt.Errorf("invalid anchor name %q: must match %s", newName, AnchorNameRegex.String())
	}

	if oldName == newName {
		return nil
	}

	if findAnchorNode(tree, oldName) == nil {
		return fmt.Errorf("anchor %q not found in tree", oldName)
	}

	if findAnchorNode(tree, newName) != nil {
		return fmt.Errorf("anchor %q already exists — choose a different name", newName)
	}

	visitNodes(tree, func(n *yaml.Node) {
		if n.Kind == yaml.AliasNode && n.Value == oldName {
			n.Value = newName

			return
		}

		if n.Anchor == oldName {
			n.Anchor = newName
		}
	})

	return nil
}

// DeleteAnchor removes an anchor and all references to it. Every alias
// pointing at the anchor is severed via breakAliasInPlace (its slot is
// replaced with a deep copy of the target's current contents); then the
// anchor definition's .Anchor field is cleared. Returns an error if the
// anchor isn't defined.
//
// No values are lost — the file simply no longer uses the anchor/alias
// syntax. Cells that were aliases keep their effective (resolved) values
// as plain YAML literals in place.
func DeleteAnchor(tree *yaml.Node, name string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	source := findAnchorNode(tree, name)
	if source == nil {
		return fmt.Errorf("anchor %q not found in tree", name)
	}

	// Collect the alias nodes first — mutating them during traversal is
	// fine for this walker (breakAliasInPlace only touches the node's own
	// fields) but deferring makes the intent explicit.
	var aliases []*yaml.Node

	visitNodes(tree, func(n *yaml.Node) {
		if n.Kind == yaml.AliasNode && n.Value == name {
			aliases = append(aliases, n)
		}
	})

	for _, a := range aliases {
		breakAliasInPlace(a)
	}

	source.Anchor = ""

	return nil
}

// visitNodes invokes visit on every non-nil descendant of root, including
// root itself. Recurses through Content; aliases are visited but not
// followed (the walker does not chase .Alias pointers, so anchor targets
// are visited via their regular position in the tree).
func visitNodes(root *yaml.Node, visit func(*yaml.Node)) {
	if root == nil {
		return
	}

	visit(root)

	for _, c := range root.Content {
		visitNodes(c, visit)
	}
}

// ClearNodeAlias replaces the alias at flatKey with a local deep copy of the
// anchored target's current contents. The anchor definition elsewhere stays
// intact; only this usage site is severed. No-op when the node at flatKey
// is not an alias. Returns an error if the path can't be reached.
func ClearNodeAlias(tree *yaml.Node, flatKey string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	segments, err := parseKeySegments(flatKey)
	if err != nil {
		return fmt.Errorf("parse key %q: %w", flatKey, err)
	}

	if len(segments) == 0 {
		return errors.New("empty key")
	}

	parent, childIdx, err := findParentSlot(tree, segments)
	if err != nil {
		return err
	}

	node := parent.Content[childIdx]
	if node.Kind != yaml.AliasNode {
		return nil
	}

	breakAliasInPlace(node)

	return nil
}

// SetNodeAlias replaces the node at flatKey with an alias pointing at the
// anchor named anchorName. The anchor must exist elsewhere in the tree.
// Mutates tree in place. Returns an error if the anchor can't be found or
// if flatKey is the anchor's own defining location (would create a cycle).
func SetNodeAlias(tree *yaml.Node, flatKey, anchorName string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	segments, err := parseKeySegments(flatKey)
	if err != nil {
		return fmt.Errorf("parse key %q: %w", flatKey, err)
	}

	if len(segments) == 0 {
		return errors.New("empty key")
	}

	target := findAnchorNode(tree, anchorName)
	if target == nil {
		return fmt.Errorf("anchor %q not found in tree", anchorName)
	}

	parent, childIdx, err := findParentSlot(tree, segments)
	if err != nil {
		return err
	}

	if parent.Content[childIdx] == target {
		return fmt.Errorf("cannot alias %q to itself (it is the anchor's defining location)", flatKey)
	}

	// Keep the alias's own comments from the prior node so annotations at this
	// site survive the conversion.
	prev := parent.Content[childIdx]
	alias := &yaml.Node{
		Kind:        yaml.AliasNode,
		Value:       anchorName,
		Alias:       target,
		HeadComment: prev.HeadComment,
		LineComment: prev.LineComment,
		FootComment: prev.FootComment,
	}
	parent.Content[childIdx] = alias

	return nil
}

// findAnchorNode returns the first node in the tree whose .Anchor equals name,
// or nil. Walks via mapping children, sequence items, and alias targets so
// anchors reachable through merge keys are still found.
func findAnchorNode(root *yaml.Node, name string) *yaml.Node {
	var found *yaml.Node

	var walk func(n *yaml.Node)

	walk = func(n *yaml.Node) {
		if n == nil || found != nil {
			return
		}

		if n.Anchor == name && n.Kind != yaml.AliasNode {
			found = n

			return
		}

		for _, c := range n.Content {
			walk(c)
		}
	}

	walk(root)

	return found
}

// resolvePhysicalForMutation walks segments and returns the final leaf node,
// breaking any alias encountered along the way so mutation never leaks into
// an anchor's shared target. Returns an error if the path is missing
// physically (merge-inherited keys cannot be mutated through this API).
func resolvePhysicalForMutation(tree *yaml.Node, segments []keySegment) (*yaml.Node, error) {
	current := tree

	for i, seg := range segments {
		if current.Kind == yaml.AliasNode {
			breakAliasInPlace(current)
		}

		if seg.isIndex {
			if current.Kind != yaml.SequenceNode || seg.index < 0 || seg.index >= len(current.Content) {
				return nil, fmt.Errorf("segment %d: index %d out of range", i, seg.index)
			}

			current = current.Content[seg.index]

			continue
		}

		if current.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("segment %d (%q): parent is not a mapping", i, seg.name)
		}

		idx := mappingKeyIndex(current, seg.name)
		if idx < 0 {
			return nil, fmt.Errorf("segment %d (%q): key not present in local mapping", i, seg.name)
		}

		current = current.Content[idx+1]
	}

	return current, nil
}

// findParentSlot walks to the parent of the final segment and returns the
// parent node plus the Content index holding the final child, so the caller
// can swap the child (e.g. replace it with an AliasNode). Breaks aliases
// along the way for mutation safety.
func findParentSlot(tree *yaml.Node, segments []keySegment) (*yaml.Node, int, error) {
	if len(segments) == 0 {
		return nil, 0, errors.New("empty segments")
	}

	current := tree

	for i := 0; i < len(segments)-1; i++ {
		if current.Kind == yaml.AliasNode {
			breakAliasInPlace(current)
		}

		seg := segments[i]
		if seg.isIndex {
			if current.Kind != yaml.SequenceNode || seg.index >= len(current.Content) {
				return nil, 0, fmt.Errorf("segment %d: index %d out of range", i, seg.index)
			}

			current = current.Content[seg.index]

			continue
		}

		if current.Kind != yaml.MappingNode {
			return nil, 0, fmt.Errorf("segment %d (%q): parent is not a mapping", i, seg.name)
		}

		idx := mappingKeyIndex(current, seg.name)
		if idx < 0 {
			return nil, 0, fmt.Errorf("segment %d (%q): key not present in local mapping", i, seg.name)
		}

		current = current.Content[idx+1]
	}

	if current.Kind == yaml.AliasNode {
		breakAliasInPlace(current)
	}

	last := segments[len(segments)-1]
	if last.isIndex {
		if current.Kind != yaml.SequenceNode || last.index >= len(current.Content) {
			return nil, 0, fmt.Errorf("final segment: index %d out of range", last.index)
		}

		return current, last.index, nil
	}

	if current.Kind != yaml.MappingNode {
		return nil, 0, fmt.Errorf("final segment (%q): parent is not a mapping", last.name)
	}

	idx := mappingKeyIndex(current, last.name)
	if idx < 0 {
		return nil, 0, fmt.Errorf("final segment (%q): key not present in local mapping", last.name)
	}

	return current, idx + 1, nil
}

// PatchNodeTree re-serializes a values file by mutating a deep copy of the
// parsed yaml.Node tree rather than rebuilding from scratch. This preserves
// anchors, aliases, comments, and scalar styles for subtrees the user did not
// edit. Falls back to FlatEntriesToYAML when no mapping root is available.
//
// Behavior for YAML anchors and aliases:
//   - Leaves whose editor value equals the underlying scalar (after alias
//     resolution) are left untouched, so aliases survive unedited saves.
//   - An edit that changes an aliased leaf breaks the alias locally: the
//     anchored node is deep-copied into the alias position and then patched.
//     The anchor definition and any other aliases pointing at it stay intact.
//   - Anchors attached to an edited scalar leaf are preserved on the node so
//     aliases elsewhere continue to resolve (to the new value).
//
// Behavior for YAML merge keys (`<<: *base`):
//   - Inherited keys are not treated as physically present. Upserts create a
//     new sibling entry that shadows the merge, which matches how Helm would
//     evaluate the override. Clearing an inherited-only key is a no-op — the
//     value comes from the merge target, not the local mapping.
func PatchNodeTree(root *yaml.Node, entries []OverrideEntry, indent int) (string, error) {
	if root == nil || root.Kind != yaml.MappingNode {
		return FlatEntriesToYAML(entries, indent)
	}

	workingTree := deepCopyYAMLNode(root)

	want := make(map[string]OverrideEntry, len(entries))
	for _, e := range entries {
		want[e.Key] = e
	}

	for _, k := range collectPhysicalLeafKeys(workingTree) {
		if _, keep := want[k]; keep {
			continue
		}

		segs, err := parseKeySegments(k)
		if err != nil {
			return "", fmt.Errorf("parse stale key %q: %w", k, err)
		}

		deletePath(workingTree, segs)
	}

	for _, e := range entries {
		segments, err := parseKeySegments(e.Key)
		if err != nil {
			return "", fmt.Errorf("parse key %q: %w", e.Key, err)
		}

		if len(segments) == 0 {
			continue
		}

		if effective, ok := findEffectiveScalar(workingTree, segments); ok && effective == e.Value {
			continue
		}

		if err := upsertPath(workingTree, segments, convertValue(e.Value, e.Type)); err != nil {
			return "", fmt.Errorf("upsert key %q: %w", e.Key, err)
		}
	}

	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)

	if err := enc.Encode(workingTree); err != nil {
		return "", fmt.Errorf("marshal patched tree: %w", err)
	}

	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("close YAML encoder: %w", err)
	}

	return buf.String(), nil
}

// deepCopyYAMLNode clones a yaml.Node tree so the original stays unchanged.
// Shared alias-to-anchor references are preserved: multiple alias nodes that
// point at the same anchor in the input will still share a target in the copy.
func deepCopyYAMLNode(n *yaml.Node) *yaml.Node {
	return copyNodeRec(n, make(map[*yaml.Node]*yaml.Node))
}

func copyNodeRec(n *yaml.Node, seen map[*yaml.Node]*yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}

	if cp, ok := seen[n]; ok {
		return cp
	}

	cp := &yaml.Node{
		Kind:        n.Kind,
		Style:       n.Style,
		Tag:         n.Tag,
		Value:       n.Value,
		Anchor:      n.Anchor,
		HeadComment: n.HeadComment,
		LineComment: n.LineComment,
		FootComment: n.FootComment,
		Line:        n.Line,
		Column:      n.Column,
	}
	seen[n] = cp

	if n.Alias != nil {
		cp.Alias = copyNodeRec(n.Alias, seen)
	}

	if len(n.Content) > 0 {
		cp.Content = make([]*yaml.Node, len(n.Content))
		for i, c := range n.Content {
			cp.Content[i] = copyNodeRec(c, seen)
		}
	}

	return cp
}

// ExtractAnchors walks root and returns a map from flat key to AnchorInfo for
// every node that either defines an anchor (`&name`) or is an alias usage
// (`*name`). Returns nil when the tree contains no anchors or aliases, so
// callers can use map-is-nil as a cheap "no badges to render" check.
//
// Keys inherited via merge keys (`<<: *base`) are not annotated; the badge
// should live at the alias or anchor position, not on every inherited leaf.
func ExtractAnchors(root *yaml.Node) map[string]AnchorInfo {
	if root == nil {
		return nil
	}

	out := make(map[string]AnchorInfo)
	walkAnchors(root, "", out)

	if len(out) == 0 {
		return nil
	}

	return out
}

func walkAnchors(n *yaml.Node, prefix string, out map[string]AnchorInfo) {
	if n == nil {
		return
	}

	if n.Kind == yaml.AliasNode {
		if prefix != "" && n.Value != "" {
			out[prefix] = AnchorInfo{Role: AnchorRoleAlias, Name: n.Value}
		}

		return
	}

	if n.Anchor != "" && prefix != "" {
		out[prefix] = AnchorInfo{Role: AnchorRoleAnchor, Name: n.Anchor}
	}

	switch n.Kind {
	case yaml.MappingNode:
		forEachMappingChild(n, prefix, func(childPrefix string, child *yaml.Node) {
			walkAnchors(child, childPrefix, out)
		})

	case yaml.SequenceNode:
		for i, c := range n.Content {
			walkAnchors(c, prefix+"["+strconv.Itoa(i)+"]", out)
		}

	case yaml.ScalarNode, yaml.DocumentNode, yaml.AliasNode:
		// Scalars have no children; aliases handled above; document nodes not
		// expected inside a root mapping walk.
	}
}

// forEachMappingChild invokes visit for every non-merge key/value pair in a
// mapping node, constructing the child's flat-key prefix. Merge keys are
// skipped because they introduce no new flat keys of their own.
func forEachMappingChild(n *yaml.Node, prefix string, visit func(childPrefix string, child *yaml.Node)) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode := n.Content[i]
		if keyNode.Tag == mergeTag || keyNode.Value == mergeKey {
			continue
		}

		childPrefix := keyNode.Value
		if prefix != "" {
			childPrefix = prefix + "." + keyNode.Value
		}

		visit(childPrefix, n.Content[i+1])
	}
}

// collectPhysicalLeafKeys returns flat keys for every scalar leaf that lives
// in the tree as concrete content. Aliases and keys inherited via merge keys
// are excluded — removing them would affect other parts of the file (or cannot
// be expressed at all), so they are not candidates for deletion.
func collectPhysicalLeafKeys(root *yaml.Node) []string {
	var out []string

	walkPhysicalLeaves(root, "", &out)

	return out
}

func walkPhysicalLeaves(n *yaml.Node, prefix string, out *[]string) {
	if n == nil {
		return
	}

	switch n.Kind {
	case yaml.MappingNode:
		if len(n.Content) == 0 {
			if prefix != "" {
				*out = append(*out, prefix)
			}

			return
		}

		forEachMappingChild(n, prefix, func(childPrefix string, child *yaml.Node) {
			walkPhysicalLeaves(child, childPrefix, out)
		})

	case yaml.SequenceNode:
		if len(n.Content) == 0 {
			if prefix != "" {
				*out = append(*out, prefix)
			}

			return
		}

		for i, c := range n.Content {
			walkPhysicalLeaves(c, prefix+"["+strconv.Itoa(i)+"]", out)
		}

	case yaml.ScalarNode:
		if prefix != "" {
			*out = append(*out, prefix)
		}

	case yaml.AliasNode, yaml.DocumentNode:
		// Aliases contribute no physical leaf at their position; the anchor's
		// content lives under a different key and will be walked there.
		// Document nodes do not appear inside a root mapping walk.
	}
}

// EffectiveScalarAt walks tree to flatKey resolving aliases and merge keys,
// and returns the scalar value that a YAML consumer would see at that path.
// Returns ("", false) when the path doesn't resolve to a scalar.
func EffectiveScalarAt(tree *yaml.Node, flatKey string) (string, bool) {
	if tree == nil {
		return "", false
	}

	segments, err := parseKeySegments(flatKey)
	if err != nil {
		return "", false
	}

	return findEffectiveScalar(tree, segments)
}

// findEffectiveScalar walks from root to segments resolving aliases and merge
// keys, and returns the scalar value that a YAML consumer would see at that
// path. Returns ("", false) when the path resolves to a non-scalar, a missing
// key, or cannot be resolved (e.g. merge source is not a mapping). Used to
// decide whether an override value is identical to what the file already
// yields — if so, writing it again would add a noisy local sibling that
// shadows a merge or anchor for no reason.
func findEffectiveScalar(root *yaml.Node, segments []keySegment) (string, bool) {
	current := resolveAlias(root)

	for _, seg := range segments {
		if current == nil {
			return "", false
		}

		if seg.isIndex {
			if current.Kind != yaml.SequenceNode || seg.index >= len(current.Content) {
				return "", false
			}

			current = resolveAlias(current.Content[seg.index])

			continue
		}

		if current.Kind != yaml.MappingNode {
			return "", false
		}

		next := mappingValueWithMerge(current, seg.name)
		if next == nil {
			return "", false
		}

		current = resolveAlias(next)
	}

	if current == nil || current.Kind != yaml.ScalarNode {
		return "", false
	}

	return current.Value, true
}

// resolveAlias follows alias pointers until a non-alias node is reached.
func resolveAlias(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}

	return n
}

// mappingValueWithMerge returns the value for name in a mapping, searching
// the local entries first and then any merge-key sources (<<: *anchor) in
// insertion order. Returns nil when the key is not reachable.
func mappingValueWithMerge(m *yaml.Node, name string) *yaml.Node {
	if idx := mappingKeyIndex(m, name); idx >= 0 {
		return m.Content[idx+1]
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if k.Tag != mergeTag && k.Value != mergeKey {
			continue
		}

		src := resolveAlias(m.Content[i+1])
		if src == nil {
			continue
		}

		if src.Kind == yaml.SequenceNode {
			for _, item := range src.Content {
				if v := mappingValueWithMerge(resolveAlias(item), name); v != nil {
					return v
				}
			}

			continue
		}

		if src.Kind != yaml.MappingNode {
			continue
		}

		if v := mappingValueWithMerge(src, name); v != nil {
			return v
		}
	}

	return nil
}

// mappingKeyIndex returns the Content index of the key node matching name in
// a mapping, or -1. Merge keys are skipped — upsert and delete always target
// the local physical entry.
func mappingKeyIndex(m *yaml.Node, name string) int {
	if m.Kind != yaml.MappingNode {
		return -1
	}

	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if k.Tag == mergeTag || k.Value == mergeKey {
			continue
		}

		if k.Value == name {
			return i
		}
	}

	return -1
}

// breakAliasInPlace converts an alias node into a local deep copy of its
// target, severing the alias at this position. The alias's own comments stay
// on the node so inline documentation at the alias site is preserved.
func breakAliasInPlace(alias *yaml.Node) {
	if alias.Kind != yaml.AliasNode || alias.Alias == nil {
		return
	}

	cp := deepCopyYAMLNode(alias.Alias)
	head, line, foot := alias.HeadComment, alias.LineComment, alias.FootComment

	alias.Kind = cp.Kind
	alias.Style = cp.Style
	alias.Tag = cp.Tag
	alias.Value = cp.Value
	alias.Anchor = ""
	alias.Alias = nil
	alias.Content = cp.Content
	alias.HeadComment = head
	alias.LineComment = line
	alias.FootComment = foot
}

// leafNodeFromValue builds a scalar node from the converted override value.
// When preserve points at an existing scalar, its style, comments, and anchor
// are carried over so formatting and anchor definitions survive the edit.
func leafNodeFromValue(value any, preserve *yaml.Node) *yaml.Node {
	node := &yaml.Node{}

	if err := node.Encode(value); err != nil {
		node.Kind = yaml.ScalarNode
		node.Tag = "!!str"
		node.Value = fmt.Sprint(value)
	}

	if preserve != nil && preserve.Kind == yaml.ScalarNode && node.Kind == yaml.ScalarNode {
		node.Style = preserve.Style
		node.Anchor = preserve.Anchor
		node.HeadComment = preserve.HeadComment
		node.LineComment = preserve.LineComment
		node.FootComment = preserve.FootComment
	}

	return node
}

// upsertPath sets value at the flat path inside root, creating intermediate
// mapping or sequence nodes as needed. Alias nodes encountered along the path
// (or at the leaf) are broken via deep copy so mutations never leak into the
// shared anchor target. Merge keys are ignored — writes always land in the
// local mapping, shadowing any inherited value.
func upsertPath(root *yaml.Node, segments []keySegment, value any) error {
	if len(segments) == 0 {
		return nil
	}

	current := root

	for i := 0; i < len(segments)-1; i++ {
		if current.Kind == yaml.AliasNode {
			breakAliasInPlace(current)
		}

		next, err := descendOrCreate(current, segments[i], segments[i+1])
		if err != nil {
			return fmt.Errorf("segment %d: %w", i, err)
		}

		current = next
	}

	if current.Kind == yaml.AliasNode {
		breakAliasInPlace(current)
	}

	return setLeaf(current, segments[len(segments)-1], value)
}

// descendOrCreate returns the node at seg inside current, creating an empty
// mapping or sequence when the slot is missing or not a container. Alias
// nodes encountered at the step are broken in place so mutations stay local.
func descendOrCreate(current *yaml.Node, seg, next keySegment) (*yaml.Node, error) {
	if seg.isIndex {
		return descendSequence(current, seg, next)
	}

	return descendMapping(current, seg, next)
}

func descendSequence(current *yaml.Node, seg, next keySegment) (*yaml.Node, error) {
	if current.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("expected sequence, got kind %d", current.Kind)
	}

	for len(current.Content) <= seg.index {
		current.Content = append(current.Content, &yaml.Node{
			Kind: yaml.ScalarNode, Tag: "!!null", Value: "null",
		})
	}

	child := current.Content[seg.index]
	if child.Kind == yaml.AliasNode {
		breakAliasInPlace(child)
	}

	if child.Kind != yaml.MappingNode && child.Kind != yaml.SequenceNode {
		child = containerForNextSegment(next)
		current.Content[seg.index] = child
	}

	return child, nil
}

func descendMapping(current *yaml.Node, seg, next keySegment) (*yaml.Node, error) {
	if current.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected mapping at %q, got kind %d", seg.name, current.Kind)
	}

	idx := mappingKeyIndex(current, seg.name)
	if idx < 0 {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg.name}
		valNode := containerForNextSegment(next)
		current.Content = append(current.Content, keyNode, valNode)

		return valNode, nil
	}

	child := current.Content[idx+1]
	if child.Kind == yaml.AliasNode {
		breakAliasInPlace(child)
	}

	if child.Kind != yaml.MappingNode && child.Kind != yaml.SequenceNode {
		child = containerForNextSegment(next)
		current.Content[idx+1] = child
	}

	return child, nil
}

// setLeaf writes value at the final segment inside current, preserving style,
// anchor, and comments when the slot already holds a compatible scalar. An
// alias leaf is replaced outright (the anchor definition elsewhere stays).
func setLeaf(current *yaml.Node, seg keySegment, value any) error {
	if seg.isIndex {
		if current.Kind != yaml.SequenceNode {
			return fmt.Errorf("expected sequence for leaf index, got kind %d", current.Kind)
		}

		for len(current.Content) <= seg.index {
			current.Content = append(current.Content, &yaml.Node{
				Kind: yaml.ScalarNode, Tag: "!!null", Value: "null",
			})
		}

		current.Content[seg.index] = leafNodeFromValue(value, current.Content[seg.index])

		return nil
	}

	if current.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping for leaf %q, got kind %d", seg.name, current.Kind)
	}

	idx := mappingKeyIndex(current, seg.name)
	if idx < 0 {
		keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: seg.name}
		current.Content = append(current.Content, keyNode, leafNodeFromValue(value, nil))

		return nil
	}

	existing := current.Content[idx+1]
	if existing.Kind == yaml.AliasNode {
		current.Content[idx+1] = leafNodeFromValue(value, nil)

		return nil
	}

	current.Content[idx+1] = leafNodeFromValue(value, existing)

	return nil
}

func containerForNextSegment(next keySegment) *yaml.Node {
	if next.isIndex {
		return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	}

	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

// deletePath removes the physical key at the flat path. Aliases along the way
// are broken (deep-copied) so the deletion stays local. Returns false if the
// key is not physically present (e.g. inherited via a merge key).
func deletePath(root *yaml.Node, segments []keySegment) bool {
	if len(segments) == 0 {
		return false
	}

	current := root

	for i, seg := range segments {
		isLast := i == len(segments)-1

		if current.Kind == yaml.AliasNode {
			breakAliasInPlace(current)
		}

		if seg.isIndex {
			if current.Kind != yaml.SequenceNode || seg.index >= len(current.Content) {
				return false
			}

			if isLast {
				current.Content = append(current.Content[:seg.index], current.Content[seg.index+1:]...)

				return true
			}

			child := current.Content[seg.index]
			if child.Kind == yaml.AliasNode {
				breakAliasInPlace(child)
			}

			current = child

			continue
		}

		if current.Kind != yaml.MappingNode {
			return false
		}

		idx := mappingKeyIndex(current, seg.name)
		if idx < 0 {
			return false
		}

		if isLast {
			current.Content = append(current.Content[:idx], current.Content[idx+2:]...)

			return true
		}

		child := current.Content[idx+1]
		if child.Kind == yaml.AliasNode {
			breakAliasInPlace(child)
		}

		current = child
	}

	return false
}

// convertValue converts a string value to the appropriate Go type for YAML marshaling.
func convertValue(value, typ string) any {
	switch typ {
	case "number":
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return n
		}

		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}

	case "bool":
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}

	case typeNull:
		if value == typeNull || value == "~" || value == "" {
			return nil
		}
	}

	return value
}
