package service

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// PatchSourceText is the byte-faithful save entrypoint. It prefers leaf-line
// splicing — for value-only edits to plain scalars, the output differs from
// raw by exactly one line per edit, with everything else byte-identical.
// Falls back to the encoder + PreserveSourceFormatting path when splice
// preconditions don't hold for any entry.
func PatchSourceText(
	raw []byte,
	root *yaml.Node,
	entries []OverrideEntry,
	indent int,
	docs DocComments,
) (string, error) {
	// Strip BOM and convert CRLF→LF first: EncodeForFile re-applies them
	// downstream based on stored encoding/line-ending labels, so leaving them
	// would double the BOM and turn \r\n into \r\r\n. yaml.v3 also reports
	// node Line/Column against the BOM-stripped, LF-normalized form, so the
	// splice's byte math lines up.
	raw = normalizeForEncodeForFile(raw)

	if root != nil && len(raw) > 0 {
		if edits, ok := planScalarSplice(raw, root, entries, docs); ok {
			if len(edits) == 0 {
				return string(raw), nil
			}

			if spliced, spliceOK := SpliceScalarValues(raw, root, edits); spliceOK {
				return string(spliced), nil
			}
		}
	}

	encoded, err := PatchNodeTree(root, entries, indent, docs)
	if err != nil {
		return "", fmt.Errorf("patch source: %w", err)
	}

	return string(PreserveSourceFormatting(raw, root, []byte(encoded))), nil
}

func normalizeForEncodeForFile(raw []byte) []byte {
	raw = bytes.TrimPrefix(raw, bomUTF8)
	if bytes.Contains(raw, []byte("\r\n")) {
		raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	}

	return raw
}

// PreserveSourceFormatting post-processes encoder output to recover source
// formatting yaml.v3 normalizes away: inter-node blank lines, the spurious
// !!merge tag on `<<:` keys, byte-verbatim block scalars and tagged
// collections, and semantically-equivalent leaves. Best-effort: each pass
// passes through on parse failure or structural divergence.
func PreserveSourceFormatting(raw []byte, origRoot *yaml.Node, encoded []byte) []byte {
	encoded = stripMergeTag(encoded)
	if len(raw) == 0 || origRoot == nil {
		return encoded
	}

	// Pass order matters: block-range substitution runs first because it
	// replaces multi-line spans, invalidating any line-map built before it.
	encoded = substituteBlockScalarRanges(raw, origRoot, encoded)
	encoded = substituteUnchangedLinesFromSource(raw, origRoot, encoded)
	encoded = insertMissingSourceLines(raw, origRoot, encoded)

	return restoreBlankLines(raw, origRoot, encoded)
}
