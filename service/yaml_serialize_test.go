package service

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestPatchNodeTree_PreservesBanner verifies the file-level banner survives a
// round-trip. yaml.v3 attaches the banner block to the first key's HeadComment,
// so its preservation flows through the existing OverrideEntry.HeadComment
// path — not through DocComments.Head, which is reserved for the rare case of
// a yaml DocumentNode-level head comment (typically only present with `---`
// document separators). This test still passes through the new
// encodeWithDocComments wrapper to confirm the wrapper doesn't break the
// existing path.
//
// PatchNodeTree's deletion loop drops any leaf not in `want`, so the test
// passes the full entry set echoing the source values; a real save flow does
// the same via collectOverrides.
func TestPatchNodeTree_PreservesBanner(t *testing.T) {
	t.Parallel()

	src := `# Banner line one
# Banner line two
key: value
`

	tree, docs := loadTreeAndDocs(t, src)

	const fixtureKey = "key"

	entries := []OverrideEntry{
		{Key: fixtureKey, Value: "value", Type: typeString, HeadComment: "Banner line one\nBanner line two"},
	}

	got, err := PatchNodeTree(tree, entries, DefaultYAMLIndent, docs)
	if err != nil {
		t.Fatalf("PatchNodeTree: %v", err)
	}

	if !strings.Contains(got, "Banner line one") {
		t.Fatalf("output missing banner:\n%s", got)
	}

	if !strings.Contains(got, "Banner line two") {
		t.Fatalf("output missing second banner line:\n%s", got)
	}

	if !strings.Contains(got, fixtureKey+": value") {
		t.Fatalf("entry missing:\n%s", got)
	}
}

// TestPatchNodeTree_PreservesFootOnUneditedLeaf is the regression test for the
// FootComment-wipe bug — applyOverrideComments used to clear FootComment on
// every edited leaf, which silently destroyed orphan blocks attached to
// neighboring leaves the user never touched. With the fix in place an
// unrelated edit must leave foot comments alone.
func TestPatchNodeTree_PreservesFootOnUneditedLeaf(t *testing.T) {
	t.Parallel()

	src := `auth:
  enabled: true
  password: secret
  # trailing comment on auth.password
keep: original
`

	tree, docs := loadTreeAndDocs(t, src)

	// Edit only `keep`, leaving auth.password (and its foot) untouched.
	const fixtureEdited = "edited"

	entries := []OverrideEntry{
		{Key: "auth.enabled", Value: yamlTrueLiteral, Type: typeBool},
		{Key: "auth.password", Value: "secret", Type: typeString},
		{Key: "keep", Value: fixtureEdited, Type: typeString},
	}

	got, err := PatchNodeTree(tree, entries, DefaultYAMLIndent, docs)
	if err != nil {
		t.Fatalf("PatchNodeTree: %v", err)
	}

	if !strings.Contains(got, "trailing comment on auth.password") {
		t.Fatalf("foot comment dropped on unrelated edit:\n%s", got)
	}

	if !strings.Contains(got, fixtureEdited) {
		t.Fatalf("edited value missing:\n%s", got)
	}
}

// TestPatchNodeTree_PreservesFootOnEditedLeaf — even when the user edits the
// very leaf that owns a foot comment, the foot must survive (it's attached to
// the leaf's value node, not a property of the value itself).
func TestPatchNodeTree_PreservesFootOnEditedLeaf(t *testing.T) {
	t.Parallel()

	src := `key: original
# foot block on key

other: x
`

	tree, docs := loadTreeAndDocs(t, src)

	const fixtureEdited2 = "edited"

	entries := []OverrideEntry{
		{Key: "key", Value: fixtureEdited2, Type: typeString},
		{Key: "other", Value: "x", Type: typeString},
	}

	got, err := PatchNodeTree(tree, entries, DefaultYAMLIndent, docs)
	if err != nil {
		t.Fatalf("PatchNodeTree: %v", err)
	}

	if !strings.Contains(got, "foot block on key") {
		t.Fatalf("foot dropped on edited leaf:\n%s", got)
	}

	if !strings.Contains(got, "key: edited") {
		t.Fatalf("edited value missing:\n%s", got)
	}
}

// TestFlatEntriesToYAML_BannerWithoutTree exercises the from-scratch path —
// when no NodeTree is available, FlatEntriesToYAML still wraps the encoded
// mapping in a DocumentNode so banner/footer make it into the output.
func TestFlatEntriesToYAML_BannerWithoutTree(t *testing.T) {
	t.Parallel()

	entries := []OverrideEntry{
		{Key: "k", Value: "v", Type: "string"},
	}

	docs := DocComments{
		Head: "# fresh banner",
	}

	got, err := FlatEntriesToYAML(entries, DefaultYAMLIndent, docs)
	if err != nil {
		t.Fatalf("FlatEntriesToYAML: %v", err)
	}

	if !strings.Contains(got, "fresh banner") {
		t.Fatalf("banner missing from from-scratch output:\n%s", got)
	}

	if !strings.Contains(got, "k: v") {
		t.Fatalf("entry missing:\n%s", got)
	}
}

// loadTreeAndDocs is the test helper that mirrors how ReadCustomValues sets up
// tree + docs: parse the YAML, store doc.Content[0] as the working tree, run
// parseOrphanComments to capture banner/footer/foots.
func loadTreeAndDocs(t *testing.T, src string) (*yaml.Node, DocComments) {
	t.Helper()

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(src), &doc); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	if len(doc.Content) == 0 {
		t.Fatal("yaml.Unmarshal produced no content")
	}

	oc, err := parseOrphanComments([]byte(src))
	if err != nil {
		t.Fatalf("parseOrphanComments: %v", err)
	}

	return doc.Content[0], DocComments{
		Head:  oc.DocHead,
		Foot:  oc.DocFoot,
		Foots: oc.Foots,
	}
}
