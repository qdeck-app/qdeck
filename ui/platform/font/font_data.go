package font

import _ "embed"

// Three Fira Code weights are embedded on every platform. The design spec
// uses 400 (default), 500 (section keys, override values, primary
// buttons), and 600 (brand wordmark only). Embedding all three avoids
// the per-OS split that previously baked exactly one weight per build.
//
// Bundle size grows by ~700 KB. Fira Code is OFL-licensed; the existing
// LICENSE file in this directory covers redistribution.

//go:embed FiraCode-Regular.ttf
var fontDataRegular []byte

//go:embed FiraCode-Medium.ttf
var fontDataMedium []byte

//go:embed FiraCode-SemiBold.ttf
var fontDataSemiBold []byte

// fontDataAll is the ordered slice of embedded TTF byte buffers parsed
// by font.go's init. Order is informational only — opentype reads each
// font's weight metadata to populate FontFace.Font.Weight.
var fontDataAll = [][]byte{
	fontDataRegular,
	fontDataMedium,
	fontDataSemiBold,
}
