package service

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
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
// HeadComment is the cleaned comment block (no "# " prefix, lines joined by
// "\n") the user typed above the value. LineComment is the cleaned trailing
// inline comment on the same line as the value, e.g. "port: 8085 # default"
// → LineComment = "default". Both are empty when absent.
type OverrideEntry struct {
	Key         string
	Value       string
	Type        string
	HeadComment string
	LineComment string
}

// FlatEntriesToYAML reconstructs nested YAML from flat dot-separated key-value
// pairs. indent controls the number of spaces per nesting level in the output.
// Returns empty string when entries is empty. Builds a yaml.Node tree via
// upsertPath so head/line comments from each OverrideEntry can be attached to
// the appropriate key/value nodes before marshalling.
func FlatEntriesToYAML(entries []OverrideEntry, indent int) (string, error) {
	if len(entries) == 0 {
		return "", nil
	}

	root := &yaml.Node{Kind: yaml.MappingNode}

	for _, e := range entries {
		segments, err := parseKeySegments(e.Key)
		if err != nil {
			return "", fmt.Errorf("parse key %q: %w", e.Key, err)
		}

		if len(segments) == 0 {
			continue
		}

		if err := upsertPath(root, segments, convertValue(e.Value, e.Type)); err != nil {
			return "", fmt.Errorf("set key %q: %w", e.Key, err)
		}

		applyOverrideComments(root, segments, e.HeadComment, e.LineComment)
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

		effective, hasValue := findEffectiveScalar(workingTree, segments)
		valueUnchanged := hasValue && effective == e.Value

		effHead, effLine := effectiveComments(workingTree, segments)
		commentsUnchanged := overrideCommentsMatch(e.HeadComment, e.LineComment, effHead, effLine)

		if valueUnchanged && commentsUnchanged {
			continue
		}

		if !valueUnchanged {
			if err := upsertPath(workingTree, segments, convertValue(e.Value, e.Type)); err != nil {
				return "", fmt.Errorf("upsert key %q: %w", e.Key, err)
			}
		}

		if !commentsUnchanged {
			applyOverrideComments(workingTree, segments, e.HeadComment, e.LineComment)
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

// effectiveComments returns the cleaned (head, line) comment pair associated
// with the leaf at segments. head is the best head comment from key-then-val,
// line is the best line comment from val-then-key. Callers use these together
// to decide whether an OverrideEntry's explicit HeadComment/LineComment truly
// differ from what the file already encodes — unchanged comments are skipped
// so the original inline vs. head-block style survives an unrelated save.
func effectiveComments(root *yaml.Node, segments []keySegment) (string, string) {
	parent, slot, err := findParentSlot(root, segments)
	if err != nil {
		return "", ""
	}

	switch parent.Kind {
	case yaml.MappingNode:
		keyNode := parent.Content[slot-1]
		valNode := parent.Content[slot]

		head := bestHeadComment(keyNode.HeadComment, valNode.HeadComment)
		line := bestLineComment(valNode.LineComment, keyNode.LineComment)

		return head, line
	case yaml.SequenceNode:
		child := parent.Content[slot]

		return bestHeadComment(child.HeadComment), bestLineComment(child.LineComment)
	default:
		return "", ""
	}
}

// overrideCommentsMatch reports whether the user's explicit HeadComment and
// LineComment from the editor equal what the tree already encodes. Loading
// collapses an inline LineComment into the editor's leading-head-comment
// area (formatCommentForEditor), so a user-unchanged head can also match the
// tree's line comment and vice-versa. Only treated as a match when the user
// typed a single comment and the tree holds exactly one source.
func overrideCommentsMatch(entryHead, entryLine, effHead, effLine string) bool {
	if entryLine != "" {
		return entryHead == effHead && entryLine == effLine
	}

	if entryHead == "" {
		return effHead == "" && effLine == ""
	}

	return entryHead == effHead || entryHead == effLine
}

// applyOverrideComments normalizes comment placement on the leaf at segments:
// clears any existing head/line/foot comments on the key and value nodes,
// then writes the user's head block as HeadComment on the key (or the item
// node in a sequence) and the user's inline comment as LineComment on the
// value node. Empty strings leave the slot cleared. The "# " prefix is added
// here because yaml.v3 emits HeadComment and LineComment verbatim.
func applyOverrideComments(root *yaml.Node, segments []keySegment, head, line string) {
	parent, slot, err := findParentSlot(root, segments)
	if err != nil {
		return
	}

	var (
		headTarget *yaml.Node
		lineTarget *yaml.Node
	)

	switch parent.Kind {
	case yaml.MappingNode:
		keyNode := parent.Content[slot-1]
		valNode := parent.Content[slot]

		keyNode.HeadComment = ""
		keyNode.LineComment = ""
		keyNode.FootComment = ""
		valNode.HeadComment = ""
		valNode.LineComment = ""
		valNode.FootComment = ""

		headTarget = keyNode
		lineTarget = valNode
	case yaml.SequenceNode:
		item := parent.Content[slot]
		item.HeadComment = ""
		item.LineComment = ""
		item.FootComment = ""

		headTarget = item
		lineTarget = item
	default:
		return
	}

	if head != "" {
		headTarget.HeadComment = formatHeadComment(head)
	}

	if line != "" {
		lineTarget.LineComment = "# " + line
	}
}

// formatHeadComment prefixes each line of a multi-line head comment with
// "# ", producing the verbatim form yaml.v3 emits above the node.
func formatHeadComment(comment string) string {
	var b strings.Builder

	for i, line := range strings.Split(comment, "\n") {
		if i > 0 {
			b.WriteByte('\n')
		}

		b.WriteString("# ")
		b.WriteString(line)
	}

	return b.String()
}
