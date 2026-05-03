package service

import (
	"os"
	"strings"
	"testing"
)

// yamlTrueLiteral is the YAML scalar form of boolean true. Shared across
// service test files so goconst doesn't flag duplication.
const yamlTrueLiteral = "true"

// fixturePath is the on-disk corner-case fixture loaded by both
// TestRoundTripCornerCases and (via ReadCustomValues) TestLoadCustomFixture_*.
// Path is relative to the service/ directory, where `go test` runs.
const fixturePath = "../assets/test-data/redis-values-cornercases.yaml"

// TestRoundTripCornerCases is the headline regression for comment + anchor +
// unusual-key preservation. It loads the on-disk fixture
// assets/test-data/redis-values-cornercases.yaml — which exercises file-level
// banners, anchors with alias references, single-source merge keys
// (`<<: *base`), multi-source merge keys (`<<: [*a, *b]`), per-leaf foot
// blocks, deep nesting, AND the two unusual-key cases the FlatKey escape
// scheme handles: empty-string map keys (`"": v`) and keys containing
// literal '.' (`app.kubernetes.io/part-of`).
//
// The test patches a single primitive scalar so PatchNodeTree's edit path
// runs end-to-end — without an edit the path would shortcut through "nothing
// changed" and miss serializer regressions.
func TestRoundTripCornerCases(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Skipf("fixture not present or unreadable: %v", err)
	}

	src := string(raw)

	tree, docs := loadTreeAndDocs(t, src)

	svc := NewValuesService()

	parsed, err := svc.ParseYAMLText(t.Context(), src)
	if err != nil {
		t.Fatalf("ParseYAMLText: %v", err)
	}

	const (
		fixtureRegistry   = "my-registry.example.com"
		fixtureBanner     = "Bitnami Redis"
		fixtureAnchor     = "&defaults"
		fixtureMergeAlpha = "<<:"
		fixtureAlias      = "*defaults"
		// Empty-string key in cornerCases.quotedKeys (line 420 of fixture).
		fixtureEmptyKeyValue = "empty key"
	)
	// fixtureDottedLabelValue is the value paired with the literal-dot
	// k8s label key in commonLabels (line 79 of fixture). Lifted to
	// package scope as dottedLabelValue so the serialise tests can reuse
	// the same literal.
	fixtureDottedLabelValue := dottedLabelValue

	// Build override entries from the parsed flat-key list, patching one
	// scalar so the edit path runs.
	entries := make([]OverrideEntry, 0, len(parsed.Entries))
	patched := false

	for _, e := range parsed.Entries {
		if e.Type == typeMap || e.Type == typeList {
			continue
		}

		value := e.Value
		if !patched && string(e.Key) == "image.registry" {
			value = fixtureRegistry
			patched = true
		}

		entries = append(entries, OverrideEntry{
			Key:   string(e.Key),
			Value: value,
			Type:  e.Type,
		})
	}

	if !patched {
		t.Fatal("did not find image.registry in parsed entries; fixture changed?")
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
		{"banner", fixtureBanner},
		{"anchor &defaults", fixtureAnchor},
		{"merge key directive", fixtureMergeAlpha},
		{"alias reference *defaults", fixtureAlias},
		{"empty-string-key value", fixtureEmptyKeyValue},
		{"literal-dot label value", fixtureDottedLabelValue},
		{"literal-dot key preserved as single map key", "app.kubernetes.io/part-of"},
		{"another literal-dot key", "kubernetes.io/change-cause"},
	}

	for _, c := range checks {
		if !strings.Contains(got, c.contains) {
			t.Errorf("%s missing from output (looked for %q)", c.name, c.contains)
		}
	}

	// Guard against the silent-corruption shape: a literal-dot key being
	// split into nested mappings would produce a top-level "  app:" line
	// inside commonLabels.
	commonLabelsBlock := extractMappingBlock(got, "commonLabels:")
	if strings.Contains(commonLabelsBlock, "\n  app:\n") {
		t.Errorf("literal-dot label key got split into nested mappings:\n%s", commonLabelsBlock)
	}
}

// extractMappingBlock returns the substring of yaml from the line beginning
// with header through the next blank line or top-level key. Used to scope
// substring assertions to a specific mapping block.
func extractMappingBlock(yaml, header string) string {
	idx := strings.Index(yaml, header)
	if idx < 0 {
		return ""
	}

	rest := yaml[idx:]

	// Stop at the first non-indented, non-empty line after the header line.
	lines := strings.Split(rest, "\n")
	if len(lines) <= 1 {
		return rest
	}

	end := 1

	for end < len(lines) {
		line := lines[end]
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#") {
			break
		}

		end++
	}

	return strings.Join(lines[:end], "\n")
}
