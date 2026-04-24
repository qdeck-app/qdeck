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
			if keyNode == nil || valNode == nil || keyNode.Value == "" {
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
			return cleanHeadComment(c)
		}
	}

	return ""
}

func cleanSingleComment(c string) string {
	c = strings.TrimLeft(c, "#")

	return strings.TrimSpace(c)
}

// cleanHeadComment handles multi-line head comments by stripping "# " from each line.
// Empty lines are intentionally skipped to produce compact output suitable for UI captions.
func cleanHeadComment(hc string) string {
	lines := strings.Split(hc, "\n")

	var b strings.Builder

	b.Grow(len(hc))

	for _, line := range lines {
		cleaned := cleanSingleComment(strings.TrimSpace(line))
		if cleaned == "" {
			// Skip empty lines for compact output. This condenses multi-line comments
			// to remove intentional blank lines, making them more suitable for display
			// in compact UI captions.
			continue
		}

		if b.Len() > 0 {
			b.WriteByte('\n')
		}

		b.WriteString(cleaned)
	}

	return b.String()
}
