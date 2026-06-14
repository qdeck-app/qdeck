// Package screen reports primary-display metrics that Gio's portable window
// API doesn't expose, so the app can pick a sensible default window size before
// the window (and thus its monitor) exists.
package screen

// WorkAreaDp returns the primary monitor's usable work area — the desktop minus
// the taskbar/dock — in density-independent pixels. ok is false when the value
// can't be determined (every platform except Windows today, plus Windows query
// failures); callers should fall back to a fixed default size.
func WorkAreaDp() (widthDp, heightDp int, ok bool) {
	return workAreaDp()
}
