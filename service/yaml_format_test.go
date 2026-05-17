package service

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Shared fixture-row constants reused across save-path tests. The redis
// cornercase file's `architecture` key (with `replication`/`standalone`
// as before/after values) and the smaller `replicaCount` leaf are the
// canonical hand to swap for "single-field edit" scenarios.
const (
	fixtureKeyArchitecture = "architecture"
	fixtureKeyReplicaCount = "replicaCount"
	fixtureValReplication  = "replication"
	fixtureValStandalone   = "standalone"
)

// lineDiffs returns one descriptor per line index where a and b differ.
// Both inputs are right-trimmed of trailing newlines so a present-vs-
// absent final newline doesn't register. Lines beyond one side's length
// compare against the empty string, producing a diff entry per missing
// line. Test-only helper.
func lineDiffs(a, b string) []string {
	aLines := strings.Split(strings.TrimRight(a, "\n"), "\n")
	bLines := strings.Split(strings.TrimRight(b, "\n"), "\n")

	n := len(aLines)
	if len(bLines) > n {
		n = len(bLines)
	}

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

// TestPatchSourceText_BOM_CRLF_RoundTripsThroughEncodeForFile pins down the
// full save contract for UTF-8-BOM + CRLF source files: after PatchSourceText
// returns and EncodeForFile re-applies the encoding labels stored on the
// column, the on-disk bytes must contain exactly one BOM and use \r\n line
// endings (not \r\r\n).
//
// The bug this guards against: PatchSourceText returning source bytes that
// already carry the BOM and CRLF, then EncodeForFile prepending a second
// BOM and rewriting each \n (which is already preceded by \r) to \r\n —
// producing a doubled-BOM file with \r\r\n separators.
//
// Both the splice-success path (value edit) and the no-edit fast path
// are checked because both routed source bytes verbatim through
// PatchSourceText before the fix.
func TestPatchSourceText_BOM_CRLF_RoundTripsThroughEncodeForFile(t *testing.T) {
	t.Parallel()

	const utf8BOM = "\xEF\xBB\xBF"

	rawText := utf8BOM + "architecture: replication\r\nreplicaCount: 2\r\n"
	raw := []byte(rawText)

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	if len(doc.Content) == 0 {
		t.Fatal("empty doc after parse")
	}

	root := doc.Content[0]

	cases := []struct {
		name      string
		entries   []OverrideEntry
		wantValue string
	}{
		{
			name: "no edits — fast path",
			entries: []OverrideEntry{
				{Key: fixtureKeyArchitecture, Value: fixtureValReplication, Type: typeString},
				{Key: fixtureKeyReplicaCount, Value: "2", Type: typeNumber},
			},
			wantValue: fixtureValReplication,
		},
		{
			name: "value edit — splice path",
			entries: []OverrideEntry{
				{Key: fixtureKeyArchitecture, Value: fixtureValStandalone, Type: typeString},
				{Key: fixtureKeyReplicaCount, Value: "2", Type: typeNumber},
			},
			wantValue: fixtureValStandalone,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := PatchSourceText(raw, root, tc.entries, DefaultYAMLIndent, DocComments{})
			if err != nil {
				t.Fatalf("PatchSourceText: %v", err)
			}

			onDisk := EncodeForFile(got, EncodingUTF8BOM, LineEndingCRLF)

			if !bytes.HasPrefix(onDisk, []byte(utf8BOM)) {
				t.Fatalf("on-disk bytes missing BOM prefix: %q", onDisk[:6])
			}

			if bytes.HasPrefix(onDisk[3:], []byte(utf8BOM)) {
				t.Fatalf("doubled BOM in on-disk bytes: %q", onDisk[:9])
			}

			if bytes.Contains(onDisk, []byte("\r\r\n")) {
				t.Fatalf("doubled CR in on-disk bytes: %q", onDisk)
			}

			if !bytes.Contains(onDisk, []byte(tc.wantValue+"\r\n")) {
				t.Fatalf("expected %q followed by CRLF in output, got:\n%q", tc.wantValue, onDisk)
			}
		})
	}
}

// TestSingleFieldEditYieldsSingleLineDiff loads the cornercase fixture
// (../assets/test-data/redis-values-cornercases.yaml), changes one
// plain scalar value via the same save path the UI controller uses
// (PatchSourceText, which prefers leaf-line splice and falls back to
// PatchNodeTree + PreserveSourceFormatting), and asserts the result
// differs from the source by exactly one line.
//
// The build is realistic: it loads via ReadCustomValues to populate
// the same RawBytes / NodeTree / flat entry set the controller has at
// save time, copies every original leaf into the override list, and
// patches exactly one — mirroring "user opened a file, changed one
// field, hit save."
//
// This pins the production contract: a single-field edit through the
// real save path must produce a single-line diff. Failures here mean
// the splice precondition tightened or a regression has crept into
// PatchSourceText's encoder fallback.
func TestSingleFieldEditYieldsSingleLineDiff(t *testing.T) {
	t.Parallel()

	svc := NewValuesService()

	vf, err := svc.ReadCustomValues(context.Background(), fixturePath)
	if err != nil {
		t.Skipf("fixture not present or unreadable: %v", err)
	}

	if vf.NodeTree == nil {
		t.Fatal("NodeTree is nil — fixture failed to parse as yaml.Node")
	}

	if len(vf.RawBytes) == 0 {
		t.Fatal("RawBytes empty — read path didn't retain source bytes")
	}

	const (
		targetKey = fixtureKeyArchitecture
		oldValue  = fixtureValReplication
		newValue  = fixtureValStandalone
	)

	// Build the override list the controller would produce: every
	// loaded leaf entry, with the target key's value swapped. The
	// per-entry HeadComment carries the flattener's loaded comment
	// text so PatchSourceText's splice-viability check sees the same
	// comment shape PatchNodeTree would on the encoder path.
	entries := make([]OverrideEntry, 0, len(vf.Entries))
	patched := false

	for _, e := range vf.Entries {
		if e.Type == typeMap || e.Type == typeList {
			continue
		}

		value := e.Value
		if string(e.Key) == targetKey {
			if value != oldValue {
				t.Fatalf("fixture changed: %s is %q, expected %q", targetKey, value, oldValue)
			}

			value = newValue
			patched = true
		}

		entries = append(entries, OverrideEntry{
			Key:         string(e.Key),
			Value:       value,
			Type:        e.Type,
			HeadComment: e.Comment,
		})
	}

	if !patched {
		t.Fatalf("target key %q not present in flat entries", targetKey)
	}

	// Pass the loaded doc-level comments — same path the controller
	// takes via col.DocCommentsForSave(). The redis fixture has a
	// banner, trailer, and per-leaf foots; an earlier version of this
	// test passed DocComments{} and silently took the splice path that
	// the production save couldn't, masking the bug where any file with
	// a banner fell through to the encoder.
	docs := DocComments{
		Head:  vf.DocHeadComment,
		Foot:  vf.DocFootComment,
		Foots: vf.FootComments,
	}

	if !docsMatchSource(vf.RawBytes, docs) {
		oc, _ := parseOrphanComments(vf.RawBytes)
		t.Fatalf("docsMatchSource returned false for unedited load — splice will reject every save:\n"+
			"  docs.Head len=%d vs oc.DocHead len=%d match=%v\n"+
			"  docs.Foot len=%d vs oc.DocFoot len=%d match=%v\n"+
			"  docs.Foots len=%d vs oc.Foots len=%d\n"+
			"  docs.SectionHeads len=%d",
			len(docs.Head), len(oc.DocHead), docs.Head == oc.DocHead,
			len(docs.Foot), len(oc.DocFoot), docs.Foot == oc.DocFoot,
			len(docs.Foots), len(oc.Foots),
			len(docs.SectionHeads))
	}

	got, err := PatchSourceText(vf.RawBytes, vf.NodeTree, entries, vf.Indent, docs)
	if err != nil {
		t.Fatalf("PatchSourceText: %v", err)
	}

	diffs := lineDiffs(string(vf.RawBytes), got)
	if len(diffs) != 1 {
		t.Fatalf("expected exactly 1 line of diff, got %d:\n%s",
			len(diffs), strings.Join(diffs, "\n"))
	}

	if !strings.Contains(diffs[0], oldValue) || !strings.Contains(diffs[0], newValue) {
		t.Errorf("diff line doesn't describe the %s→%s rewrite:\n  %s",
			oldValue, newValue, diffs[0])
	}

	if !strings.Contains(diffs[0], targetKey) {
		t.Errorf("diff line doesn't reference target key %q:\n  %s", targetKey, diffs[0])
	}
}
