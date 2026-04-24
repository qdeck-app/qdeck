//go:build !darwin

package font

import _ "embed"

//go:embed FiraCode-Medium.ttf
var fontData []byte
