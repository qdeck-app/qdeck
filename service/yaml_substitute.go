package service

import (
	"bytes"
	"slices"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/qdeck-app/qdeck/domain"
)

// substituteBlockScalarRanges replaces encoded byte ranges for block scalars
// and tagged collections with the source's verbatim bytes when the content is
// structurally unchanged. A value/structure mismatch keeps the encoded form
// so user edits survive.
func substituteBlockScalarRanges(raw []byte, origRoot *yaml.Node, encoded []byte) []byte {
	if origRoot == nil || len(raw) == 0 {
		return encoded
	}

	var encDoc yaml.Node
	if err := yaml.Unmarshal(encoded, &encDoc); err != nil || len(encDoc.Content) == 0 {
		return encoded
	}

	encRoot := encDoc.Content[0]

	rawLines := bytes.Split(raw, []byte("\n"))
	encLines := bytes.Split(encoded, []byte("\n"))

	origBlocks := collectBlockScalarEntries(origRoot, rawLines)
	encBlocks := collectBlockScalarEntries(encRoot, encLines)

	type substitution struct {
		encStart, encEnd int
		srcStart, srcEnd int
	}

	var subs []substitution

	for flatKey, encInfo := range encBlocks {
		origInfo, ok := origBlocks[flatKey]
		if !ok {
			continue
		}

		if !blockScalarsEquivalent(origInfo.node, encInfo.node) {
			continue
		}

		subs = append(subs, substitution{
			encStart: encInfo.startLine,
			encEnd:   encInfo.endLine,
			srcStart: origInfo.startLine,
			srcEnd:   origInfo.endLine,
		})
	}

	if len(subs) == 0 {
		return encoded
	}

	slices.SortFunc(subs, func(a, b substitution) int { return a.encStart - b.encStart })

	out := make([][]byte, 0, len(encLines))

	cursor := 1

	for _, s := range subs {
		for i := cursor; i < s.encStart; i++ {
			out = append(out, encLines[i-1])
		}

		for i := s.srcStart; i <= s.srcEnd; i++ {
			if i >= 1 && i <= len(rawLines) {
				out = append(out, rawLines[i-1])
			}
		}

		cursor = s.encEnd + 1
	}

	for i := cursor; i <= len(encLines); i++ {
		out = append(out, encLines[i-1])
	}

	return bytes.Join(out, []byte("\n"))
}

type blockScalarEntry struct {
	startLine int
	endLine   int
	node      *yaml.Node
}

// collectBlockScalarEntries maps each flat key whose value is substitutable
// (block scalar or tagged collection) to its source line range. Recursion
// stops at substitutable nodes — the parent owns the whole byte range.
func collectBlockScalarEntries(root *yaml.Node, lines [][]byte) map[string]blockScalarEntry {
	out := make(map[string]blockScalarEntry)

	var walk func(node *yaml.Node, path string)

	walk = func(node *yaml.Node, path string) {
		if node == nil {
			return
		}

		switch node.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(node.Content); i += 2 {
				key := node.Content[i]
				val := node.Content[i+1]
				child := joinFlatKey(path, domain.EscapeSegment(key.Value))

				if isBlockScalarNode(val) || isTaggedCollectionNode(val) {
					bodyStart := val.Line + 1
					if val.Line <= key.Line {
						bodyStart = key.Line + 1
					}

					out[child] = blockScalarEntry{
						startLine: key.Line,
						endLine:   refineBlockScalarEnd(lines, bodyStart, len(lines)),
						node:      val,
					}

					continue
				}

				walk(val, child)
			}
		case yaml.SequenceNode:
			for i, item := range node.Content {
				child := path + "[" + strconv.Itoa(i) + "]"

				if isBlockScalarNode(item) || isTaggedCollectionNode(item) {
					out[child] = blockScalarEntry{
						startLine: item.Line,
						endLine:   refineBlockScalarEnd(lines, item.Line+1, len(lines)),
						node:      item,
					}

					continue
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

// substituteUnchangedLinesFromSource replaces each encoded line whose
// corresponding source line is semantically equivalent (same key, value, and
// inline comment modulo whitespace) with the source bytes. Recovers
// inline-comment column alignment for keys the encoder reformatted as a
// side-effect of re-emitting a different leaf.
func substituteUnchangedLinesFromSource(raw []byte, origRoot *yaml.Node, encoded []byte) []byte {
	var encDoc yaml.Node
	if err := yaml.Unmarshal(encoded, &encDoc); err != nil || len(encDoc.Content) == 0 {
		return encoded
	}

	encRoot := encDoc.Content[0]

	origLineKey := lineToFlatKey(origRoot)
	encLineKey := lineToFlatKey(encRoot)

	if len(origLineKey) == 0 || len(encLineKey) == 0 {
		return encoded
	}

	origKeyLine := make(map[string]int, len(origLineKey))
	for line, key := range origLineKey {
		origKeyLine[key] = line
	}

	rawLines := bytes.Split(raw, []byte("\n"))
	encLines := bytes.Split(encoded, []byte("\n"))

	for i := range encLines {
		encLineNo := i + 1

		key, ok := encLineKey[encLineNo]
		if !ok {
			continue
		}

		srcLineNo, ok := origKeyLine[key]
		if !ok {
			continue
		}

		if srcLineNo < 1 || srcLineNo > len(rawLines) {
			continue
		}

		srcLine := rawLines[srcLineNo-1]
		if linesSemanticallyMatch(srcLine, encLines[i]) {
			encLines[i] = srcLine
		}
	}

	return bytes.Join(encLines, []byte("\n"))
}

// insertMissingSourceLines re-inserts single-line source nodes (scalar leaves
// and empty containers) whose keys are missing from the encoded output.
// Typically recovers `key: []` / `key: {}` placeholders upstream layers
// skipped as no-op container entries.
func insertMissingSourceLines(raw []byte, origRoot *yaml.Node, encoded []byte) []byte {
	if origRoot == nil || len(raw) == 0 {
		return encoded
	}

	var encDoc yaml.Node
	if err := yaml.Unmarshal(encoded, &encDoc); err != nil || len(encDoc.Content) == 0 {
		return encoded
	}

	encRoot := encDoc.Content[0]
	origLineKey := lineToFlatKey(origRoot)
	encLineKey := lineToFlatKey(encRoot)

	encKeyPresent := make(map[string]bool, len(encLineKey))
	for _, key := range encLineKey {
		encKeyPresent[key] = true
	}

	encKeyLine := make(map[string]int, len(encLineKey))
	for line, key := range encLineKey {
		encKeyLine[key] = line
	}

	insertable := insertableSingleLineKeys(origRoot)

	sourceLines := make([]int, 0, len(origLineKey))
	for line := range origLineKey {
		sourceLines = append(sourceLines, line)
	}

	slices.Sort(sourceLines)

	// Anchor each insertion on the PREVIOUS source key that exists in encoded;
	// insert AFTER that key's line. Anchoring on the next key would land the
	// insertion across a section boundary.
	type insertion struct {
		afterEncLine int // 1-indexed; 0 means "insert at top of file"
		srcLine      int // 1-indexed source line to emit
	}

	var insertions []insertion

	for i, srcLine := range sourceLines {
		key := origLineKey[srcLine]
		if encKeyPresent[key] {
			continue
		}

		if !insertable[key] {
			continue
		}

		var prevEncLine int

		for j := i - 1; j >= 0; j-- {
			candidateKey := origLineKey[sourceLines[j]]
			if encLine, ok := encKeyLine[candidateKey]; ok {
				prevEncLine = encLine

				break
			}
		}

		insertions = append(insertions, insertion{
			afterEncLine: prevEncLine,
			srcLine:      srcLine,
		})
	}

	if len(insertions) == 0 {
		return encoded
	}

	rawLines := bytes.Split(raw, []byte("\n"))
	encLines := bytes.Split(encoded, []byte("\n"))

	buckets := make(map[int][]insertion)
	for _, ins := range insertions {
		buckets[ins.afterEncLine] = append(buckets[ins.afterEncLine], ins)
	}

	out := make([][]byte, 0, len(encLines))

	emitBucket := func(target int) {
		for _, ins := range buckets[target] {
			if ins.srcLine >= 1 && ins.srcLine <= len(rawLines) {
				out = append(out, rawLines[ins.srcLine-1])
			}
		}
	}

	emitBucket(0)

	for i, encLine := range encLines {
		encLineNo := i + 1

		out = append(out, encLine)

		emitBucket(encLineNo)
	}

	return bytes.Join(out, []byte("\n"))
}

// insertableSingleLineKeys returns flat keys whose source representation
// fits on one line — scalar leaves and empty containers.
func insertableSingleLineKeys(root *yaml.Node) map[string]bool {
	if root == nil {
		return nil
	}

	out := make(map[string]bool)

	var walk func(node *yaml.Node, path string)

	walk = func(node *yaml.Node, path string) {
		if node == nil {
			return
		}

		switch node.Kind {
		case yaml.MappingNode:
			// Flow-style mappings put all entries on one line; lineToFlatKey can
			// only record one key per line. The parent's single-line entry
			// covers the whole mapping.
			if node.Style == yaml.FlowStyle {
				return
			}

			for i := 0; i+1 < len(node.Content); i += 2 {
				key := node.Content[i]
				val := node.Content[i+1]
				child := joinFlatKey(path, domain.EscapeSegment(key.Value))

				if isSingleLineNode(val) {
					out[child] = true
				} else {
					walk(val, child)
				}
			}
		case yaml.SequenceNode:
			if node.Style == yaml.FlowStyle {
				return
			}

			for i, item := range node.Content {
				child := path + "[" + strconv.Itoa(i) + "]"

				if isSingleLineNode(item) {
					out[child] = true
				} else {
					walk(item, child)
				}
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
