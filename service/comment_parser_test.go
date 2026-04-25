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

func TestCleanCommentForDisplay_StripsHashAndBlanks(t *testing.T) {
	t.Parallel()

	raw := "# foo\n#\n# bar\n#  \n# baz"
	got := CleanCommentForDisplay(raw)
	want := "foo\nbar\nbaz"

	if got != want {
		t.Errorf("CleanCommentForDisplay = %q, want %q", got, want)
	}
}

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	return out
}
