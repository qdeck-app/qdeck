//go:build ios || !(darwin || windows)

package nativedrop

import "gioui.org/io/event"

// Linux/BSD (no XDND on X11, Gio v0.9.0 discards Wayland DnD data offers) and
// mobile targets have no working native drag-and-drop implementation, so
// DropSupported stays false and these entry points are no-ops.
//
// The `ios ||` guard is needed because GOOS=ios sets both the `darwin` and
// `ios` build tags; without it this file would be excluded on iOS and leave
// the package without a platformState implementation.

type platformState struct{}

func newPlatformState(_ *Target) platformState {
	return platformState{}
}

// ListenEvents is a no-op on platforms without native drag-and-drop support.
func (t *Target) ListenEvents(_ event.Event) {}

// Close releases no platform resources.
func (t *Target) Close() {}
