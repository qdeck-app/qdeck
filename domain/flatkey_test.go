package domain

import "testing"

const (
	tkSingle    = "single"
	tkAB        = "a.b"
	tkABC       = "a.b.c"
	tkBackslash = `\`

	tkPlain     = "plainKey"
	tkSpaces    = "with spaces"
	tkColon     = "key:value"
	tkHash      = "value with # not a comment"
	tkSlashKey  = "costcenter/team"
	tkK8sLabel  = "app.kubernetes.io/part-of"
	tkK8sLabelE = `app\.kubernetes\.io/part-of`
	tkLabelsKey = `commonLabels.app\.kubernetes\.io/part-of`
	tkEscDot    = `a\.b`
	tkTrailing  = "trailing."
	tkFooBar0   = "foo.bar[0]"
)

func TestEscapeUnescapeRoundTrip(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", tkPlain, tkPlain},
		{"empty", "", ""},
		{"only dot", ".", `\.`},
		{"only backslash", tkBackslash, `\\`},
		{"only bracket", "[", `\[`},
		{"k8s label", tkK8sLabel, tkK8sLabelE},
		{"slash key", tkSlashKey, tkSlashKey},
		{"with spaces", tkSpaces, tkSpaces},
		{"colon", tkColon, tkColon},
		{"hash", tkHash, tkHash},
		{"mixed special", `a.b\c[d`, `a\.b\\c\[d`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := EscapeSegment(tc.in)
			if got != tc.want {
				t.Errorf("EscapeSegment(%q) = %q, want %q", tc.in, got, tc.want)
			}

			back, err := UnescapeSegment(got)
			if err != nil {
				t.Fatalf("UnescapeSegment(%q): %v", got, err)
			}

			if back != tc.in {
				t.Errorf("round-trip: got %q, want %q", back, tc.in)
			}
		})
	}
}

func TestUnescapeSegmentErrors(t *testing.T) {
	t.Parallel()

	bad := []string{
		tkBackslash, // dangling
		`abc\`,      // dangling at end
		`\x`,        // unknown escape
		`\n`,        // unknown escape (we don't recognise C-style escapes)
	}

	for _, in := range bad {
		if _, err := UnescapeSegment(in); err == nil {
			t.Errorf("UnescapeSegment(%q): expected error, got nil", in)
		}
	}
}

func TestFlatKeyDepth(t *testing.T) {
	t.Parallel()

	cases := []struct {
		key  FlatKey
		want int
	}{
		{"", 0},
		{tkSingle, 1},
		{tkAB, 2},
		{tkABC, 3},
		{tkEscDot, 1},
		{`a\.b.c`, 2},
		{tkLabelsKey, 2},
		{tkTrailing, 2},
		{"foo[0].bar", 2},
		{`a\\b.c`, 2},
	}

	for _, tc := range cases {
		got := tc.key.Depth()
		if got != tc.want {
			t.Errorf("FlatKey(%q).Depth() = %d, want %d", tc.key, got, tc.want)
		}
	}
}

// flatKeyMethodCase drives both Parent and LastSegment table tests through
// one shared loop so the two table-test bodies don't trip the dupl linter
// while still letting each method assert against its own expected output.
type flatKeyMethodCase struct {
	key      FlatKey
	parent   FlatKey
	lastSeg  string
	skipPart bool // skip Parent assertion (case is LastSegment-specific)
	skipLast bool // skip LastSegment assertion (case is Parent-specific)
}

func TestFlatKeyParentAndLastSegment(t *testing.T) {
	t.Parallel()

	cases := []flatKeyMethodCase{
		{key: "", parent: "", lastSeg: ""},
		{key: tkSingle, parent: "", lastSeg: tkSingle},
		{key: tkAB, parent: "a", lastSeg: "b"},
		{key: tkABC, parent: tkAB, lastSeg: "c"},
		{key: FlatKey(tkFooBar0), parent: "foo.bar", lastSeg: "bar[0]"},
		{key: "foo.bar[0].baz", parent: FlatKey(tkFooBar0), skipLast: true},
		{key: tkLabelsKey, parent: "commonLabels", lastSeg: tkK8sLabel},
		{key: tkEscDot, parent: "", lastSeg: "a.b"},
		{key: FlatKey(tkTrailing), parent: "trailing", lastSeg: ""},
		{key: `a.b\\c`, skipPart: true, lastSeg: `b\c`},
	}

	for _, tc := range cases {
		if !tc.skipPart {
			if got := tc.key.Parent(); got != tc.parent {
				t.Errorf("FlatKey(%q).Parent() = %q, want %q", tc.key, got, tc.parent)
			}
		}

		if !tc.skipLast {
			if got := tc.key.LastSegment(); got != tc.lastSeg {
				t.Errorf("FlatKey(%q).LastSegment() = %q, want %q", tc.key, got, tc.lastSeg)
			}
		}
	}
}
