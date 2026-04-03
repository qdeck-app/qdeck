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

// parseComments extracts YAML comments from raw bytes and returns
// a map from flat dot-separated key to comment string.
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

		switch item.node.Kind {
		case yaml.MappingNode:
			processMappingComments(item, &stack, comments)
		case yaml.SequenceNode:
			processSequenceComments(item, &stack, comments)
		}
	}

	return comments, nil
}

func processMappingComments(item commentStackItem, stack *[]commentStackItem, comments map[string]string) {
	content := item.node.Content

	for i := 0; i < len(content)-1; i += 2 {
		keyNode := content[i]
		valNode := content[i+1]

		if keyNode == nil || valNode == nil {
			continue
		}

		// Skip entries with empty keys to avoid malformed keys in the comment map.
		if keyNode.Value == "" {
			continue
		}

		fullKey := buildKey(item.prefix, keyNode.Value)

		// Try line comments first (inline), then head comments (block above).
		// Check both value and key nodes for line comments, then both for head comments.
		comment := bestLineComment(valNode.LineComment, keyNode.LineComment)
		if comment == "" {
			comment = bestHeadComment(keyNode.HeadComment, valNode.HeadComment)
		}

		if comment != "" {
			comments[fullKey] = comment
		}

		if valNode.Kind == yaml.MappingNode || valNode.Kind == yaml.SequenceNode {
			*stack = append(*stack, commentStackItem{prefix: fullKey, node: valNode})
		}
	}
}

func processSequenceComments(item commentStackItem, stack *[]commentStackItem, comments map[string]string) {
	for i, child := range item.node.Content {
		if child == nil {
			continue
		}

		indexKey := item.prefix + "[" + strconv.Itoa(i) + "]"

		// Sequence items use simplified comment priority (line then head comments from the child node).
		// Unlike mappings which have separate key/value nodes, each sequence item is represented
		// by a single yaml.Node with no separate key node to provide comments.
		comment := bestLineComment(child.LineComment)
		if comment == "" {
			comment = bestHeadComment(child.HeadComment)
		}

		if comment != "" {
			comments[indexKey] = comment
		}

		if child.Kind == yaml.MappingNode || child.Kind == yaml.SequenceNode {
			*stack = append(*stack, commentStackItem{prefix: indexKey, node: child})
		}
	}
}

// bestLineComment returns the first non-empty single-line comment from candidates, cleaned.
// Used for line comments (inline comments on the same line as the value).
func bestLineComment(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return cleanSingleComment(c)
		}
	}

	return ""
}

// bestHeadComment returns the first non-empty multi-line head comment from candidates, cleaned.
// Used for head comments (block comments above the key).
func bestHeadComment(candidates ...string) string {
	for _, c := range candidates {
		if c != "" {
			return cleanHeadComment(c)
		}
	}

	return ""
}

// cleanSingleComment strips the leading "# " from a single-line comment.
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
