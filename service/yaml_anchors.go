package service

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// AnchorNameRegex defines the legal character set for anchor names in this
// app: letters, digits, underscore, and hyphen. YAML itself is more permissive
// but a conservative subset avoids ambiguity and keeps user-facing display
// predictable.
var AnchorNameRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// SetNodeAnchor marks the node at flatKey as a YAML anchor named anchorName.
// Mutates tree in place. When the target is reached via an alias, the alias is
// broken before the anchor is set so the new anchor lives on a local copy, not
// on a node shared with other aliases. Returns an error if tree is nil, the
// key can't be reached as a physical location, or the name is invalid.
func SetNodeAnchor(tree *yaml.Node, flatKey, anchorName string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	if !AnchorNameRegex.MatchString(anchorName) {
		return fmt.Errorf("invalid anchor name %q: must match %s", anchorName, AnchorNameRegex.String())
	}

	segments, err := parseKeySegments(flatKey)
	if err != nil {
		return fmt.Errorf("parse key %q: %w", flatKey, err)
	}

	if len(segments) == 0 {
		return errors.New("empty key")
	}

	node, err := resolvePhysicalForMutation(tree, segments)
	if err != nil {
		return err
	}

	node.Anchor = anchorName

	return nil
}

// ClearNodeAnchor removes the anchor name from the node at flatKey. Aliases
// elsewhere in the tree that pointed at this anchor become dangling and will
// fail to marshal; callers should either also convert those aliases to
// literals first or accept the save failure. Returns nil if the target has no
// anchor set.
func ClearNodeAnchor(tree *yaml.Node, flatKey string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	segments, err := parseKeySegments(flatKey)
	if err != nil {
		return fmt.Errorf("parse key %q: %w", flatKey, err)
	}

	node, err := resolvePhysicalForMutation(tree, segments)
	if err != nil {
		return err
	}

	node.Anchor = ""

	return nil
}

// RenameAnchor replaces the anchor name oldName with newName on the source
// node and on every alias that references it. Mutates tree in place. Returns
// an error if oldName is not defined anywhere, or if newName is not a legal
// anchor identifier.
//
// The .Alias pointer on each alias node continues to target the same source
// node, so no re-linking is needed — only the user-visible name changes on
// the source (.Anchor) and aliases (.Value).
func RenameAnchor(tree *yaml.Node, oldName, newName string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	if !AnchorNameRegex.MatchString(newName) {
		return fmt.Errorf("invalid anchor name %q: must match %s", newName, AnchorNameRegex.String())
	}

	if oldName == newName {
		return nil
	}

	if findAnchorNode(tree, oldName) == nil {
		return fmt.Errorf("anchor %q not found in tree", oldName)
	}

	if findAnchorNode(tree, newName) != nil {
		return fmt.Errorf("anchor %q already exists — choose a different name", newName)
	}

	visitNodes(tree, func(n *yaml.Node) {
		if n.Kind == yaml.AliasNode && n.Value == oldName {
			n.Value = newName

			return
		}

		if n.Anchor == oldName {
			n.Anchor = newName
		}
	})

	return nil
}

// DeleteAnchor removes an anchor and all references to it. Every alias
// pointing at the anchor is severed via breakAliasInPlace (its slot is
// replaced with a deep copy of the target's current contents); then the
// anchor definition's .Anchor field is cleared. Returns an error if the
// anchor isn't defined.
//
// No values are lost — the file simply no longer uses the anchor/alias
// syntax. Cells that were aliases keep their effective (resolved) values
// as plain YAML literals in place.
func DeleteAnchor(tree *yaml.Node, name string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	source := findAnchorNode(tree, name)
	if source == nil {
		return fmt.Errorf("anchor %q not found in tree", name)
	}

	// Collect the alias nodes first — mutating them during traversal is
	// fine for this walker (breakAliasInPlace only touches the node's own
	// fields) but deferring makes the intent explicit.
	var aliases []*yaml.Node

	visitNodes(tree, func(n *yaml.Node) {
		if n.Kind == yaml.AliasNode && n.Value == name {
			aliases = append(aliases, n)
		}
	})

	for _, a := range aliases {
		breakAliasInPlace(a)
	}

	source.Anchor = ""

	return nil
}

// ClearNodeAlias replaces the alias at flatKey with a local deep copy of the
// anchored target's current contents. The anchor definition elsewhere stays
// intact; only this usage site is severed. No-op when the node at flatKey
// is not an alias. Returns an error if the path can't be reached.
func ClearNodeAlias(tree *yaml.Node, flatKey string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	segments, err := parseKeySegments(flatKey)
	if err != nil {
		return fmt.Errorf("parse key %q: %w", flatKey, err)
	}

	if len(segments) == 0 {
		return errors.New("empty key")
	}

	parent, childIdx, err := findParentSlot(tree, segments)
	if err != nil {
		return err
	}

	node := parent.Content[childIdx]
	if node.Kind != yaml.AliasNode {
		return nil
	}

	breakAliasInPlace(node)

	return nil
}

// SetNodeAlias replaces the node at flatKey with an alias pointing at the
// anchor named anchorName. The anchor must exist elsewhere in the tree.
// Mutates tree in place. Returns an error if the anchor can't be found or
// if flatKey is the anchor's own defining location (would create a cycle).
func SetNodeAlias(tree *yaml.Node, flatKey, anchorName string) error {
	if tree == nil {
		return errors.New("tree is nil")
	}

	segments, err := parseKeySegments(flatKey)
	if err != nil {
		return fmt.Errorf("parse key %q: %w", flatKey, err)
	}

	if len(segments) == 0 {
		return errors.New("empty key")
	}

	target := findAnchorNode(tree, anchorName)
	if target == nil {
		return fmt.Errorf("anchor %q not found in tree", anchorName)
	}

	parent, childIdx, err := findParentSlot(tree, segments)
	if err != nil {
		return err
	}

	if parent.Content[childIdx] == target {
		return fmt.Errorf("cannot alias %q to itself (it is the anchor's defining location)", flatKey)
	}

	// Keep the alias's own comments from the prior node so annotations at this
	// site survive the conversion.
	prev := parent.Content[childIdx]
	alias := &yaml.Node{
		Kind:        yaml.AliasNode,
		Value:       anchorName,
		Alias:       target,
		HeadComment: prev.HeadComment,
		LineComment: prev.LineComment,
		FootComment: prev.FootComment,
	}
	parent.Content[childIdx] = alias

	return nil
}

// findAnchorNode returns the first node in the tree whose .Anchor equals name,
// or nil. Walks via mapping children, sequence items, and alias targets so
// anchors reachable through merge keys are still found.
func findAnchorNode(root *yaml.Node, name string) *yaml.Node {
	var found *yaml.Node

	var walk func(n *yaml.Node)

	walk = func(n *yaml.Node) {
		if n == nil || found != nil {
			return
		}

		if n.Anchor == name && n.Kind != yaml.AliasNode {
			found = n

			return
		}

		for _, c := range n.Content {
			walk(c)
		}
	}

	walk(root)

	return found
}

// visitNodes invokes visit on every non-nil descendant of root, including
// root itself. Recurses through Content; aliases are visited but not
// followed (the walker does not chase .Alias pointers, so anchor targets
// are visited via their regular position in the tree).
func visitNodes(root *yaml.Node, visit func(*yaml.Node)) {
	if root == nil {
		return
	}

	visit(root)

	for _, c := range root.Content {
		visitNodes(c, visit)
	}
}

// ExtractAnchors walks root and returns a map from flat key to AnchorInfo for
// every node that either defines an anchor (`&name`) or is an alias usage
// (`*name`). Returns nil when the tree contains no anchors or aliases, so
// callers can use map-is-nil as a cheap "no badges to render" check.
//
// Keys inherited via merge keys (`<<: *base`) are not annotated; the badge
// should live at the alias or anchor position, not on every inherited leaf.
func ExtractAnchors(root *yaml.Node) map[string]AnchorInfo {
	if root == nil {
		return nil
	}

	out := make(map[string]AnchorInfo)
	walkAnchors(root, "", out)

	if len(out) == 0 {
		return nil
	}

	return out
}

func walkAnchors(n *yaml.Node, prefix string, out map[string]AnchorInfo) {
	if n == nil {
		return
	}

	if n.Kind == yaml.AliasNode {
		if prefix != "" && n.Value != "" {
			out[prefix] = AnchorInfo{Role: AnchorRoleAlias, Name: n.Value}
		}

		return
	}

	if n.Anchor != "" && prefix != "" {
		out[prefix] = AnchorInfo{Role: AnchorRoleAnchor, Name: n.Anchor}
	}

	switch n.Kind {
	case yaml.MappingNode:
		forEachMappingChild(n, prefix, func(childPrefix string, child *yaml.Node) {
			walkAnchors(child, childPrefix, out)
		})

	case yaml.SequenceNode:
		for i, c := range n.Content {
			walkAnchors(c, prefix+"["+strconv.Itoa(i)+"]", out)
		}

	case yaml.ScalarNode, yaml.DocumentNode, yaml.AliasNode:
		// Scalars have no children; aliases handled above; document nodes not
		// expected inside a root mapping walk.
	}
}

// breakAliasInPlace converts an alias node into a local deep copy of its
// target, severing the alias at this position. The alias's own comments stay
// on the node so inline documentation at the alias site is preserved.
func breakAliasInPlace(alias *yaml.Node) {
	if alias.Kind != yaml.AliasNode || alias.Alias == nil {
		return
	}

	cp := deepCopyYAMLNode(alias.Alias)
	head, line, foot := alias.HeadComment, alias.LineComment, alias.FootComment

	alias.Kind = cp.Kind
	alias.Style = cp.Style
	alias.Tag = cp.Tag
	alias.Value = cp.Value
	alias.Anchor = ""
	alias.Alias = nil
	alias.Content = cp.Content
	alias.HeadComment = head
	alias.LineComment = line
	alias.FootComment = foot
}

// resolveAlias follows alias pointers until a non-alias node is reached.
func resolveAlias(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}

	return n
}
