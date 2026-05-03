package service

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type commentStackItem struct {
	prefix string
	node   *yaml.Node
}

// OrphanComments holds YAML comment blocks that aren't attached to any leaf
// key/value pair, and would otherwise be silently dropped on parse and lost on
// save. Three positions are captured:
//
//   - DocHead: file-level banner — yaml.v3's DocumentNode head comment, the
//     block that sits before the first key in the source.
//   - DocFoot: file-level trailer — DocumentNode foot comment, after the last
//     key in the source.
//   - Foots: per-leaf foot comments — `valNode.FootComment` (mappings) or
//     `child.FootComment` (sequence items). Each entry maps the leaf's flat
//     key to the comment text that trails it. yaml.v3 attaches a trailing
//     block at the end of a section to the FootComment of that section's
//     last child, so container-trailers fall out of this map for free — no
//     separate case needed.
//
// All comment text is stored RAW — verbatim with "# " prefixes intact, blank
// lines (just "#") preserved — so the encode path can write them back without
// flattening multi-paragraph blocks. Use CleanCommentForDisplay to strip
// prefixes before rendering in the UI.
type OrphanComments struct {
	DocHead string
	DocFoot string
	Foots   map[string]string
}

// parseOrphanComments walks the YAML document a second time (alongside
// parseComments) to capture banner/trailer/foot comments that the leaf-keyed
// walker can't represent. Returns an empty OrphanComments (no error) when
// the input is empty or has no mapping root.
func parseOrphanComments(data []byte) (OrphanComments, error) {
	oc := OrphanComments{Foots: make(map[string]string)}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return oc, fmt.Errorf("unmarshal yaml for orphan comments: %w", err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return oc, nil
	}

	// Banner is the document head comment. Trailer is doc.FootComment when set;
	// otherwise yaml.v3 sometimes lands a final block on the root mapping's
	// FootComment instead, so fall back to that.
	oc.DocHead = doc.HeadComment

	root := doc.Content[0]

	switch {
	case doc.FootComment != "":
		oc.DocFoot = doc.FootComment
	case root != nil:
		oc.DocFoot = root.FootComment
	}

	if root == nil || root.Kind != yaml.MappingNode {
		return oc, nil
	}

	stack := []commentStackItem{{prefix: "", node: root}}

	for len(stack) > 0 {
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		walkFootComments(item, &stack, oc.Foots)
	}

	return oc, nil
}

// walkFootComments collects FootComment blocks for every direct child of
// item.node (mapping value nodes and sequence items), keyed by the child's
// flat path. Composite children are pushed onto the stack so their own
// children are visited in turn — same shape as walkCommentChildren but only
// reading FootComment, never HeadComment/LineComment.
func walkFootComments(item commentStackItem, stack *[]commentStackItem, foots map[string]string) {
	content := item.node.Content

	switch item.node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(content)-1; i += 2 {
			keyNode, valNode := content[i], content[i+1]
			if keyNode == nil || valNode == nil {
				continue
			}

			key := buildKey(item.prefix, keyNode.Value)

			// Foot text may sit on either node. Value node wins when both are
			// set since that's where yaml.v3 puts blank-line-separated trailers.
			switch {
			case valNode.FootComment != "":
				foots[key] = valNode.FootComment
			case keyNode.FootComment != "":
				foots[key] = keyNode.FootComment
			}

			if valNode.Kind == yaml.MappingNode || valNode.Kind == yaml.SequenceNode {
				*stack = append(*stack, commentStackItem{prefix: key, node: valNode})
			}
		}
	case yaml.SequenceNode:
		for i, child := range content {
			if child == nil {
				continue
			}

			key := item.prefix + "[" + strconv.Itoa(i) + "]"

			if child.FootComment != "" {
				foots[key] = child.FootComment
			}

			if child.Kind == yaml.MappingNode || child.Kind == yaml.SequenceNode {
				*stack = append(*stack, commentStackItem{prefix: key, node: child})
			}
		}
	}
}

// CleanCommentForDisplay strips "#" prefixes and preserves interior blank
// lines so authored paragraph breaks survive the load → edit → save
// round-trip. Leading and trailing blanks are trimmed.
func CleanCommentForDisplay(raw string) string {
	return cleanCommentLines(raw, false)
}

// FormatCommentForYAML converts plain user-typed prose back into the
// "# "-prefixed verbatim form yaml.v3's encoder writes for HeadComment /
// FootComment / DocumentNode comments. Each line gets a "# " prefix; empty
// lines become "#". Returns "" for empty input so callers can clear a slot
// by passing "".
//
// This is the inverse of CleanCommentForDisplay (which strips prefixes for
// rendering); a round-trip clean → format → clean is idempotent. The format
// is what the load path reads off the parsed yaml.Node tree, so a freshly
// formatted block round-trips identically through PatchNodeTree.
func FormatCommentForYAML(text string) string {
	if text == "" {
		return ""
	}

	return formatHeadComment(text)
}

func parseComments(data []byte) (map[string]string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal yaml for comments: %w", err)
	}

	comments := make(map[string]string)

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return comments, nil
	}

	root := doc.Content[0]
	if root == nil || root.Kind != yaml.MappingNode {
		return comments, nil
	}

	stack := []commentStackItem{{prefix: "", node: root}}

	for len(stack) > 0 {
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		walkCommentChildren(item, &stack, comments)
	}

	return comments, nil
}

// walkCommentChildren visits each child of item.node, records any comment
// into the map keyed by the child's flat path, and pushes composite children
// onto the stack. Mapping nodes pair (key, value); sequence nodes have a single
// node per index — the two share the rest of the logic.
func walkCommentChildren(item commentStackItem, stack *[]commentStackItem, comments map[string]string) {
	content := item.node.Content

	switch item.node.Kind {
	case yaml.MappingNode:
		for i := 0; i < len(content)-1; i += 2 {
			keyNode, valNode := content[i], content[i+1]
			if keyNode == nil || valNode == nil {
				continue
			}

			// Line comments may sit on key or value; head comments on either.
			comment := bestComment(
				[]string{valNode.LineComment, keyNode.LineComment},
				[]string{keyNode.HeadComment, valNode.HeadComment},
			)
			recordChild(buildKey(item.prefix, keyNode.Value), valNode, comment, stack, comments)
		}
	case yaml.SequenceNode:
		for i, child := range content {
			if child == nil {
				continue
			}

			// Sequence items have no separate key node — comments come only
			// from the child itself.
			comment := bestComment([]string{child.LineComment}, []string{child.HeadComment})
			recordChild(item.prefix+"["+strconv.Itoa(i)+"]", child, comment, stack, comments)
		}
	}
}

func recordChild(
	key string, node *yaml.Node, comment string,
	stack *[]commentStackItem, comments map[string]string,
) {
	if comment != "" {
		comments[key] = comment
	}

	if node.Kind == yaml.MappingNode || node.Kind == yaml.SequenceNode {
		*stack = append(*stack, commentStackItem{prefix: key, node: node})
	}
}

// bestComment prefers a non-empty line comment, falling back to the first
// non-empty head comment.
func bestComment(lineCandidates, headCandidates []string) string {
	if c := bestLineComment(lineCandidates...); c != "" {
		return c
	}

	return bestHeadComment(headCandidates...)
}

func bestLineComment(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return cleanSingleComment(c)
		}
	}

	return ""
}

func bestHeadComment(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return cleanCommentLines(c, true)
		}
	}

	return ""
}

func cleanSingleComment(c string) string {
	c = strings.TrimLeft(c, "#")

	return strings.TrimSpace(c)
}

// cleanCommentLines strips "# " from each line and joins the remainder with
// "\n". When dropBlankLines is true, every blank line is removed (compact
// caption output); when false, interior blanks are preserved while leading
// and trailing blanks are trimmed (round-trippable editor display).
//
// Single-pass: blanks are buffered as a count and flushed before the next
// non-empty line, so the trailing-blank case naturally falls off when no
// content follows.
func cleanCommentLines(hc string, dropBlankLines bool) string {
	var b strings.Builder

	b.Grow(len(hc))

	pendingBlanks := 0
	haveContent := false

	for _, line := range strings.Split(hc, "\n") {
		cleaned := cleanSingleComment(strings.TrimSpace(line))
		if cleaned == "" {
			pendingBlanks++

			continue
		}

		if haveContent {
			b.WriteByte('\n')

			if !dropBlankLines {
				for range pendingBlanks {
					b.WriteByte('\n')
				}
			}
		}

		pendingBlanks = 0
		haveContent = true

		b.WriteString(cleaned)
	}

	return b.String()
}
