package widget

import "github.com/qdeck-app/qdeck/domain"

// lastSegment returns the portion after the last unescaped dot in a flat key,
// decoded back to its literal form. Used by the override table to derive the
// leaf label shown in the key column.
func lastSegment(key string) string {
	return domain.FlatKey(key).LastSegment()
}

// parentPath returns the portion before the last unescaped dot, or "" for
// root-level keys. Unlike domain.FlatKey.Parent this splits only on '.', not
// on '[', which matches what the override table uses for grouping rows by
// their enclosing mapping (list-header levels aren't shown as separate rows
// there).
func parentPath(key string) string {
	return string(domain.FlatKey(key).ParentMapPath())
}
