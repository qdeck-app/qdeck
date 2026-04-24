package font

import (
	"gioui.org/font"
	"gioui.org/font/opentype"
)

// Typeface is the registered name for the embedded font. Using a distinct
// name prevents system-installed variants of the same family (e.g. Fira
// Code Regular) from winning the fontscan weight match over the embedded
// file. System fonts remain available as glyph fallback for Unicode
// characters outside the embedded font's coverage.
const Typeface font.Typeface = "QDeck Mono"

//nolint:gochecknoglobals // parsed once at init from embedded data
var collection []font.FontFace

func init() { //nolint:gochecknoinits // parsed once from embedded font data; trivial and side-effect-free
	faces, err := opentype.ParseCollection(fontData)
	if err != nil {
		panic("failed to parse font: " + err.Error())
	}

	for i := range faces {
		faces[i].Font.Typeface = Typeface
	}

	collection = faces
}

func Collection() []font.FontFace {
	return collection
}
