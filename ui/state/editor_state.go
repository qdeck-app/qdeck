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
// Only entries with non-empty editor text are included. Section headers
// (map/list) are skipped. indent controls the number of spaces per nesting
// level in the output. Returns empty string with nil error when no overrides
// are present.
func OverridesToYAML(
	entries []service.FlatValueEntry, editors []widget.Editor, indent int, tree *yaml.Node,
) (string, error) {
	overrides := collectOverrides(entries, editors)
	if len(overrides) == 0 {
		return "", nil
	}

	yamlText, err := service.PatchNodeTree(tree, overrides, indent)
	if err != nil {
		return "", fmt.Errorf("overrides to YAML: %w", err)
	}

	return yamlText, nil
}

// collectOverrides gathers non-empty, non-section editor values into OverrideEntry slice.
// Leading YAML comment lines (# ...) in editors are stripped before extracting values.
func collectOverrides(entries []service.FlatValueEntry, editors []widget.Editor) []service.OverrideEntry {
	count := 0

	for i := range entries {
		if i >= len(editors) {
			break
		}

		if StripYAMLComments(editors[i].Text()) != "" && !entries[i].IsSection() {
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

		val := StripYAMLComments(editors[i].Text())
		if val == "" || entry.IsSection() {
			continue
		}

		result = append(result, service.OverrideEntry{
			Key:   entry.Key,
			Value: val,
			Type:  entry.Type,
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
