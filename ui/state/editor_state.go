package state

import (
	"fmt"

	"gioui.org/widget"

	"github.com/qdeck-app/qdeck/service"
)

// OverridesToYAML builds nested YAML from non-empty override editors.
// Only entries with non-empty editor text are included.
// Section headers (map/list) are skipped.
// Returns empty string with nil error when no overrides are present.
func OverridesToYAML(entries []service.FlatValueEntry, editors []widget.Editor) (string, error) {
	overrides := collectOverrides(entries, editors)
	if len(overrides) == 0 {
		return "", nil
	}

	yamlText, err := service.FlatEntriesToYAML(overrides)
	if err != nil {
		return "", fmt.Errorf("overrides to YAML: %w", err)
	}

	return yamlText, nil
}

// collectOverrides gathers non-empty, non-section editor values into OverrideEntry slice.
func collectOverrides(entries []service.FlatValueEntry, editors []widget.Editor) []service.OverrideEntry {
	count := 0

	for i := range entries {
		if i >= len(editors) {
			break
		}

		if editors[i].Text() != "" && !entries[i].IsSection() {
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

		val := editors[i].Text()
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
