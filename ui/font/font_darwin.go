//go:build darwin && !ios

package font

import _ "embed"

//go:embed FiraCode-Retina.ttf
var fontData []byte
