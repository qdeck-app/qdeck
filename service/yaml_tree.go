package service

import (
	"errors"
	"fmt"
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

// keySegment represents one segment of a parsed flat key.
// If isIndex is true, index holds the array position; otherwise name holds the map key.
type keySegment struct {
	name    string
	index   int
	isIndex bool
}

// parentRef tracks the container holding the current node so that
// a reallocated slice can be written back without re-walking from root.
type parentRef struct {
	container any
	key       string
	index     int
	isIndex   bool
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

// findNodeSubtree supports array index segments like "servers[0].host" by
// navigating into SequenceNode children.
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

// stepPhysical advances current by one existing segment, breaking any alias
// encountered so the mutation never leaks into a shared anchor target. Returns
// an error when the segment can't be resolved as a physical entry (out-of-range
// index, non-mapping parent, missing key). Used by all yaml.Node read-for-
// mutation walkers to keep their stepping logic in one place.
func stepPhysical(current *yaml.Node, seg keySegment, i int) (*yaml.Node, error) {
	if current.Kind == yaml.AliasNode {
		breakAliasInPlace(current)
	}

	if seg.isIndex {
		if current.Kind != yaml.SequenceNode || seg.index < 0 || seg.index >= len(current.Content) {
			return nil, fmt.Errorf("segment %d: index %d out of range", i, seg.index)
		}

		return current.Content[seg.index], nil
	}

	if current.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("segment %d (%q): parent is not a mapping", i, seg.name)
	}

	idx := mappingKeyIndex(current, seg.name)
	if idx < 0 {
		return nil, fmt.Errorf("segment %d (%q): key not present in local mapping", i, seg.name)
	}

	return current.Content[idx+1], nil
}

// resolvePhysicalForMutation walks segments and returns the final leaf node,
// breaking any alias encountered along the way so mutation never leaks into
// an anchor's shared target. Returns an error if the path is missing
// physically (merge-inherited keys cannot be mutated through this API).
func resolvePhysicalForMutation(tree *yaml.Node, segments []keySegment) (*yaml.Node, error) {
	current := tree

	for i, seg := range segments {
		next, err := stepPhysical(current, seg, i)
		if err != nil {
			return nil, err
		}

		current = next
	}

	return current, nil
}

// findParentSlot walks to the parent of the final segment and returns the
// parent node plus the Content index holding the final child's value, so the
// caller can swap the child (e.g. replace it with an AliasNode). For mappings
// the returned index is the value's position, so the key sits at index-1.
// Breaks aliases along the way for mutation safety.
func findParentSlot(tree *yaml.Node, segments []keySegment) (*yaml.Node, int, error) {
	if len(segments) == 0 {
		return nil, 0, errors.New("empty segments")
	}

	current := tree

	for i := 0; i < len(segments)-1; i++ {
		next, err := stepPhysical(current, segments[i], i)
		if err != nil {
			return nil, 0, err
		}

		current = next
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

// deepCopyYAMLNode preserves shared alias-to-anchor references: multiple alias
// nodes that point at the same anchor in the input still share a target in the
// copy.
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
	parent, slot, err := findParentSlot(root, segments)
	if err != nil {
		return false
	}

	switch parent.Kind {
	case yaml.SequenceNode:
		parent.Content = append(parent.Content[:slot], parent.Content[slot+1:]...)
	case yaml.MappingNode:
		// findParentSlot returns the value index for mappings; the key sits at
		// slot-1. Remove the key/value pair as a unit.
		parent.Content = append(parent.Content[:slot-1], parent.Content[slot+1:]...)
	default:
		return false
	}

	return true
}

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
