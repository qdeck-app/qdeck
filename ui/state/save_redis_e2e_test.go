package state

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"gioui.org/widget"

	"github.com/qdeck-app/qdeck/service"
)

const redisFixturePath = "../../assets/test-data/redis-values-cornercases.yaml"

// TestSaveRedisFixture_SingleFieldEdit_OneLineDiff is the production-faithful
// end-to-end test for the save path. It loads the cornercase fixture through
// the SAME entrypoint the controller uses (LoadAndMergeCustomValues), then
// reparses the bytes through ParseEditorContent to mimic the keystroke-time
// onChange cycle — preserving load-time metadata via the carry-over the
// controller performs at ui/page/values_controller.go pollEditorParse —
// and finally runs the result through OverridesToYAML exactly as
// onSaveColumnValues does.
//
// Why the load path matters: LoadAndMergeCustomValues routes through
// ReadAndMergeCustomValues even for a single file. An earlier iteration
// only retained RawBytes in ReadCustomValues, so production loads
// silently lost the source bytes the splice path needs. This test calls
// the real load path so that regression can't slip back in.
func TestSaveRedisFixture_SingleFieldEdit_OneLineDiff(t *testing.T) {
	t.Parallel()

	svc := service.NewValuesService()

	loaded, err := svc.LoadAndMergeCustomValues(context.Background(), []string{redisFixturePath})
	if err != nil {
		t.Skipf("fixture not present or unreadable: %v", err)
	}

	if len(loaded.RawBytes) == 0 {
		t.Fatal("LoadAndMergeCustomValues didn't retain RawBytes for single-file load — splice will always fail in production")
	}

	// Mimic the controller's editor-reparse cycle: ParseEditorContent
	// returns a fresh FlatValues from raw text, and the controller's
	// pollEditorParse calls MergeLoadedMetadata to carry forward
	// load-time data (NodeTree, Indent, doc comments, RawBytes,
	// per-entry head comments) from the previous CustomValues.
	flat, err := svc.ParseEditorContent(context.Background(), string(loaded.RawBytes))
	if err != nil {
		t.Fatalf("ParseEditorContent: %v", err)
	}

	service.MergeLoadedMetadata(flat, loaded)

	const (
		targetKey = "architecture"
		oldValue  = "replication"
		newValue  = "standalone"
	)

	loadedValues := LoadedValuesMap(flat)

	editors := make([]widget.Editor, len(flat.Entries))

	patched := false

	for i := range flat.Entries {
		e := flat.Entries[i]

		var text string

		switch {
		case !e.IsFocusable():
			text = e.Comment
		case e.Key == targetKey:
			if e.Value != oldValue {
				t.Fatalf("fixture changed: %s is %q, expected %q", targetKey, e.Value, oldValue)
			}

			text = loadFormForEditor(e.Comment, newValue)
			patched = true
		default:
			text = loadFormForEditor(e.Comment, loadedValues[e.Key])
		}

		editors[i].SetText(text)
	}

	if !patched {
		t.Fatalf("target key %q not present in flat entries", targetKey)
	}

	docs := service.DocComments{
		Head:         flat.DocHeadComment,
		Foot:         flat.DocFootComment,
		Foots:        flat.FootComments,
		SectionHeads: flat.SectionHeads,
	}

	// First OverridesToYAML — same as the keystroke-time onChange path
	// at ui/widget/override_table_events.go: rebuilds yaml from the
	// editor state and feeds it to ParseEditorContent. Any reformatting
	// that creeps in here gets baked into the post-keystroke col state.
	keystrokeYAML, err := OverridesToYAML(
		flat.Entries, editors, flat.Indent, flat.NodeTree, docs, loadedValues, nil, flat.RawBytes,
	)
	if err != nil {
		t.Fatalf("keystroke OverridesToYAML: %v", err)
	}

	// Sanity check: the keystroke output must already be a one-line diff —
	// otherwise the reparse cycle bakes encoder normalization into the
	// post-keystroke state and the save call can never recover it.
	keystrokeDiffs := lineDiffs(string(flat.RawBytes), keystrokeYAML)
	if len(keystrokeDiffs) != 1 {
		overrides := collectOverrides(flat.Entries, editors, loadedValues, nil)

		_, ok := service.PlanScalarSpliceForTest(flat.RawBytes, flat.NodeTree, overrides, docs)
		t.Fatalf("keystroke YAML has %d diff lines (expected 1); splice viable: %v",
			len(keystrokeDiffs), ok)
	}

	// Simulate the EditorParseRunner cycle: ParseEditorContent reads
	// keystrokeYAML and returns a fresh FlatValues; the controller's
	// pollEditorParse then patches load-time metadata back onto it.
	reparsed, err := svc.ParseEditorContent(context.Background(), keystrokeYAML)
	if err != nil {
		t.Fatalf("ParseEditorContent of keystroke YAML: %v", err)
	}

	service.MergeLoadedMetadata(reparsed, loaded)

	// Re-build editors against the reparsed entries. In production the
	// editors aren't reset on reparse — they keep the user's text — but
	// the unified entry list IS rebuilt, so the index/key alignment
	// drifts. Re-binding here mirrors that.
	postKeystrokeEditors := make([]widget.Editor, len(reparsed.Entries))
	postKeystrokeLoaded := LoadedValuesMap(reparsed)

	for i := range reparsed.Entries {
		e := reparsed.Entries[i]

		var text string

		switch {
		case !e.IsFocusable():
			text = e.Comment
		case e.Key == targetKey:
			text = loadFormForEditor(e.Comment, newValue)
		default:
			text = loadFormForEditor(e.Comment, postKeystrokeLoaded[e.Key])
		}

		postKeystrokeEditors[i].SetText(text)
	}

	postKeystrokeDocs := service.DocComments{
		Head:         reparsed.DocHeadComment,
		Foot:         reparsed.DocFootComment,
		Foots:        reparsed.FootComments,
		SectionHeads: reparsed.SectionHeads,
	}

	// Save call — second OverridesToYAML. This is what hits disk.
	got, err := OverridesToYAML(
		reparsed.Entries, postKeystrokeEditors, reparsed.Indent, reparsed.NodeTree,
		postKeystrokeDocs, postKeystrokeLoaded, nil, reparsed.RawBytes,
	)
	if err != nil {
		t.Fatalf("save OverridesToYAML: %v", err)
	}

	diffs := lineDiffs(string(loaded.RawBytes), got)
	if len(diffs) != 1 {
		overrides := collectOverrides(reparsed.Entries, postKeystrokeEditors, postKeystrokeLoaded, nil)

		_, ok := service.PlanScalarSpliceForTest(reparsed.RawBytes, reparsed.NodeTree, overrides, postKeystrokeDocs)
		t.Fatalf("expected exactly 1 line of diff, got %d; splice viable: %v\n%s",
			len(diffs), ok, strings.Join(diffs, "\n"))
	}

	if !strings.Contains(diffs[0], oldValue) || !strings.Contains(diffs[0], newValue) {
		t.Errorf("diff line doesn't describe the %s→%s rewrite:\n  %s",
			oldValue, newValue, diffs[0])
	}
}

// TestSaveRedisFixture_AddingChartDefaultKey_NoUnrelatedDiff covers the
// scenario the user hit in production: toggling a bool switch on a
// chart-default-only key copies the chart default into the override
// editor, producing an OverrideEntry for a path that isn't in the
// custom file's tree. Splice can't handle adds, so the encoder runs,
// and earlier the encoder normalized inline-comment column alignment
// across every other leaf in the file.
//
// After substituteUnchangedLinesFromSource lands, the diff should be
// limited to the newly-added lines — every other line must be byte-
// identical with the source even though the encoder re-emitted them.
// "Limited to newly-added lines" means: every removed-line in the diff
// is a blank line we can re-insert, OR the removed lines correspond to
// folded scalars that the encoder canonicalizes; every added-line is
// either a folded scalar's single-line form or the genuinely new key.
//
// The test asserts a much looser bound than the value-edit test: at
// most a small number of diff lines, dominated by encoder-canonicalized
// folded scalars (a separate yaml.v3 limitation) plus the new key
// itself. If we regress on inline-comment alignment, the count balloons
// and the test fails.
func TestSaveRedisFixture_AddingChartDefaultKey_NoUnrelatedDiff(t *testing.T) {
	t.Parallel()

	svc := service.NewValuesService()

	loaded, err := svc.LoadAndMergeCustomValues(context.Background(), []string{redisFixturePath})
	if err != nil {
		t.Skipf("fixture not present or unreadable: %v", err)
	}

	loadedValues := LoadedValuesMap(loaded)

	editors := make([]widget.Editor, len(loaded.Entries)+1)

	for i := range loaded.Entries {
		e := loaded.Entries[i]

		var text string

		switch {
		case !e.IsFocusable():
			text = e.Comment
		default:
			text = loadFormForEditor(e.Comment, loadedValues[e.Key])
		}

		editors[i].SetText(text)
	}

	// Simulate the user toggling a bool switch on a chart-only key —
	// the cell handler writes the chart default into the override
	// editor (see ui/widget/override_table_cells.go layoutBoolSwitchCell).
	// We model this by appending an entry to the override list with the
	// editor pre-populated.
	const addedKey = "global.security.allowInsecureImages"

	entries := make([]service.FlatValueEntry, 0, len(loaded.Entries)+1)
	entries = append(entries, loaded.Entries...)
	entries = append(entries, service.FlatValueEntry{
		Key:   addedKey,
		Value: yamlTrueLiteralForTest,
		Type:  "bool",
	})
	editors[len(loaded.Entries)].SetText(yamlTrueLiteralForTest)

	docs := service.DocComments{
		Head:         loaded.DocHeadComment,
		Foot:         loaded.DocFootComment,
		Foots:        loaded.FootComments,
		SectionHeads: loaded.SectionHeads,
	}

	got, err := OverridesToYAML(
		entries, editors, loaded.Indent, loaded.NodeTree, docs, loadedValues, nil, loaded.RawBytes,
	)
	if err != nil {
		t.Fatalf("OverridesToYAML: %v", err)
	}

	// Set-based check: count source lines that DON'T appear anywhere in
	// the output. Insertion of the added key shifts line positions, so a
	// pure line-by-line diff would balloon with false positives — what
	// matters here is whether the post-process preserved every source
	// line somewhere in the output, not whether positions exactly align.
	unmatched := unmatchedSourceLines(string(loaded.RawBytes), got)

	// Allow a small budget for yaml.v3 emitter quirks we don't fix:
	// folded scalars collapse, !!set form converts, !!binary blank line.
	// The strict goal is ≤ 1 (only the empty `disableCommands: []`
	// could legitimately go missing if B's insertion misses an edge case).
	const maxAllowedUnmatched = 8
	if unmatched > maxAllowedUnmatched {
		t.Fatalf("expected ≤%d source lines missing from output, got %d", maxAllowedUnmatched, unmatched)
	}
}

// lineDiffs returns one descriptor per line index where a and b differ.
// Both inputs are right-trimmed of trailing newlines so a present-vs-
// absent final newline doesn't register. Lines beyond one side's length
// compare against the empty string, producing a diff entry per missing
// line.
func lineDiffs(a, b string) []string {
	aLines := strings.Split(strings.TrimRight(a, "\n"), "\n")
	bLines := strings.Split(strings.TrimRight(b, "\n"), "\n")

	n := max(len(bLines), len(aLines))

	var diffs []string

	for i := range n {
		var aL, bL string
		if i < len(aLines) {
			aL = aLines[i]
		}

		if i < len(bLines) {
			bL = bLines[i]
		}

		if aL != bL {
			diffs = append(diffs, fmt.Sprintf("line %d: %q -> %q", i+1, aL, bL))
		}
	}

	return diffs
}

// unmatchedSourceLines returns the count of source lines that don't
// appear (as exact string matches) anywhere in output. Used by the
// add-scenario test to assess source preservation without sensitivity
// to line-position shifts introduced by the encoder's added content.
func unmatchedSourceLines(src, out string) int {
	outLines := strings.Split(out, "\n")
	outCount := make(map[string]int, len(outLines))

	for _, l := range outLines {
		outCount[l]++
	}

	unmatched := 0

	for srcLine := range strings.SplitSeq(src, "\n") {
		if outCount[srcLine] > 0 {
			outCount[srcLine]--

			continue
		}

		unmatched++
	}

	return unmatched
}

const yamlTrueLiteralForTest = "true"
