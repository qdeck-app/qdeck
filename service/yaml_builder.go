package service

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	yamlIndent  = 2
	maxKeyIndex = 10000
)

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
// Returns empty string when entries is empty.
func FlatEntriesToYAML(entries []OverrideEntry) (string, error) {
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
	enc.SetIndent(yamlIndent)

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
