// Package domain ...
//
// # FlatKey encoding
//
// A FlatKey is a dot-separated path into a YAML document where map keys live
// between '.' separators and sequence indices live in '[i]' suffixes. Three
// characters are special inside a map-key segment and must be backslash-escaped
// to round-trip safely:
//
//	\.   literal '.'
//	\[   literal '['
//	\\   literal '\'
//
// An empty-string map key is encoded as a zero-character segment, so the
// boundary '.' alone carries the structure (e.g. "parent." is the segment
// "parent" followed by an empty-string child key). Sequence indices ('[i]')
// are unaffected by escaping; '[' inside a map key must be escaped to avoid
// being parsed as the start of an index group.
//
// EscapeSegment / UnescapeSegment apply and reverse the encoding for one
// segment at a time. The string-walking helpers (Depth, Parent, LastSegment)
// honour escapes — they only treat unescaped '.' or '[' as boundaries.
package domain

import (
	"fmt"
	"iter"
	"strings"
)

// FlatKey represents a dot-separated path into a YAML document.
// Example: "service.ports[0].name". See package doc for the escape scheme
// covering map-key segments that contain '.', '[', '\', or are empty.
type FlatKey string

// escapeGrowSlack is the extra capacity reserved on the strings.Builder used
// by EscapeSegment to absorb a handful of escape backslashes without a
// reallocation. Tuned for typical k8s label keys (1–3 dots).
const escapeGrowSlack = 4

// EscapeSegment returns name with the three FlatKey escape sequences applied.
// Empty input maps to empty output — an empty-string map key needs no escape;
// the surrounding '.' boundaries already encode the empty segment.
func EscapeSegment(name string) string {
	if !strings.ContainsAny(name, `.[\`) {
		return name
	}

	var b strings.Builder

	b.Grow(len(name) + escapeGrowSlack)

	for i := 0; i < len(name); i++ {
		c := name[i]
		if c == '.' || c == '[' || c == '\\' {
			b.WriteByte('\\')
		}

		b.WriteByte(c)
	}

	return b.String()
}

// UnescapeSegment reverses EscapeSegment. Returns an error when the input
// contains a dangling '\' or an unrecognised escape (anything other than
// \., \[, or \\).
func UnescapeSegment(seg string) (string, error) {
	if !strings.ContainsRune(seg, '\\') {
		return seg, nil
	}

	var b strings.Builder

	b.Grow(len(seg))

	for i := 0; i < len(seg); i++ {
		c := seg[i]
		if c != '\\' {
			b.WriteByte(c)

			continue
		}

		if i+1 >= len(seg) {
			return "", fmt.Errorf("dangling backslash at end of segment %q", seg)
		}

		next := seg[i+1]
		if next != '.' && next != '[' && next != '\\' {
			return "", fmt.Errorf("invalid escape \\%c in segment %q", next, seg)
		}

		b.WriteByte(next)

		i++
	}

	return b.String(), nil
}

// MapSegments returns the dot-separated segments of the key, decoded back to
// their literal form. Sequence indices stay attached to their preceding map
// segment (e.g. "foo.bar[0]" → ["foo", "bar[0]"]). Returns nil for the empty
// key. Used by display code that needs the human-readable path components;
// not for tree navigation, which goes through parseKeySegments.
//
// On a malformed escape inside a segment that segment is returned encoded
// (no error) so display paths don't have to handle errors.
func (k FlatKey) MapSegments() []string {
	if k == "" {
		return nil
	}

	parts := SplitEncoded(string(k))
	out := make([]string, len(parts))

	for i, p := range parts {
		decoded, err := UnescapeSegment(p)
		if err != nil {
			out[i] = p

			continue
		}

		out[i] = decoded
	}

	return out
}

// unescapedBytes yields (index, byte) pairs of s that are not part of an
// escape sequence. Backslash-escape pairs `\X` are silently consumed —
// neither byte is yielded — so callers see only "real" boundary candidates.
// A trailing unmatched `\` is yielded as itself. Shared by every
// escape-aware walker so the skip-pair logic lives in one place.
func unescapedBytes(s string) iter.Seq2[int, byte] {
	return func(yield func(int, byte) bool) {
		for i := 0; i < len(s); {
			c := s[i]
			if c == '\\' && i+1 < len(s) {
				i += 2

				continue
			}

			if !yield(i, c) {
				return
			}

			i++
		}
	}
}

// SplitEncoded splits s on unescaped '.' boundaries, returning the parts in
// their still-encoded form (callers unescape per segment). A trailing '.'
// produces a final empty part; consecutive ".." produce an empty middle part.
// Used by MapSegments and by the service-layer flat-key parser.
func SplitEncoded(s string) []string {
	if s == "" {
		return nil
	}

	out := make([]string, 0, strings.Count(s, ".")+1)
	start := 0

	for i, c := range unescapedBytes(s) {
		if c == '.' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}

	out = append(out, s[start:])

	return out
}

// IndexUnescaped returns the index of the first unescaped occurrence of b in
// s, or -1 when none exists. Used by the service-layer parser to find the
// first real '[' (start of an index group) inside an already-split flat-key
// segment without misreading an escaped literal `\[`.
func IndexUnescaped(s string, b byte) int {
	for i, c := range unescapedBytes(s) {
		if c == b {
			return i
		}
	}

	return -1
}

// Depth returns the nesting depth measured by unescaped '.' separators.
// Empty key returns 0; a key with no separators returns 1.
func (k FlatKey) Depth() int {
	if k == "" {
		return 0
	}

	count := 1

	for _, c := range unescapedBytes(string(k)) {
		if c == '.' {
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
//
// Honours backslash escapes — an escaped '\.' or '\[' inside a segment is
// not treated as a boundary.
func (k FlatKey) Parent() FlatKey {
	idx := lastBoundary(string(k))
	if idx < 0 {
		return ""
	}

	return k[:idx]
}

// ParentMapPath returns the prefix before the last unescaped '.', or "" if
// none exists. Unlike Parent it ignores '[' boundaries — used by UI code
// that groups rows by their enclosing mapping and treats list-header levels
// as part of the leaf, not a separate parent row.
func (k FlatKey) ParentMapPath() FlatKey {
	idx := lastUnescapedDot(string(k))
	if idx < 0 {
		return ""
	}

	return k[:idx]
}

// LastSegment returns the final segment of the key, decoded back to its
// literal form (escapes removed).
//
// Note: unlike Parent this splits only on '.'; it does NOT treat "[i]" as a
// separator, so LastSegment("foo.bar[0]") is "bar[0]" (the list field plus
// its index), not "[0]". That matches what UI code wants to display as the
// leaf label and is why the two helpers intentionally diverge. If you need
// the index-only form, strip it explicitly from the result.
//
// On a malformed escape the encoded tail is returned unchanged so callers
// don't have to plumb error handling through display paths.
func (k FlatKey) LastSegment() string {
	s := lastSegmentRaw(string(k))

	decoded, err := UnescapeSegment(s)
	if err != nil {
		return s
	}

	return decoded
}

// lastSegmentRaw returns the tail after the last unescaped '.', preserving
// any escape sequences. Used internally where the caller wants to compare
// against another encoded form.
func lastSegmentRaw(s string) string {
	idx := lastUnescapedDot(s)
	if idx < 0 {
		return s
	}

	return s[idx+1:]
}

// lastBoundary returns the byte index of the last unescaped '.' or '[' in s,
// or -1 when none exists. The boundary char itself is at the returned index;
// callers slice with s[:idx] to drop it.
func lastBoundary(s string) int {
	last := -1

	for i, c := range unescapedBytes(s) {
		if c == '.' || c == '[' {
			last = i
		}
	}

	return last
}

// lastUnescapedDot returns the byte index of the last unescaped '.' in s,
// or -1. Used by LastSegment, which does not treat '[' as a boundary.
func lastUnescapedDot(s string) int {
	last := -1

	for i, c := range unescapedBytes(s) {
		if c == '.' {
			last = i
		}
	}

	return last
}
