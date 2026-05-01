package font

import (
	"testing"

	"gioui.org/font"
)

// TestCollectionHasThreeWeights asserts the embedded font collection
// exposes all three weights the design system expects (400, 500, 600).
// Catches a regression where one of the //go:embed directives loses its
// file or where opentype fails to read weight metadata from a future
// Fira Code release.
func TestCollectionHasThreeWeights(t *testing.T) {
	want := map[font.Weight]string{
		font.Normal:   "Regular (400)",
		font.Medium:   "Medium (500)",
		font.SemiBold: "SemiBold (600)",
	}

	got := make(map[font.Weight]bool)

	for _, face := range Collection() {
		got[face.Font.Weight] = true

		if face.Font.Typeface != Typeface {
			t.Errorf("face %+v: Typeface = %q, want %q", face.Font, face.Font.Typeface, Typeface)
		}
	}

	for w, label := range want {
		if !got[w] {
			t.Errorf("missing weight %s in Collection()", label)
		}
	}
}
