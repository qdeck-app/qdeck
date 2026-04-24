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

	th.ContrastBg = ColorContrastBg
	th.ContrastFg = ColorContrastFg

	return th
}
