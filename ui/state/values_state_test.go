package state

import "testing"

const (
	keyMaster            = "master"
	keyMasterPersistence = "master.persistence"
)

func TestMarkNullified_LazyAllocAndSet(t *testing.T) {
	var c CustomColumnState

	if c.NullifiedKeys != nil {
		t.Fatalf("expected nil map before first MarkNullified")
	}

	c.MarkNullified("auth.password")

	if c.NullifiedKeys == nil {
		t.Fatalf("expected lazy allocation on first MarkNullified")
	}

	if !c.NullifiedKeys["auth.password"] {
		t.Errorf("expected auth.password to be flagged")
	}
}

func TestMarkNullified_EmptyKeyIgnored(t *testing.T) {
	var c CustomColumnState

	c.MarkNullified("")

	if c.NullifiedKeys != nil {
		t.Errorf("empty key should not trigger lazy alloc; got %v", c.NullifiedKeys)
	}
}

func TestIsNullifiedDirect_ExactMatchOnly(t *testing.T) {
	c := CustomColumnState{NullifiedKeys: map[string]bool{keyMasterPersistence: true}}

	if !c.IsNullifiedDirect(keyMasterPersistence) {
		t.Errorf("expected direct hit on exact key")
	}

	if c.IsNullifiedDirect("master.persistence.size") {
		t.Errorf("descendant should not be direct-nullified")
	}

	if c.IsNullifiedDirect(keyMaster) {
		t.Errorf("ancestor should not be direct-nullified")
	}
}

func TestIsNullifiedCovered_HitsAncestor(t *testing.T) {
	c := CustomColumnState{NullifiedKeys: map[string]bool{keyMasterPersistence: true}}

	cases := []struct {
		key  string
		want bool
	}{
		{keyMasterPersistence, true},              // direct
		{"master.persistence.size", true},         // child
		{"master.persistence.access.modes", true}, // grandchild
		{keyMaster, false},                        // ancestor of nullified key — not covered
		{"replica.persistence", false},            // sibling subtree
		{"", false},
	}

	for _, tc := range cases {
		if got := c.IsNullifiedCovered(tc.key); got != tc.want {
			t.Errorf("IsNullifiedCovered(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestIsNullifiedCovered_EmptyMap(t *testing.T) {
	var c CustomColumnState

	if c.IsNullifiedCovered("anything") {
		t.Errorf("empty NullifiedKeys should never report covered")
	}
}

func TestClearNullified_DropsKeyAndAncestors(t *testing.T) {
	c := CustomColumnState{NullifiedKeys: map[string]bool{
		keyMaster:            true,
		keyMasterPersistence: true,
		"replica":            true,
	}}

	c.ClearNullified("master.persistence.size")

	if c.NullifiedKeys[keyMaster] {
		t.Errorf("ClearNullified should drop ancestor %q", keyMaster)
	}

	if c.NullifiedKeys[keyMasterPersistence] {
		t.Errorf("ClearNullified should drop ancestor %q", keyMasterPersistence)
	}

	if !c.NullifiedKeys["replica"] {
		t.Errorf("ClearNullified should NOT touch unrelated key 'replica'")
	}
}

func TestClearNullified_NoOpOnEmpty(t *testing.T) {
	var c CustomColumnState

	c.ClearNullified("anything") // should not panic, should not alloc.

	if c.NullifiedKeys != nil {
		t.Errorf("ClearNullified on nil map should stay nil")
	}
}

func TestClearNullified_EmptyKeyIgnored(t *testing.T) {
	c := CustomColumnState{NullifiedKeys: map[string]bool{keyMaster: true}}

	c.ClearNullified("")

	if !c.NullifiedKeys[keyMaster] {
		t.Errorf("ClearNullified('') should be a no-op")
	}
}

func TestClearNullified_HonorsFlatKeyEscapes(t *testing.T) {
	// Quoted segment with embedded dot — Parent walk must stop at the
	// quoted boundary and NOT split on the inner dot.
	c := CustomColumnState{NullifiedKeys: map[string]bool{
		`weird."foo.bar"`: true,
	}}

	c.ClearNullified(`weird."foo.bar".child`)

	if c.NullifiedKeys[`weird."foo.bar"`] {
		t.Errorf("escape-aware ancestor walk should have dropped weird.\"foo.bar\"")
	}
}
