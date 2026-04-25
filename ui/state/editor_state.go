package state

import (
	"fmt"
	"strings"

	"gioui.org/widget"
	"gopkg.in/yaml.v3"

	"github.com/qdeck-app/qdeck/service"
)

// OverridesToYAML builds YAML text from non-empty override editors. When tree
// is non-nil, it is used as a template: anchors, aliases, comments, and scalar
// styles from the originally loaded file are preserved for subtrees the user
// did not edit. When tree is nil (no file loaded yet), the result is rebuilt
// from scratch via FlatEntriesToYAML.
//
// Only entries with non-empty editor text are included. Section headers and
// orphan-comment rows are skipped. indent controls the number of spaces per
// nesting level in the output. docs carries doc-level orphan comments
// (banner/trailer/per-leaf foots) so they round-trip on save — a zero
// DocComments leaves the output without any banner or foot blocks. Returns
// empty string with nil error when no overrides AND no doc comments are present.
func OverridesToYAML(
	entries []service.FlatValueEntry,
	editors []widget.Editor,
	indent int,
	tree *yaml.Node,
	docs service.DocComments,
) (string, error) {
	overrides := collectOverrides(entries, editors)
	if len(overrides) == 0 && docs.Head == "" && docs.Foot == "" && len(docs.Foots) == 0 {
		return "", nil
	}

	yamlText, err := service.PatchNodeTree(tree, overrides, indent, docs)
	if err != nil {
		return "", fmt.Errorf("overrides to YAML: %w", err)
	}

	return yamlText, nil
}

// collectOverrides gathers non-empty, non-section editor values into OverrideEntry slice.
// Editor text may contain leading "# ..." lines (block comment) and/or a
// trailing " # ..." inline comment on a single-line value. Both are extracted
// into separate OverrideEntry fields so the serializer can write the block
// above the key and the inline comment next to the value, matching the style
// the user typed.
func collectOverrides(entries []service.FlatValueEntry, editors []widget.Editor) []service.OverrideEntry {
	count := 0

	for i := range entries {
		if i >= len(editors) {
			break
		}

		if StripYAMLComments(editors[i].Text()) != "" && entries[i].IsFocusable() {
			count++
		}
	}

	if count == 0 {
		return nil
	}

	result := make([]service.OverrideEntry, 0, count)

	for i, entry := range entries {
		if i >= len(editors) {
			break
		}

		raw := editors[i].Text()

		val := StripYAMLComments(raw)
		if val == "" || !entry.IsFocusable() {
			continue
		}

		cleanVal, inline := SplitInlineComment(val)

		result = append(result, service.OverrideEntry{
			Key:         entry.Key,
			Value:       cleanVal,
			Type:        entry.Type,
			HeadComment: ExtractLeadingComment(raw),
			LineComment: inline,
		})
	}

	return result
}

// StripYAMLComments removes leading lines starting with # from editor text,
// returning only the value portion. If the text contains only comment lines,
// an empty string is returned.
func StripYAMLComments(text string) string {
	if !strings.Contains(text, "#") {
		return text
	}

	lines := strings.Split(text, "\n")
	start := 0

	for start < len(lines) {
		trimmed := strings.TrimSpace(lines[start])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			start++

			continue
		}

		break
	}

	if start == 0 {
		return text
	}

	return strings.Join(lines[start:], "\n")
}

// SplitInlineComment peels a trailing " # ..." comment off a single-line
// scalar value, returning (cleanValue, cleanedInlineComment). Multi-line
// values (e.g. flow maps / block scalars rendered as YAML) are returned
// unchanged — locating a safe split point inside nested YAML is out of scope
// and the load path anyway lifts inline comments in sub-trees during parsing.
// Quote-aware for simple '…' and "…" runs so a "#" inside a quoted literal is
// not mis-identified as an inline comment start.
func SplitInlineComment(value string) (string, string) {
	if strings.Contains(value, "\n") {
		return value, ""
	}

	var quote byte

	for i := 0; i < len(value); i++ {
		c := value[i]

		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case (c == ' ' || c == '\t') && i+1 < len(value) && value[i+1] == '#':
			cleanVal := strings.TrimRight(value[:i], " \t")
			comment := strings.TrimSpace(strings.TrimLeft(value[i+1:], "#"))

			return cleanVal, comment
		}
	}

	return value, ""
}

// ExtractLeadingComment pulls the leading "# ..." comment block off editor
// text and returns the cleaned text joined with newlines — no "# " prefix,
// blank lines dropped. Returns "" when the text starts with a non-comment
// non-blank line. Mirrors the parsing rules StripYAMLComments uses to bound
// the block so the two stay in sync.
func ExtractLeadingComment(text string) string {
	if !strings.Contains(text, "#") {
		return ""
	}

	var b strings.Builder

	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if !strings.HasPrefix(trimmed, "#") {
			break
		}

		cleaned := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))

		if b.Len() > 0 {
			b.WriteByte('\n')
		}

		b.WriteString(cleaned)
	}

	return b.String()
}
