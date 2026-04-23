package domain

import "strings"

// FlatKey represents a dot-separated path into a YAML document.
// Example: "service.ports[0].name"
type FlatKey string

// Depth returns the nesting depth (number of dot segments).
func (k FlatKey) Depth() int {
	if k == "" {
		return 0
	}

	count := 1

	for i := range len(string(k)) {
		if k[i] == '.' {
			count++
		}
	}

	return count
}

// Parent returns the key of the enclosing container, or empty if at root.
// Both separators matter: map fields are joined by '.' and list items by
// "[i]", so the parent of "foo.bar[0].baz" is "foo.bar[0]", and the parent
// of "foo.bar[0]" is "foo.bar" (the list header). Splitting only on '.'
// would skip the list-header level and break ancestor walks.
func (k FlatKey) Parent() FlatKey {
	s := string(k)

	dotIdx := strings.LastIndexByte(s, '.')
	bracketIdx := strings.LastIndexByte(s, '[')

	idx := max(bracketIdx, dotIdx)

	if idx < 0 {
		return ""
	}

	return FlatKey(s[:idx])
}

// LastSegment returns the final segment of the key.
//
// Note: unlike Parent this splits only on '.'; it does NOT treat "[i]" as a
// separator, so LastSegment("foo.bar[0]") is "bar[0]" (the list field plus
// its index), not "[0]". That matches what UI code wants to display as the
// leaf label and is why the two helpers intentionally diverge. If you need
// the index-only form, strip it explicitly from the result.
func (k FlatKey) LastSegment() string {
	s := string(k)

	idx := strings.LastIndexByte(s, '.')
	if idx < 0 {
		return s
	}

	return s[idx+1:]
}
