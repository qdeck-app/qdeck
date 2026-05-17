package service

import (
	"bytes"
	"slices"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/qdeck-app/qdeck/domain"
)

// stripMergeTag removes the explicit `!!merge` tag yaml.v3 emits before
// merge keys. Canonical YAML `<<: *anchor` carries no tag.
func stripMergeTag(encoded []byte) []byte {
	return bytes.ReplaceAll(encoded, []byte("!!merge <<:"), []byte("<<:"))
}

// restoreBlankLines re-inserts blank lines that appeared in the original
// source after specific flat keys. Blanks inside block scalars are ignored —
// they're part of the scalar value and round-trip through the encoder.
func restoreBlankLines(raw []byte, origRoot *yaml.Node, encoded []byte) []byte {
	var encDoc yaml.Node
	if err := yaml.Unmarshal(encoded, &encDoc); err != nil || len(encDoc.Content) == 0 {
		return encoded
	}

	encRoot := encDoc.Content[0]

	origLineKey := lineToFlatKey(origRoot)
	encLineKey := lineToFlatKey(encRoot)

	rawLines := bytes.Split(raw, []byte("\n"))
	blockRegions := blockScalarRegions(origRoot, raw, len(rawLines))

	runs := collectBlankRuns(rawLines, origLineKey, blockRegions)
	if len(runs) == 0 {
		return encoded
	}

	encKeyLine := make(map[string]int, len(encLineKey))
	for line, key := range encLineKey {
		encKeyLine[key] = line
	}

	// Aggregate by encoded-line: a key that appears twice after deduplication
	// shouldn't blow up.
	inserts := make(map[int]int)

	for _, r := range runs {
		if line, ok := encKeyLine[r.afterKey]; ok {
			inserts[line] += r.count
		}
	}

	if len(inserts) == 0 {
		return encoded
	}

	encLines := bytes.Split(encoded, []byte("\n"))

	totalBlanks := 0
	for _, c := range inserts {
		totalBlanks += c
	}

	var out bytes.Buffer

	out.Grow(len(encoded) + totalBlanks)

	for i, line := range encLines {
		out.Write(line)

		if i < len(encLines)-1 {
			out.WriteByte('\n')
		}

		if c := inserts[i+1]; c > 0 {
			for range c {
				out.WriteByte('\n')
			}
		}
	}

	return out.Bytes()
}

type blankRun struct {
	afterKey string
	count    int
}

// collectBlankRuns scans rawLines for blank-line runs outside block-scalar
// regions, attributing each run to the most recent keyed line. Comment-only
// lines don't update the anchor — comments re-emit as HeadComment on the
// next key, so blanks belong with the prior key.
func collectBlankRuns(
	rawLines [][]byte,
	lineKey map[int]string,
	blockRegions [][2]int,
) []blankRun {
	var (
		runs        []blankRun
		lastKey     string
		blanks      int
		keyAdjacent bool
	)

	for i, line := range rawLines {
		lineNo := i + 1

		if inSortedRegion(lineNo, blockRegions) {
			// substituteBlockScalarRanges owns these bytes; don't generate runs.
			blanks = 0

			if k, ok := lineKey[lineNo]; ok {
				lastKey = k
				keyAdjacent = true
			}

			continue
		}

		if isBlankBytes(line) {
			blanks++

			continue
		}

		if blanks > 0 {
			// Only attribute blanks when no intervening non-key lines (top-level
			// comment blocks become HeadComment on a future node — re-inserting
			// after the prior key lands them in the wrong section).
			if lastKey != "" && keyAdjacent {
				runs = append(runs, blankRun{afterKey: lastKey, count: blanks})
			}

			blanks = 0
		}

		if k, ok := lineKey[lineNo]; ok {
			lastKey = k
			keyAdjacent = true
		} else {
			keyAdjacent = false
		}
	}

	return runs
}

// lineToFlatKey maps 1-indexed source line numbers to the flat key whose
// declaration starts on that line. Keys are encoded via domain.EscapeSegment
// so literal '.' or '[' inside a map key doesn't collide with path separators.
func lineToFlatKey(root *yaml.Node) map[int]string {
	if root == nil {
		return nil
	}

	out := make(map[int]string)

	var walk func(node *yaml.Node, path string)

	walk = func(node *yaml.Node, path string) {
		switch node.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				key := node.Content[i]
				val := node.Content[i+1]
				child := joinFlatKey(path, domain.EscapeSegment(key.Value))

				if key.Line > 0 {
					out[key.Line] = child
				}

				walk(val, child)
			}
		case yaml.SequenceNode:
			for i, item := range node.Content {
				child := path + "[" + strconv.Itoa(i) + "]"

				if item.Line > 0 {
					out[item.Line] = child
				}

				walk(item, child)
			}
		case yaml.DocumentNode:
			for _, c := range node.Content {
				walk(c, path)
			}
		}
	}

	walk(root, "")

	return out
}

// refineBlockScalarEnd narrows a sibling-heuristic block end via the YAML
// spec rule: the block extends while a line is blank or its indentation is
// >= the content column; it ends at the first non-blank line with smaller
// indentation.
func refineBlockScalarEnd(rawLines [][]byte, startLine, siblingEnd int) int {
	if startLine < 1 || startLine > len(rawLines) {
		return siblingEnd
	}

	contentCol := -1

	for i := startLine - 1; i < len(rawLines) && i < siblingEnd; i++ {
		col := lineIndentColumn(rawLines[i])
		if col >= 0 {
			contentCol = col

			break
		}
	}

	if contentCol < 0 {
		return siblingEnd
	}

	last := startLine - 1

	for i := startLine - 1; i < len(rawLines) && i < siblingEnd; i++ {
		line := rawLines[i]
		col := lineIndentColumn(line)

		if col < 0 {
			// Tentatively include blanks; trailing-blank clipping handles them.
			last = i + 1

			continue
		}

		if col < contentCol {
			break
		}

		last = i + 1
	}

	return last
}

// lineIndentColumn returns the 0-indexed column of the first non-whitespace
// byte, or -1 when the line is blank. Tabs count as 1 (yaml.v3 forbids tabs
// in indentation).
func lineIndentColumn(line []byte) int {
	for i, c := range line {
		if c != ' ' && c != '\t' && c != '\r' {
			return i
		}
	}

	return -1
}

// blockScalarRegions returns sorted inclusive line ranges covering every
// block scalar and tagged collection body. Used to exclude blanks inside
// these regions from blank-run detection — substituteBlockScalarRanges
// emits them source-verbatim.
func blockScalarRegions(root *yaml.Node, raw []byte, totalLines int) [][2]int {
	if root == nil {
		return nil
	}

	type entry struct {
		line int
		node *yaml.Node
	}

	var (
		blocks    []*yaml.Node
		positions []entry
	)

	var walk func(n *yaml.Node)

	walk = func(n *yaml.Node) {
		if n == nil {
			return
		}

		if n.Line > 0 {
			positions = append(positions, entry{n.Line, n})
		}

		if isBlockScalarNode(n) || isTaggedCollectionNode(n) {
			blocks = append(blocks, n)
		}

		for _, c := range n.Content {
			walk(c)
		}
	}

	walk(root)

	slices.SortFunc(positions, func(a, b entry) int { return a.line - b.line })

	var rawLines [][]byte
	if len(raw) > 0 {
		rawLines = bytes.Split(raw, []byte("\n"))
	}

	regions := make([][2]int, 0, len(blocks))

	for _, b := range blocks {
		startLine := b.Line + 1

		// Sibling-heuristic cap valid only for block SCALARS: tagged
		// collections have children with their own Line that would mis-cap
		// the region. Rely on refineBlockScalarEnd's indent scan for those.
		endLine := totalLines

		if isBlockScalarNode(b) {
			for _, p := range positions {
				if p.line > b.Line && p.node != b {
					endLine = p.line - 1

					break
				}
			}
		}

		if rawLines != nil {
			endLine = refineBlockScalarEnd(rawLines, startLine, endLine)
		}

		if endLine >= startLine {
			regions = append(regions, [2]int{startLine, endLine})
		}
	}

	slices.SortFunc(regions, func(a, b [2]int) int { return a[0] - b[0] })

	return regions
}

// inSortedRegion reports whether line falls within any [start,end] range.
// regions must be sorted by start.
func inSortedRegion(line int, regions [][2]int) bool {
	for _, r := range regions {
		if r[0] > line {
			break
		}

		if line <= r[1] {
			return true
		}
	}

	return false
}

// isBlankBytes reports whether b contains only ASCII whitespace. Trailing CR
// is honored so CRLF files don't read as non-blank.
func isBlankBytes(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\r' {
			return false
		}
	}

	return true
}
