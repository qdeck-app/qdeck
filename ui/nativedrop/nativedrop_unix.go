//go:build (linux || freebsd || openbsd) && !android

package nativedrop

import (
	"gioui.org/app"
	"gioui.org/io/event"
)

type platformState struct{}

func newPlatformState(_ *Target) platformState {
	return platformState{}
}

// ListenEvents detects whether the display server supports drag-and-drop.
// Wayland supports DnD via Gio's built-in transfer events.
// X11 does not (Gio v0.9.0 has no XDND implementation).
func (t *Target) ListenEvents(evt event.Event) {
	if _, ok := evt.(app.WaylandViewEvent); ok {
		t.DropSupported = true
	}
}

// Close is a no-op on Linux/BSD (no platform resources to release).
func (t *Target) Close() {}
