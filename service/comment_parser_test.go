package service

import (
	"strings"
	"testing"
)

func TestParseOrphanComments_Banner(t *testing.T) {
	t.Parallel()

	src := `# Top banner
# spans two lines

key: value
`

	oc, err := parseOrphanComments([]byte(src))
	if err != nil {
		t.Fatalf("parseOrphanComments: %v", err)
	}

	if oc.DocHead == "" {
		t.Fatalf("expected non-empty DocHead, got empty")
	}

	if !strings.Contains(oc.DocHead, "Top banner") || !strings.Contains(oc.DocHead, "spans two lines") {
		t.Fatalf("DocHead missing banner content: %q", oc.DocHead)
	}
}

func TestParseOrphanComments_FootBetweenSiblings(t *testing.T) {
	t.Parallel()

	src := `auth:
  enabled: true
  password: secret
  # trailing block on auth.password — belongs to auth, not commonLabels

commonLabels:
  app: foo
`

	oc, err := parseOrphanComments([]byte(src))
	if err != nil {
		t.Fatalf("parseOrphanComments: %v", err)
	}

	got, ok := oc.Foots["auth.password"]
	if !ok {
		t.Fatalf("expected Foots[auth.password], got keys: %v", mapKeys(oc.Foots))
	}

	if !strings.Contains(got, "trailing block") {
		t.Fatalf("Foots[auth.password] missing expected text: %q", got)
	}
}

func TestParseOrphanComments_NoComments(t *testing.T) {
	t.Parallel()

	src := `auth:
  enabled: true
  password: secret
`

	oc, err := parseOrphanComments([]byte(src))
	if err != nil {
		t.Fatalf("parseOrphanComments: %v", err)
	}

	if oc.DocHead != "" {
		t.Errorf("expected empty DocHead, got: %q", oc.DocHead)
	}

	if oc.DocFoot != "" {
		t.Errorf("expected empty DocFoot, got: %q", oc.DocFoot)
	}

	if len(oc.Foots) != 0 {
		t.Errorf("expected empty Foots, got: %v", oc.Foots)
	}
}

func TestParseOrphanComments_Empty(t *testing.T) {
	t.Parallel()

	oc, err := parseOrphanComments(nil)
	if err != nil {
		t.Fatalf("parseOrphanComments(nil): %v", err)
	}

	if oc.DocHead != "" || oc.DocFoot != "" || len(oc.Foots) != 0 {
		t.Errorf("expected empty OrphanComments for nil input, got %+v", oc)
	}
}

func TestCleanCommentForDisplay_PreservesInteriorBlanks(t *testing.T) {
	t.Parallel()

	raw := "# foo\n#\n# bar\n#  \n# baz"
	got := CleanCommentForDisplay(raw)
	want := "foo\n\nbar\n\nbaz"

	if got != want {
		t.Errorf("CleanCommentForDisplay = %q, want %q", got, want)
	}
}

func TestCleanCommentForDisplay_TrimsLeadingTrailingBlanks(t *testing.T) {
	t.Parallel()

	raw := "#\n# foo\n# bar\n#"
	got := CleanCommentForDisplay(raw)
	want := "foo\nbar"

	if got != want {
		t.Errorf("CleanCommentForDisplay = %q, want %q", got, want)
	}
}

func TestCleanCommentForDisplay_RoundTripIdempotent(t *testing.T) {
	t.Parallel()

	// A blank line between paragraphs survives clean → format → clean so the
	// editable comment surfaces preserve user-authored paragraph breaks.
	raw := "# header line\n#\n# body line"

	cleaned := CleanCommentForDisplay(raw)
	formatted := FormatCommentForYAML(cleaned)
	recleaned := CleanCommentForDisplay(formatted)

	if recleaned != cleaned {
		t.Errorf("round-trip mismatch: cleaned=%q, formatted=%q, recleaned=%q", cleaned, formatted, recleaned)
	}

	if formatted != raw {
		t.Errorf("formatted form drifts from raw: got %q, want %q", formatted, raw)
	}
}

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}
