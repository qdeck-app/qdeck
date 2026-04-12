package font

import (
	"sync"

	"gioui.org/font"
	"gioui.org/font/opentype"
)

var (
	collection []font.FontFace
	once       sync.Once
)

func Collection() []font.FontFace {
	once.Do(func() {
		faces, err := opentype.ParseCollection(fontData)
		if err != nil {
			panic("failed to parse font: " + err.Error())
		}

		collection = append(collection, faces...)
	})

	return collection
}
