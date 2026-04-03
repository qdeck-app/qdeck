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

// Parent returns the parent key, or empty if at root.
func (k FlatKey) Parent() FlatKey {
	s := string(k)

	idx := strings.LastIndexByte(s, '.')
	if idx < 0 {
		return ""
	}

	return FlatKey(s[:idx])
}

// LastSegment returns the final segment of the key.
func (k FlatKey) LastSegment() string {
	s := string(k)

	idx := strings.LastIndexByte(s, '.')
	if idx < 0 {
		return s
	}

	return s[idx+1:]
}
