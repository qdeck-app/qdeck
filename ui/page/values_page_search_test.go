package page

import "testing"

func TestWordBoundaries(t *testing.T) {
	const (
		sample      = "hello world"
		dotted      = "foo.bar"
		spaced      = "foo   "
		underscored = "a_b cd"
	)

	cases := []struct {
		name string
		fn   func([]rune, int) int
		text string
		pos  int
		want int
	}{
		{"prev/empty", prevWordBoundary, "", 0, 0},
		{"prev/at start", prevWordBoundary, sample, 0, 0},
		{"prev/end skips last word", prevWordBoundary, sample, 11, 6},
		{"prev/mid word to word start", prevWordBoundary, sample, 9, 6},
		{"prev/word start crosses space+word", prevWordBoundary, sample, 6, 0},
		{"prev/leading spaces to start", prevWordBoundary, "  foo", 2, 0},
		{"prev/trailing spaces then word", prevWordBoundary, spaced, 6, 0},
		{"prev/punctuation run", prevWordBoundary, dotted, 7, 4},
		{"prev/stops at punctuation", prevWordBoundary, dotted, 4, 3},
		{"prev/underscore is word rune", prevWordBoundary, underscored, 3, 0},

		{"next/empty", nextWordBoundary, "", 0, 0},
		{"next/at end", nextWordBoundary, sample, 11, 11},
		{"next/start skips first word", nextWordBoundary, sample, 0, 5},
		{"next/mid word to word end", nextWordBoundary, sample, 2, 5},
		{"next/space crosses space+word", nextWordBoundary, sample, 5, 11},
		{"next/trailing spaces to end", nextWordBoundary, spaced, 3, 6},
		{"next/punctuation run", nextWordBoundary, dotted, 0, 3},
		{"next/stops at punctuation", nextWordBoundary, dotted, 3, 4},
		{"next/underscore is word rune", nextWordBoundary, underscored, 0, 3},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.fn([]rune(c.text), c.pos); got != c.want {
				t.Errorf("%s([]rune(%q), %d) = %d, want %d", c.name, c.text, c.pos, got, c.want)
			}
		})
	}
}
