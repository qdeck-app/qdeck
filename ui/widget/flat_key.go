package widget

import "strings"

// lastSegment returns the portion after the last dot in a dot-separated flat
// key. Used by the override table to derive the leaf label shown in the key
// column.
func lastSegment(key string) string {
	idx := strings.LastIndexByte(key, '.')

	if idx < 0 {
		return key
	}

	return key[idx+1:]
}

// parentPath returns the portion before the last dot, or "" for root-level
// keys. Unlike domain.FlatKey.Parent this splits only on '.', which matches
// what the override table uses for grouping rows by their enclosing mapping
// (list-header levels aren't shown as separate rows there).
func parentPath(key string) string {
	idx := strings.LastIndexByte(key, '.')

	if idx < 0 {
		return ""
	}

	return key[:idx]
}
