//go:build (linux || freebsd || openbsd) && !android

package nativedrop

import "gioui.org/io/event"

type platformState struct{}

func newPlatformState(_ *Target) platformState {
	return platformState{}
}

// ListenEvents is a no-op on Linux/BSD.
// Neither X11 (no XDND) nor Wayland (Gio v0.9.0 discards DnD data offers)
// supports drag-and-drop, so DropSupported stays false.
func (t *Target) ListenEvents(_ event.Event) {}

// Close is a no-op on Linux/BSD (no platform resources to release).
func (t *Target) Close() {}
