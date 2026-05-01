package service

import (
	"strings"
	"testing"
)

// yamlTrueLiteral is the YAML scalar form of boolean true. Shared across
// service test files so goconst doesn't flag duplication.
const yamlTrueLiteral = "true"

// TestRoundTripCornerCases is the headline regression for the comment +
// anchor preservation work. The inline source below is a condensed
// counterpart to test-data/redis-values-cornercases.yaml — it exercises the
// features qdeck must round-trip on save: file-level banner, anchors with
// alias references, single-source merge keys (`<<: *base`), multi-source
// merge keys (`<<: [*a, *b]`), per-leaf foot blocks, deeply nested mappings,
// and quoted keys.
//
// The inline form is preferred over loading the on-disk fixture because the
// parser doesn't yet support every YAML construct the fixture contains
// (notably empty-string keys produce flat keys like `parent.` that
// parseKeySegments rejects). When that limitation is fixed, this test can
// be ported to load the fixture directly and the assertions transferred.
//
// The test patches a single primitive scalar so PatchNodeTree's edit path
// runs end-to-end — without an edit, the path would just shortcut through
// "nothing changed" and miss serializer regressions.
func TestRoundTripCornerCases(t *testing.T) {
	t.Parallel()

	src := `# ============================================================================
# Banner block — must survive as DocumentNode.HeadComment.
# ============================================================================

x-defaults: &defaults
  enabled: true
  replicas: 1

x-extra: &extra
  tier: gold

global:
  imageRegistry: ""
  storageClass: "standard-rwo"
  # foot block on storageClass

primary:
  <<: *defaults
  resources:
    cpu: 100m
    memory: 128Mi

replica:
  <<: [*defaults, *extra]
  enabled: false

deep:
  level1:
    level2:
      level3:
        leaf: deep-value
`

	tree, docs := loadTreeAndDocs(t, src)

	// Build entries from the parsed tree. Filter to physical keys the
	// parser handles — anchors / merge sources are physical leaves but
	// merge keys themselves aren't user-edited, so we don't add them to
	// `want`. A real save flow does the same via collectOverrides.
	const (
		fixtureRegistry  = "my-registry.example.com"
		fixtureDeepValue = "deep-value"
		fixtureFootCheck = "foot block on storageClass"
	)

	entries := []OverrideEntry{
		{Key: "x-defaults.enabled", Value: yamlTrueLiteral, Type: typeBool},
		{Key: "x-defaults.replicas", Value: "1", Type: "int"},
		{Key: "x-extra.tier", Value: "gold", Type: typeString},
		{Key: "global.imageRegistry", Value: fixtureRegistry, Type: typeString},
		{Key: "global.storageClass", Value: "standard-rwo", Type: typeString},
		{Key: "primary.resources.cpu", Value: "100m", Type: typeString},
		{Key: "primary.resources.memory", Value: "128Mi", Type: typeString},
		{Key: "replica.enabled", Value: "false", Type: typeBool},
		{Key: "deep.level1.level2.level3.leaf", Value: fixtureDeepValue, Type: typeString},
	}

	got, err := PatchNodeTree(tree, entries, DefaultYAMLIndent, docs)
	if err != nil {
		t.Fatalf("PatchNodeTree: %v", err)
	}

	checks := []struct {
		name     string
		contains string
	}{
		{"patched value", fixtureRegistry},
		{"banner", "Banner block"},
		{"anchor &defaults", "&defaults"},
		{"anchor &extra", "&extra"},
		{"merge key directive", "<<:"},
		{"alias reference *defaults", "*defaults"},
		{"alias reference *extra", "*extra"},
		{"foot block on storageClass", fixtureFootCheck},
		{"deep nesting leaf", fixtureDeepValue},
	}

	for _, c := range checks {
		if !strings.Contains(got, c.contains) {
			t.Errorf("%s missing from output (looked for %q):\n%s", c.name, c.contains, got)
		}
	}
}
