package theme

import (
	"gioui.org/text"
	"gioui.org/widget/material"

	"github.com/qdeck-app/qdeck/ui/platform/font"
)

func NewTheme() *material.Theme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(font.Collection()))
	th.Face = font.Typeface

	// Material's "contrast" pair is the inverted surface: Ink (dark) becomes
	// the contrast background and Bg (near-white) becomes the contrast
	// foreground — i.e. dark chip on a light page, with light text on it.
	th.ContrastBg = Default.Ink
	th.ContrastFg = Default.Bg

	return th
}
