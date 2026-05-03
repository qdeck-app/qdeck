package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/qdeck-app/qdeck/domain"
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
		{Key: keepKey, Value: fixtureEdited, Type: typeString},
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

// commonLabelsK8sPartOfKey is the flat-key form of the k8s
// "app.kubernetes.io/part-of" label nested under "commonLabels", used by
// several escape-aware serialiser tests. dottedLabelValue is the literal
// value paired with that key in the corner-case fixture.
const (
	commonLabelsK8sPartOfKey = `commonLabels.app\.kubernetes\.io/part-of`
	dottedLabelValue         = "platform"
	// keepKey is a stable scalar key shared across round-trip tests for
	// the "edit only some siblings" scenario.
	keepKey = "keep"

	// resourceCPUValue / resourceMemValue are the canonical scalar values
	// used by the alias and merge-key round-trip tests; lifted to
	// package-scope constants so goconst stays quiet.
	resourceCPUValue = "100m"
	resourceMemValue = "128Mi"
)

// TestPatchNodeTree_EmptyStringKey verifies that a YAML mapping containing an
// empty-string key ("": value) round-trips through PatchNodeTree without the
// flat-key parser rejecting the trailing-dot encoding.
func TestPatchNodeTree_EmptyStringKey(t *testing.T) {
	t.Parallel()

	src := `quotedKeys:
  regular: one
  "": two
`

	tree, docs := loadTreeAndDocs(t, src)

	entries := []OverrideEntry{
		{Key: "quotedKeys.regular", Value: "one", Type: typeString},
		{Key: "quotedKeys.", Value: "two", Type: typeString}, // trailing dot encodes empty-string key
	}

	got, err := PatchNodeTree(tree, entries, DefaultYAMLIndent, docs)
	if err != nil {
		t.Fatalf("PatchNodeTree: %v", err)
	}

	if !strings.Contains(got, `"":`) && !strings.Contains(got, `'':`) {
		t.Errorf("output missing empty-string key:\n%s", got)
	}

	if !strings.Contains(got, "two") {
		t.Errorf("empty-key value missing:\n%s", got)
	}

	if !strings.Contains(got, "regular: one") {
		t.Errorf("sibling regular key missing:\n%s", got)
	}
}

// TestPatchNodeTree_LiteralDotInKey verifies that a YAML mapping key
// containing a literal '.' (common for k8s labels like
// "app.kubernetes.io/part-of") survives a round-trip as a single key, not
// split into nested mappings.
func TestPatchNodeTree_LiteralDotInKey(t *testing.T) {
	t.Parallel()

	src := `commonLabels:
  app.kubernetes.io/part-of: platform
  costcenter/team: eng
`

	tree, docs := loadTreeAndDocs(t, src)

	entries := []OverrideEntry{
		{Key: commonLabelsK8sPartOfKey, Value: dottedLabelValue, Type: typeString},
		{Key: "commonLabels.costcenter/team", Value: "eng", Type: typeString},
	}

	got, err := PatchNodeTree(tree, entries, DefaultYAMLIndent, docs)
	if err != nil {
		t.Fatalf("PatchNodeTree: %v", err)
	}

	if !strings.Contains(got, "app.kubernetes.io/part-of: platform") {
		t.Errorf("literal-dot key not preserved as a single map key:\n%s", got)
	}

	// A nested form would look like "  app:\n    kubernetes:\n      io/part-of"
	// — guard against that explicit corruption shape.
	if strings.Contains(got, "  app:") {
		t.Errorf("literal-dot key was split into nested mappings:\n%s", got)
	}
}

// TestPatchNodeTree_EditLiteralDotKey verifies that editing a value whose key
// contains a literal '.' updates only that single map entry and does not
// create a parallel nested structure.
func TestPatchNodeTree_EditLiteralDotKey(t *testing.T) {
	t.Parallel()

	src := `commonLabels:
  app.kubernetes.io/part-of: platform
`

	tree, docs := loadTreeAndDocs(t, src)

	entries := []OverrideEntry{
		{Key: commonLabelsK8sPartOfKey, Value: "kube-system", Type: typeString},
	}

	got, err := PatchNodeTree(tree, entries, DefaultYAMLIndent, docs)
	if err != nil {
		t.Fatalf("PatchNodeTree: %v", err)
	}

	if !strings.Contains(got, "app.kubernetes.io/part-of: kube-system") {
		t.Errorf("edit on literal-dot key didn't land on the intended entry:\n%s", got)
	}

	if strings.Contains(got, "platform") {
		t.Errorf("old value still present after edit:\n%s", got)
	}
}

// TestFlatEntriesToYAML_EmptyAndDottedKeys exercises the from-scratch builder
// (used when no NodeTree is available) for the same edge cases.
func TestFlatEntriesToYAML_EmptyAndDottedKeys(t *testing.T) {
	t.Parallel()

	entries := []OverrideEntry{
		{Key: commonLabelsK8sPartOfKey, Value: dottedLabelValue, Type: typeString},
		{Key: "commonLabels.simple", Value: "v", Type: typeString},
		{Key: "quoted.", Value: "empty-key-value", Type: typeString},
	}

	got, err := FlatEntriesToYAML(entries, DefaultYAMLIndent, DocComments{})
	if err != nil {
		t.Fatalf("FlatEntriesToYAML: %v", err)
	}

	if !strings.Contains(got, "app.kubernetes.io/part-of: platform") {
		t.Errorf("literal-dot key missing:\n%s", got)
	}

	if !strings.Contains(got, "simple: v") {
		t.Errorf("sibling simple key missing:\n%s", got)
	}

	if !strings.Contains(got, "empty-key-value") {
		t.Errorf("empty-string-key value missing:\n%s", got)
	}
}

// TestPatchNodeTree_EmptyContainerPlaceholdersRoundTrip verifies that the
// "{}"/"[]" placeholders flattenValues emits for empty mapping/sequence leaves
// don't get rewritten as literal "{}"/"[]" string scalars on save. The
// deep-copied tree already carries the correct empty container; PatchNodeTree
// should detect the kind match and skip the entry.
func TestPatchNodeTree_EmptyContainerPlaceholdersRoundTrip(t *testing.T) {
	t.Parallel()

	src := `imagePullSecrets: []
extraPolicies: {}
keep: original
`

	tree, docs := loadTreeAndDocs(t, src)

	entries := []OverrideEntry{
		{Key: "imagePullSecrets", Value: "[]", Type: typeList},
		{Key: "extraPolicies", Value: "{}", Type: typeMap},
		{Key: keepKey, Value: "original", Type: typeString},
	}

	got, err := PatchNodeTree(tree, entries, DefaultYAMLIndent, docs)
	if err != nil {
		t.Fatalf("PatchNodeTree: %v", err)
	}

	if !strings.Contains(got, "imagePullSecrets: []") {
		t.Errorf("imagePullSecrets lost its empty-list shape:\n%s", got)
	}

	if !strings.Contains(got, "extraPolicies: {}") {
		t.Errorf("extraPolicies lost its empty-map shape:\n%s", got)
	}

	// Specifically guard against the corruption shape — the placeholder
	// being saved as a single-quoted string scalar.
	if strings.Contains(got, "imagePullSecrets: '[]'") || strings.Contains(got, "extraPolicies: '{}'") {
		t.Errorf("placeholder got saved as a string scalar:\n%s", got)
	}
}

// TestPatchNodeTree_NullValueEquivalence verifies that PatchNodeTree's
// "unchanged" check treats `~`, `null`, and the blank YAML null form as
// equivalent — without this, a null leaf in the source would mismatch an
// override entry's empty Value (set by flattenValues when typedVal is nil)
// and trigger an unwanted upsert that re-styles the null on save.
func TestPatchNodeTree_NullValueEquivalence(t *testing.T) {
	t.Parallel()

	src := `usePasswordFiles: ~
explicitNull: null
keep: x
`

	tree, docs := loadTreeAndDocs(t, src)

	// Mimic flattenValues' nil handling — Value="" for both null entries.
	entries := []OverrideEntry{
		{Key: "usePasswordFiles", Value: "", Type: typeNull},
		{Key: "explicitNull", Value: "", Type: typeNull},
		{Key: keepKey, Value: "x", Type: typeString},
	}

	got, err := PatchNodeTree(tree, entries, DefaultYAMLIndent, docs)
	if err != nil {
		t.Fatalf("PatchNodeTree: %v", err)
	}

	// Original literal forms must survive — the unchanged-check skips the
	// upsert that would otherwise force `null` everywhere.
	if !strings.Contains(got, "usePasswordFiles: ~") {
		t.Errorf("`~` literal lost:\n%s", got)
	}

	if !strings.Contains(got, "explicitNull: null") {
		t.Errorf("`null` literal lost:\n%s", got)
	}
}

// TestPatchNodeTree_AliasSurvivesUneditedSave is the regression test for the
// alias-break-via-effectiveComments bug. flattenValues resolves aliases when
// emitting flat entries, so an aliased subtree's leaves end up in `entries`
// addressed via the alias path. PatchNodeTree's per-entry loop calls
// effectiveComments to compare comment state — that walker used to mutate
// the tree (break aliases) just by reading. With the read-only walker plus
// the viaAlias signal, an unedited round-trip leaves every alias intact.
//
// The fixture mirrors the shape of redis-values-cornercases.yaml's anchor
// section: a top-level &anchor mapping, an alias under another key, and an
// inline comment on the aliased line.
func TestPatchNodeTree_AliasSurvivesUneditedSave(t *testing.T) {
	t.Parallel()

	src := `defaults: &defaults
  cpu: 100m # core baseline
  memory: 128Mi
master:
  resources: *defaults
`

	tree, docs := loadTreeAndDocs(t, src)

	entries := []OverrideEntry{
		{Key: "defaults.cpu", Value: resourceCPUValue, Type: typeString, LineComment: "core baseline"},
		{Key: "defaults.memory", Value: resourceMemValue, Type: typeString},
		// flattenValues resolves *defaults — these arrive at the
		// aliased path with the resolved values. The unchanged-check
		// must skip without breaking the alias.
		{Key: "master.resources.cpu", Value: resourceCPUValue, Type: typeString},
		{Key: "master.resources.memory", Value: resourceMemValue, Type: typeString},
	}

	got, err := PatchNodeTree(tree, entries, DefaultYAMLIndent, docs)
	if err != nil {
		t.Fatalf("PatchNodeTree: %v", err)
	}

	if !strings.Contains(got, "*defaults") {
		t.Errorf("alias *defaults expanded:\n%s", got)
	}

	if !strings.Contains(got, "&defaults") {
		t.Errorf("anchor &defaults lost:\n%s", got)
	}

	// Guard against the corruption shape: the alias being expanded into
	// a literal mapping under master.resources.
	if strings.Contains(got, "  resources:\n    cpu:") {
		t.Errorf("alias was expanded inline:\n%s", got)
	}
}

// TestBigIntPrecision is the regression for the big-int precision bug.
// Helm's chartutil decodes YAML through sigs.k8s.io/yaml, which routes
// integers through JSON and silently casts any value larger than 2^53 to
// float64 — `9007199254740993` becomes `9.007199254740992e+15` (last digit
// `3` → `2`). yaml.v3 keeps the literal text on the parsed scalar node, so
// once vf.NodeTree is populated rewriteValuesFromNodeTree restores the
// source-faithful value. Both ValuesService entry points the UI uses must
// run the rewrite — ReadCustomValues for direct loads, and the merge path
// (ReadAndMergeCustomValues, which the override-column flow calls).
func TestBigIntPrecision(t *testing.T) {
	t.Parallel()

	const literal = "9007199254740993"

	dir := t.TempDir()
	path := dir + "/values.yaml"

	if err := os.WriteFile(path, []byte("externalAccessTimeoutMs: "+literal+"\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	svc := NewValuesService()

	check := func(t *testing.T, entries []domain.ValuesEntry) {
		t.Helper()

		for _, e := range entries {
			if string(e.Key) == "externalAccessTimeoutMs" {
				if e.Value != literal {
					t.Errorf("big int lost precision: got %q, want %q", e.Value, literal)
				}

				return
			}
		}

		t.Fatal("entry not found")
	}

	t.Run("ReadCustomValues", func(t *testing.T) {
		t.Parallel()

		vf, err := svc.ReadCustomValues(context.Background(), path)
		if err != nil {
			t.Fatalf("ReadCustomValues: %v", err)
		}

		check(t, vf.Entries)
	})

	t.Run("ReadAndMergeCustomValues", func(t *testing.T) {
		t.Parallel()

		vf, err := svc.ReadAndMergeCustomValues(context.Background(), []string{path})
		if err != nil {
			t.Fatalf("ReadAndMergeCustomValues: %v", err)
		}

		check(t, vf.Entries)
	})
}

// Constants for unescaper test cases that goconst flags as duplicated.
const (
	singleQuotedEscape     = `k: '\U0001F389'` + "\n"
	commentWithEscape      = "# pretend escape \\U0001F389 in a comment\nk: 1\n"
	doubleEscapedBackslash = `k: "literal \\U0001F389 chars"` + "\n"
	plainNoBackslash       = "plain: value\n"
)

// TestParseComments_EmptyStringKey is the regression for the empty-key
// comment-loss bug. parseComments and the position walker used to skip any
// (key, value) pair whose keyNode.Value was the empty string ("") on the
// theory that it was malformed parser output. That filter conflated
// "missing key" with "explicit empty-string key", which is a legitimate
// YAML construct (`"": value`). On round-trip, the empty-key entry's
// inline / head comment was dropped, the comment got re-emitted as text
// elsewhere, and PatchNodeTree's commentsUnchanged check thought the
// user had cleared the comment.
func TestParseComments_EmptyStringKey(t *testing.T) {
	t.Parallel()

	src := []byte(`quotedKeys:
  regular: one
  "": "empty key"  # head comment on the empty key
`)

	got, err := parseComments(src)
	if err != nil {
		t.Fatalf("parseComments: %v", err)
	}

	const flatKey = "quotedKeys."

	if c, ok := got[flatKey]; !ok || c != "head comment on the empty key" {
		t.Errorf("empty-key inline comment dropped: got[%q]=%q, ok=%v", flatKey, c, ok)
	}
}

// TestUnescapeYAMLSupplementaryRunes covers the post-emit pass that
// rewrites yaml.v3's `\Unnnnnnnn` / `\unnnn` escape sequences inside
// double-quoted scalars back to their literal Unicode characters. yaml.v3
// classifies code points above the Basic Multilingual Plane as
// "unprintable" and forces double-quoted style with hex escapes regardless
// of the requested Style — without this rewrite, every save silently
// corrupts emoji and supplementary-plane CJK to seven-character escape
// strings.
func TestUnescapeYAMLSupplementaryRunes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "emoji in double-quoted scalar",
			in:   `k: "\U0001F389\U0001F680"` + "\n",
			want: "k: \"🎉🚀\"\n",
		},
		{
			name: "BMP escape inside double quotes",
			in:   `k: "é"` + "\n",
			want: "k: \"é\"\n",
		},
		{
			name: "literal `\\U` in single-quoted scalar untouched",
			in:   singleQuotedEscape,
			want: singleQuotedEscape,
		},
		{
			name: "comment with `\\U` text untouched",
			in:   commentWithEscape,
			want: commentWithEscape,
		},
		{
			name: "double-escaped backslash kept",
			in:   doubleEscapedBackslash,
			want: doubleEscapedBackslash,
		},
		{
			name: "no backslashes — fast path",
			in:   plainNoBackslash,
			want: plainNoBackslash,
		},
		{
			name: "mixed escapes in same string",
			in:   `k: "Привет 世界 \U0001F44B\U0001F3FD"` + "\n",
			want: "k: \"Привет 世界 👋🏽\"\n",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := unescapeYAMLSupplementaryRunes(tc.in)
			if got != tc.want {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
		})
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
