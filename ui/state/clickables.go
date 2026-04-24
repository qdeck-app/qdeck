package state

import "gioui.org/widget"

// growClickables extends each slice to at least count elements, only
// allocating when existing capacity is insufficient. Widget state is
// pre-allocated across frames, so this runs at most a few times as data grows.
func growClickables(count int, slices ...*[]widget.Clickable) {
	for _, s := range slices {
		for len(*s) < count {
			*s = append(*s, widget.Clickable{})
		}
	}
}
