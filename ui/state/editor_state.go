package state

import (
	"fmt"
	"strings"

	"gioui.org/widget"
	"gopkg.in/yaml.v3"

	"github.com/qdeck-app/qdeck/service"
)

// LoadedValuesMap projects fv.Entries into a flat-key → value map used by
// collectOverrides to scope the empty-value round-trip path to keys that
// were actually present in the loaded file. Returns nil for a nil
// FlatValues, so callers can pass column state without a nil-guard.
//
// Comment-row entries have an empty Key and are skipped — the map stays a
// scalar/section-leaf index keyed by flat-key strings, matching what
// FlatValueEntry.Key carries on the UI side.
func LoadedValuesMap(fv *service.FlatValues) map[string]string {
	if fv == nil {
		return nil
	}

	out := make(map[string]string, len(fv.Entries))

	for _, e := range fv.Entries {
		if e.Key == "" {
			continue
		}

		out[e.Key] = e.Value
	}

	return out
}

// OverridesToYAML builds YAML text from non-empty override editors. When tree
// is non-nil, it is used as a template: anchors, aliases, comments, and scalar
// styles from the originally loaded file are preserved for subtrees the user
// did not edit. When tree is nil (no file loaded yet), the result is rebuilt
// from scratch via FlatEntriesToYAML.
//
// loadedValues maps flat key → value as parsed from the column's loaded file
// (nil when no file was loaded, e.g. user just typed into a fresh column).
// It scopes the round-trip of explicit empty/null scalars to keys actually
// present in the loaded file: without this, every empty/null chart default
// would leak into the saved file as an override the user never made.
// Section headers and orphan-comment rows are skipped. indent controls the
// number of spaces per nesting level in the output. docs carries doc-level
// orphan comments (banner/trailer/per-leaf foots) so they round-trip on
// save — a zero DocComments leaves the output without any banner or foot
// blocks. Returns empty string with nil error when no overrides AND no doc
// comments are present.
func OverridesToYAML(
	entries []service.FlatValueEntry,
	editors []widget.Editor,
	indent int,
	tree *yaml.Node,
	docs service.DocComments,
	loadedValues map[string]string,
) (string, error) {
	overrides := collectOverrides(entries, editors, loadedValues)
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
func collectOverrides(
	entries []service.FlatValueEntry,
	editors []widget.Editor,
	loadedValues map[string]string,
) []service.OverrideEntry {
	count := 0

	for i := range entries {
		if i >= len(editors) {
			break
		}

		if !entries[i].IsFocusable() {
			continue
		}

		if entryHasContent(StripYAMLComments(editors[i].Text()), entries[i].Key, loadedValues) {
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

		// Round-trip fast path: when the raw editor text matches what
		// the load step would have written (head comment block followed
		// by the loaded value), the user hasn't edited the cell — use
		// entry.Comment / entry.Value verbatim. This is essential for
		// multi-line literal-block values whose own content starts with
		// `#` (e.g. `configuration: |` carrying a redis.conf comment),
		// where StripYAMLComments would otherwise eat the literal `#`
		// line as if it were a YAML head comment.
		loaded, hasLoaded := loadedValues[entry.Key]
		if hasLoaded && raw == loadFormForEditor(entry.Comment, loaded) {
			if !entryHasContent(loaded, entry.Key, loadedValues) {
				// Mirrors entryHasContent's "drop empty cell with no
				// loaded content" rule. The fast path can't bypass it.
				continue
			}

			result = append(result, service.OverrideEntry{
				Key:         entry.Key,
				Value:       loaded,
				Type:        entry.Type,
				HeadComment: entry.Comment,
			})

			continue
		}

		val := StripYAMLComments(raw)
		if !entry.IsFocusable() || !entryHasContent(val, entry.Key, loadedValues) {
			continue
		}

		// SplitInlineComment is ambiguous when the value itself contains
		// " #" (e.g. a quoted YAML scalar like `"s3cr3t # not really a
		// comment"`). When the editor text matches the value as parsed from
		// the loaded file, the user hasn't edited the cell and we use the
		// full text as the value (no split). Splitting in that case would
		// mis-cut the literal '#' out of the value, break the surrounding
		// YAML alias on save, and produce a phantom inline comment.
		var (
			cleanVal string
			inline   string
		)

		if hasLoaded && val == loaded {
			cleanVal = val
		} else {
			cleanVal, inline = SplitInlineComment(val)
		}

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

// loadFormForEditor reconstructs the exact text the load step writes into an
// editor cell for (comment, value): each comment line prefixed `# ` and
// terminated by `\n`, then value verbatim. Mirror of
// ui/page.formatCommentForEditor — duplicated here so collectOverrides can
// detect "user hasn't edited this cell" without crossing the page→state
// boundary. Empty comment yields just value (no leading `# ...\n`).
func loadFormForEditor(comment, value string) string {
	if comment == "" {
		return value
	}

	var b strings.Builder

	b.Grow(len(comment) + len(value) + commentLineOverheadGuess)

	for _, line := range strings.Split(comment, "\n") {
		b.WriteString("# ")
		b.WriteString(line)
		b.WriteByte('\n')
	}

	b.WriteString(value)

	return b.String()
}

// commentLineOverheadGuess is the per-line overhead Builder.Grow reserves —
// "# " plus newline. Tuned for the typical single-line comment block.
const commentLineOverheadGuess = 4

// entryHasContent reports whether collectOverrides should treat the cell as a
// real entry to round-trip on save.
//
// A non-empty stripped editor is always content. An empty stripped editor is
// only treated as content when the key is present in loadedValues with an
// empty loaded value — i.e. the source file had this key explicitly set to
// `""` / `null` / `~`. Without this round-trip path, PatchNodeTree's
// deletion phase drops the leaf, even though the source file had it.
//
// All other empty-editor cases — chart defaults the loaded file doesn't
// override, unfilled cells in a fresh column, user-cleared overrides —
// return false and the entry is dropped.
func entryHasContent(strippedEditor, key string, loadedValues map[string]string) bool {
	if strippedEditor != "" {
		return true
	}

	loaded, ok := loadedValues[key]

	return ok && loaded == ""
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
