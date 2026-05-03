package state

import (
	"testing"

	"gioui.org/widget"

	"github.com/qdeck-app/qdeck/service"
)

// typeNull / typeString mirror the canonical values the service package
// stores in FlatValueEntry.Type. Test-only flat keys are also lifted into
// constants here so goconst stays quiet.
const (
	typeNull   = "null"
	typeString = "string"

	flatKeyImageRegistry  = "global.imageRegistry"
	flatKeyUsePasswordFls = "auth.usePasswordFiles"
	flatKeyImageTag       = "image.tag"
	flatKeyImageTagValue  = "1.2.3"
	flatKeyAuthPwd        = "auth.password"
)

// TestCollectOverrides_HashInsideStringNotSplit covers the bug where a YAML
// value containing a literal `#` (e.g. `"s3cr3t # not really a comment"`)
// was being mis-split into value + inline comment by SplitInlineComment on
// save, breaking the surrounding YAML alias and producing a phantom inline
// comment. When the editor text matches the loaded entry's Value verbatim
// (no edit) the splitter must be skipped.
func TestCollectOverrides_HashInsideStringNotSplit(t *testing.T) {
	t.Parallel()

	const literal = "s3cr3t # not really a comment"

	entries := []service.FlatValueEntry{
		{Key: flatKeyAuthPwd, Value: literal, Type: typeString, Kind: service.EntryKindLeaf},
	}

	editors := make([]widget.Editor, 1)
	editors[0].SetText(literal)

	loaded := map[string]string{flatKeyAuthPwd: literal}

	overrides := collectOverrides(entries, editors, loaded)

	if len(overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(overrides))
	}

	if overrides[0].Value != literal {
		t.Errorf("Value got split: want %q, got %q", literal, overrides[0].Value)
	}

	if overrides[0].LineComment != "" {
		t.Errorf("phantom LineComment: want empty, got %q", overrides[0].LineComment)
	}
}

// TestCollectOverrides_RoundTripEmptyAndNullValues verifies that legitimately
// empty YAML values (`""`, `null`/`~`) survive an unedited save. Without this
// path, collectOverrides drops every entry whose stripped editor text is
// empty — and the deletion phase in PatchNodeTree then removes the leaf,
// even when the source file had the key with an explicit empty value.
func TestCollectOverrides_RoundTripEmptyAndNullValues(t *testing.T) {
	t.Parallel()

	entries := []service.FlatValueEntry{
		{Key: flatKeyImageRegistry, Value: "", Type: typeString, Kind: service.EntryKindLeaf},
		{Key: flatKeyUsePasswordFls, Value: "", Type: typeNull, Kind: service.EntryKindLeaf},
	}

	editors := make([]widget.Editor, 2)
	// Loaded editor text mirrors what formatCommentForEditor produces for
	// these cases — empty Value with no head comment yields empty editor text.
	editors[0].SetText("")
	editors[1].SetText("")

	loaded := map[string]string{
		flatKeyImageRegistry:  "",
		flatKeyUsePasswordFls: "",
	}

	overrides := collectOverrides(entries, editors, loaded)

	if len(overrides) != 2 {
		t.Fatalf("expected 2 overrides, got %d", len(overrides))
	}

	keys := make([]string, len(overrides))
	for i, o := range overrides {
		keys[i] = o.Key
	}

	if !contains(keys, flatKeyImageRegistry) {
		t.Errorf("empty-string entry dropped: %v", keys)
	}

	if !contains(keys, flatKeyUsePasswordFls) {
		t.Errorf("null entry dropped: %v", keys)
	}
}

// TestCollectOverrides_EmptyEditorWithNonEmptyDefaultDropped verifies the
// inverse: when the user clears a cell that had a non-empty loaded value,
// the entry IS dropped (matching the prior unfilled-override-cell semantics).
// Without this, every chart default would round-trip as an override even
// when the user never typed anything.
func TestCollectOverrides_EmptyEditorWithNonEmptyDefaultDropped(t *testing.T) {
	t.Parallel()

	entries := []service.FlatValueEntry{
		{Key: flatKeyImageTag, Value: flatKeyImageTagValue, Type: typeString, Kind: service.EntryKindLeaf},
	}

	editors := make([]widget.Editor, 1)
	editors[0].SetText("")

	// loadedValues has a non-empty value for image.tag — empty editor
	// against a non-empty loaded value reads as "user cleared this", which
	// should still drop the entry.
	loaded := map[string]string{flatKeyImageTag: flatKeyImageTagValue}

	overrides := collectOverrides(entries, editors, loaded)

	if len(overrides) != 0 {
		t.Errorf("cleared cell should drop entry; got %d overrides", len(overrides))
	}
}

// TestCollectOverrides_ChartMergedAliasPreserved simulates the bug where the
// unified entries list mixes chart defaults with a loaded override file. The
// chart's default for an aliased key (`auth.password`) holds a different value
// than what the loaded file resolved the alias to. Without the loadedValues
// signal, the editor text (loaded value) doesn't match entries[i].Value (chart
// default), the splitter cuts on " #", and PatchNodeTree breaks the alias.
//
// With loadedValues passed through, the comparison happens against the loaded
// value — which matches the editor text — so the splitter is skipped.
func TestCollectOverrides_ChartMergedAliasPreserved(t *testing.T) {
	t.Parallel()

	const (
		loadedAliased = "s3cr3t # not really a comment"
		chartDefault  = "supersecret"
	)

	entries := []service.FlatValueEntry{
		// entries[i].Value is the chart default (RebuildUnifiedEntries
		// keeps defaults' Value when both sources have the key).
		{Key: flatKeyAuthPwd, Value: chartDefault, Type: typeString, Kind: service.EntryKindLeaf},
	}

	editors := make([]widget.Editor, 1)
	editors[0].SetText(loadedAliased)

	// loadedValues reflects what the user's loaded file actually contains
	// at this key (the alias-resolved value).
	loaded := map[string]string{flatKeyAuthPwd: loadedAliased}

	overrides := collectOverrides(entries, editors, loaded)

	if len(overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(overrides))
	}

	if overrides[0].Value != loadedAliased {
		t.Errorf("alias-aware comparison failed: want %q, got %q", loadedAliased, overrides[0].Value)
	}

	if overrides[0].LineComment != "" {
		t.Errorf("phantom LineComment: %q", overrides[0].LineComment)
	}
}

// TestCollectOverrides_EmptyDefaultDropped verifies the chart-default leak
// fix: when an entry has empty editor text AND the key is NOT in the
// loaded-values map (e.g. a chart default the user's file doesn't override),
// the entry is dropped — without this, every empty/null chart default would
// flood the saved file as a phantom override.
func TestCollectOverrides_EmptyDefaultDropped(t *testing.T) {
	t.Parallel()

	entries := []service.FlatValueEntry{
		{Key: "auth.acl.userSecret", Value: "", Type: typeString, Kind: service.EntryKindLeaf},
		{Key: "master.persistence.medium", Value: "", Type: typeString, Kind: service.EntryKindLeaf},
	}

	editors := make([]widget.Editor, 2)
	editors[0].SetText("")
	editors[1].SetText("")

	// loadedValues has neither key — these are chart defaults the loaded
	// file doesn't touch.
	loaded := map[string]string{"some.other.key": "x"}

	overrides := collectOverrides(entries, editors, loaded)

	if len(overrides) != 0 {
		t.Errorf("chart defaults shouldn't leak into save; got %d overrides", len(overrides))
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}

	return false
}

// TestCollectOverrides_MultiLineLiteralBlockPreservesHashContent guards the
// editor-text round-trip for multi-line literal-block values whose own
// content starts with `#` — e.g. a `configuration: |` cell carrying a
// pasted redis.conf block. StripYAMLComments greedily eats every leading
// line that starts with `#` from the editor text, so without the
// raw==loadForm fast path it would consume the literal first line of the
// block as a YAML head comment, then PatchNodeTree would write the
// truncated value back AND hoist the eaten line to a YAML head comment
// outside the literal block.
//
// The expected behaviour: when the editor text equals exactly what the
// load step wrote (head comment block, then the loaded value), the saved
// entry uses entry.Comment / entry.Value verbatim with no stripping.
func TestCollectOverrides_MultiLineLiteralBlockPreservesHashContent(t *testing.T) {
	t.Parallel()

	const (
		key = "master.configuration"
		// Literal-block content — note both leading and mid lines start
		// with `#`. These are part of the string, not YAML comments.
		literalValue = "# Pasted verbatim from redis.conf.\n" +
			"maxmemory-policy allkeys-lru\n" +
			"# Comment inside a block scalar is part of the string, not YAML.\n" +
			"appendonly yes\n" +
			"save \"\"\n"
		headComment = "Block literal: preserves every newline and comment inside exactly."
	)

	entries := []service.FlatValueEntry{
		{Key: key, Value: literalValue, Type: typeString, Comment: headComment, Kind: service.EntryKindLeaf},
	}

	// Editor text mirrors what populateColumnOverrides → resolveCustomValueWithComments
	// writes: "# {comment}\n{value}" via formatCommentForEditor.
	loadForm := "# " + headComment + "\n" + literalValue

	editors := make([]widget.Editor, 1)
	editors[0].SetText(loadForm)

	loaded := map[string]string{key: literalValue}

	overrides := collectOverrides(entries, editors, loaded)

	if len(overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(overrides))
	}

	if overrides[0].Value != literalValue {
		t.Errorf("literal-block content lost or truncated:\ngot  %q\nwant %q", overrides[0].Value, literalValue)
	}

	if overrides[0].HeadComment != headComment {
		t.Errorf("head comment garbled:\ngot  %q\nwant %q", overrides[0].HeadComment, headComment)
	}

	if overrides[0].LineComment != "" {
		t.Errorf("phantom LineComment: %q", overrides[0].LineComment)
	}
}

// TestRebuildUnifiedEntries_CustomOnlyEmptyContainerPreserved is the
// regression for the custom-only empty-container drop. RebuildUnifiedEntries
// blanks Value on custom-only entries so the defaults-side cell renders
// empty — but for empty-container leaves (`Value="{}"` / `Value="[]"`) that
// blanking turns the row into a section-shaped entry (`Value=""` with
// typeMap/typeList), which IsSection() then misclassifies as a populated
// section header. collectOverrides drops sections, and PatchNodeTree's
// deletion phase removes the leaf from the saved file.
//
// The fix preserves Value when it's the empty-container placeholder; this
// test guards both the typeMap and typeList shapes.
func TestRebuildUnifiedEntries_CustomOnlyEmptyContainerPreserved(t *testing.T) {
	t.Parallel()

	const (
		mapKey  = "cornerCases.quotedKeys.extraPolicies"
		listKey = "cornerCases.sequences.flowEmpty"
	)

	s := &ValuesPageState{
		DefaultValues: &service.FlatValues{
			Entries: []service.FlatValueEntry{
				{Key: "image.tag", Value: flatKeyImageTagValue, Type: typeString, Kind: service.EntryKindLeaf},
			},
		},
		ColumnCount: 1,
	}

	s.Columns[0].CustomValues = &service.FlatValues{
		Entries: []service.FlatValueEntry{
			{Key: mapKey, Value: emptyMapValue, Type: "map", Kind: service.EntryKindLeaf},
			{Key: listKey, Value: emptyListValue, Type: "list", Kind: service.EntryKindLeaf},
		},
	}

	s.RebuildUnifiedEntries()

	var (
		mapEntry  *service.FlatValueEntry
		listEntry *service.FlatValueEntry
	)

	for i := range s.Entries {
		switch s.Entries[i].Key {
		case mapKey:
			mapEntry = &s.Entries[i]
		case listKey:
			listEntry = &s.Entries[i]
		}
	}

	if mapEntry == nil {
		t.Fatalf("custom-only empty map dropped from unified entries")
	}

	if listEntry == nil {
		t.Fatalf("custom-only empty list dropped from unified entries")
	}

	if mapEntry.Value != emptyMapValue {
		t.Errorf("empty-map placeholder lost: got Value=%q, want %q", mapEntry.Value, emptyMapValue)
	}

	if listEntry.Value != emptyListValue {
		t.Errorf("empty-list placeholder lost: got Value=%q, want %q", listEntry.Value, emptyListValue)
	}

	if mapEntry.IsSection() {
		t.Errorf("empty-container leaf misclassified as section: %+v", *mapEntry)
	}

	if listEntry.IsSection() {
		t.Errorf("empty-container leaf misclassified as section: %+v", *listEntry)
	}
}
