package service

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

type ScalarEdit struct {
	FlatKey  string
	NewValue string
}

// SpliceScalarValues rewrites the value portion of plain scalar leaves in raw,
// preserving every other byte. All-or-nothing: returns (raw, false) if any edit
// can't be safely applied so callers fall back to the encoder path uniformly.
func SpliceScalarValues(raw []byte, root *yaml.Node, edits []ScalarEdit) ([]byte, bool) {
	if root == nil || len(raw) == 0 || len(edits) == 0 {
		return raw, false
	}

	out := raw

	for _, e := range edits {
		next, ok := spliceScalarValue(out, root, e.FlatKey, e.NewValue)
		if !ok {
			return raw, false
		}

		out = next
	}

	return out, true
}

func spliceScalarValue(raw []byte, root *yaml.Node, flatKey, newValue string) ([]byte, bool) {
	valNode := findNodeSubtree(root, flatKey)
	if valNode == nil || valNode.Kind != yaml.ScalarNode {
		return raw, false
	}

	// Style==0 is plain; any non-zero style needs quote-aware end detection.
	if valNode.Style != 0 {
		return raw, false
	}

	if valNode.Line <= 0 || valNode.Column <= 0 {
		return raw, false
	}

	if !isYAMLPlainSafe(newValue) {
		return raw, false
	}

	lines := bytes.Split(raw, []byte("\n"))
	if valNode.Line > len(lines) {
		return raw, false
	}

	line := lines[valNode.Line-1]
	valueStart := valNode.Column - 1

	if valueStart < 0 || valueStart >= len(line) {
		return raw, false
	}

	valueEnd := plainScalarLineEnd(line, valueStart)

	newLine := make([]byte, 0, len(line)+len(newValue))
	newLine = append(newLine, line[:valueStart]...)
	newLine = append(newLine, []byte(newValue)...)
	newLine = append(newLine, line[valueEnd:]...)

	lines[valNode.Line-1] = newLine

	return bytes.Join(lines, []byte("\n")), true
}

// plainScalarLineEnd returns the offset at which a plain scalar ends:
// the first ` #` sequence (yaml.v3's inline-comment marker) or end of line.
// The offset points at the start of trailing whitespace so the splice
// preserves the spaces between value and comment.
func plainScalarLineEnd(line []byte, start int) int {
	for i := start; i < len(line); i++ {
		if line[i] != ' ' && line[i] != '\t' {
			continue
		}

		j := i
		for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
			j++
		}

		if j < len(line) && line[j] == '#' {
			return i
		}
	}

	return len(line)
}

// isYAMLPlainSafe reports whether v survives encoding as a plain (unquoted)
// scalar without yaml.v3 promoting it to a quoted form.
func isYAMLPlainSafe(v string) bool {
	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(DefaultYAMLIndent)

	if err := enc.Encode(&yaml.Node{Kind: yaml.ScalarNode, Value: v}); err != nil {
		return false
	}

	_ = enc.Close()

	out := strings.TrimRight(buf.String(), "\n")

	return out == v
}

// planScalarSplice decides whether the leaf-line splicer can apply all
// entries byte-faithfully. Returns (edits, true) when every entry is either
// a no-op or a plain-scalar value change with matching comments; returns
// (nil, false) when any entry would need the encoder fallback.
func planScalarSplice(raw []byte, root *yaml.Node, entries []OverrideEntry, docs DocComments) ([]ScalarEdit, bool) {
	if !docsMatchSource(raw, docs) {
		return nil, false
	}

	var edits []ScalarEdit

	for _, e := range entries {
		segments, err := parseKeySegments(e.Key)
		if err != nil {
			return nil, false
		}

		if e.Type == typeMap || e.Type == typeList {
			if treeNodeMatchesContainerType(root, segments, e.Type) {
				continue
			}

			return nil, false
		}

		effective, hasValue := findEffectiveScalar(root, segments)
		if !hasValue {
			return nil, false
		}

		effHead, effLine, viaAlias := effectiveComments(root, segments)

		if !viaAlias && !overrideCommentsMatch(e.HeadComment, e.LineComment, effHead, effLine) {
			return nil, false
		}

		if scalarsEquivalent(effective, e.Value, e.Type) {
			continue
		}

		if e.Type == TypeNull || viaAlias {
			return nil, false
		}

		valNode := findNodeSubtree(root, e.Key)
		if valNode == nil || valNode.Kind != yaml.ScalarNode || valNode.Style != 0 {
			return nil, false
		}

		if !isYAMLPlainSafe(e.Value) {
			return nil, false
		}

		edits = append(edits, ScalarEdit{FlatKey: e.Key, NewValue: e.Value})
	}

	return edits, true
}

// docsMatchSource reports whether docs equals the doc-level orphan comments
// currently in raw (banner, trailer, per-leaf foots) and that no section-head
// edit has been recorded. SectionHeads is a one-way edit flag: parseOrphanComments
// never populates it, so any non-empty map means a section head was touched
// and the encoder must run to place the change.
func docsMatchSource(raw []byte, docs DocComments) bool {
	if len(docs.SectionHeads) > 0 {
		return false
	}

	oc, err := parseOrphanComments(raw)
	if err != nil {
		return false
	}

	if docs.Head != oc.DocHead || docs.Foot != oc.DocFoot {
		return false
	}

	if len(docs.Foots) != len(oc.Foots) {
		return false
	}

	for k, v := range docs.Foots {
		if oc.Foots[k] != v {
			return false
		}
	}

	return true
}

// PlanScalarSpliceForTest exposes planScalarSplice to cross-package tests.
// Production callers go through PatchSourceText.
func PlanScalarSpliceForTest(
	raw []byte, root *yaml.Node, entries []OverrideEntry, docs DocComments,
) ([]ScalarEdit, bool) {
	return planScalarSplice(raw, root, entries, docs)
}
