//go:build !darwin || ios

package font

import _ "embed"

//go:embed FiraCode-Medium.ttf
var fontData []byte
