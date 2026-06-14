package domain

// WindowGeometry is the persisted main-window geometry, restored on the next
// launch.
//
// Sizes are stored in density-independent pixels (Dp), not device pixels, so
// they round-trip correctly across monitors with different DPI scaling — a
// window sized on a 2x display reopens at the same logical size on a 1x one.
//
// Position is intentionally absent: Gio's cross-platform window Config exposes
// only the window's size and mode, not its on-screen location, so location
// cannot be captured or restored through the portable API.
type WindowGeometry struct {
	WidthDp   int  `json:"widthDp"`
	HeightDp  int  `json:"heightDp"`
	Maximized bool `json:"maximized"`
}
