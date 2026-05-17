package service

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// yaml.v3 default collection tags. Both the `!!short` and full URI
// spellings can appear in node.Tag depending on whether the source
// used an explicit tag directive.
const (
	tagMap    = "!!map"
	tagSeq    = "!!seq"
	tagMapURI = "tag:yaml.org,2002:map"
	tagSeqURI = "tag:yaml.org,2002:seq"
)

// isBlockScalarNode reports whether node is a scalar with a literal `|` or
// folded `>` style. Style is a bit-field — check via bitmask so combined
// styles (e.g. TaggedStyle | LiteralStyle) still match.
func isBlockScalarNode(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.ScalarNode {
		return false
	}

	return node.Style&yaml.LiteralStyle != 0 || node.Style&yaml.FoldedStyle != 0
}

// isTaggedCollectionNode reports whether node is a mapping or sequence with a
// non-default tag (`!!set`, `!!omap`, user-defined tags). yaml.v3's emitter
// doesn't preserve presentation quirks (like `? key` for `!!set` members), so
// these classes round-trip safely only via byte-substitution.
func isTaggedCollectionNode(node *yaml.Node) bool {
	if node == nil || node.Tag == "" {
		return false
	}

	switch node.Kind {
	case yaml.MappingNode:
		return node.Tag != tagMap && node.Tag != tagMapURI
	case yaml.SequenceNode:
		return node.Tag != tagSeq && node.Tag != tagSeqURI
	}

	return false
}

func isSingleLineNode(node *yaml.Node) bool {
	if node == nil {
		return false
	}

	switch node.Kind {
	case yaml.ScalarNode:
		return !isBlockScalarNode(node)
	case yaml.MappingNode:
		return len(node.Content) == 0
	case yaml.SequenceNode:
		return len(node.Content) == 0
	case yaml.AliasNode:
		return true
	}

	return false
}

// blockScalarsEquivalent reports whether two substitutable nodes (block
// scalars or tagged collections) carry the same logical content. Style is
// intentionally NOT compared — the encoder may add the TaggedStyle bit-flag
// to a source that only had LiteralStyle.
func blockScalarsEquivalent(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return false
	}

	if a.Kind != b.Kind || a.Tag != b.Tag {
		return false
	}

	if a.Kind == yaml.ScalarNode {
		// Trim leading/trailing newlines: yaml.v3 can shift them when re-emitting
		// depending on the chomp indicator; the substituted bytes carry the
		// original whitespace shape.
		return strings.Trim(a.Value, "\n") == strings.Trim(b.Value, "\n")
	}

	return nodesStructurallyEqual(a, b)
}

// nodesStructurallyEqual reports whether two yaml.Nodes have identical content
// trees (Kind, Tag, Value, recursive Content). Style and position are not
// compared. AliasNode equality is approximated by comparing the resolved
// Anchor — a true alias-graph walk would need cycle detection.
func nodesStructurallyEqual(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}

	if a.Kind != b.Kind || a.Tag != b.Tag || a.Value != b.Value {
		return false
	}

	if a.Kind == yaml.AliasNode {
		if a.Alias == nil || b.Alias == nil {
			return a.Alias == b.Alias
		}

		return a.Alias.Anchor == b.Alias.Anchor
	}

	if len(a.Content) != len(b.Content) {
		return false
	}

	for i := range a.Content {
		if !nodesStructurallyEqual(a.Content[i], b.Content[i]) {
			return false
		}
	}

	return true
}

// linesSemanticallyMatch reports whether two YAML lines describe the same
// key/value/inline-comment, ignoring whitespace differences. Skips multi-line
// scalars (literal `|` / folded `>`) — the line-level check can't see their
// body. Also widens for the asymmetric case where the encoder dropped an
// inline comment the source carried (most common on null-tagged leaves like
// `key: ~ # comment`).
func linesSemanticallyMatch(srcLine, encLine []byte) bool {
	srcKV, srcCmt := splitKeyValueAndComment(srcLine)
	encKV, encCmt := splitKeyValueAndComment(encLine)

	srcKVStr := strings.TrimRight(string(srcKV), " \t")
	encKVStr := strings.TrimRight(string(encKV), " \t")

	if srcKVStr != encKVStr {
		return false
	}

	if strings.HasSuffix(srcKVStr, ": |") || strings.HasSuffix(srcKVStr, ": >") ||
		strings.HasSuffix(srcKVStr, ":|") || strings.HasSuffix(srcKVStr, ":>") ||
		strings.Contains(srcKVStr, ": |-") || strings.Contains(srcKVStr, ": >-") {
		return false
	}

	srcCmtTrim := strings.TrimSpace(string(srcCmt))
	encCmtTrim := strings.TrimSpace(string(encCmt))

	if srcCmtTrim == encCmtTrim {
		return true
	}

	return encCmtTrim == "" && srcCmtTrim != ""
}

// splitKeyValueAndComment partitions a YAML line into its key:value portion
// and its inline `# comment` portion. Quote-aware so a `#` inside a quoted
// scalar doesn't get treated as a comment marker.
func splitKeyValueAndComment(line []byte) (kv, comment []byte) {
	var quote byte

	for i := 0; i < len(line); i++ {
		c := line[i]

		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case (c == ' ' || c == '\t') && i+1 < len(line) && line[i+1] == '#':
			return line[:i], line[i+1:]
		}
	}

	return line, nil
}
