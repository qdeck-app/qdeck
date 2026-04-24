//go:build (linux || freebsd || openbsd) && !android

package closeguard

import "gioui.org/io/event"

type platformState struct{}

func newPlatformState(_ *CloseGuard) platformState {
	return platformState{}
}

// ListenEvents is a no-op on Linux/BSD.
func (g *CloseGuard) ListenEvents(_ event.Event) {}

// IsSupported reports whether close interception is available on this platform.
// Linux/BSD do not support close interception.
func (g *CloseGuard) IsSupported() bool { return false }

// SetGuarded is a no-op on Linux/BSD.
// Wayland's xdg-shell protocol has no close-veto mechanism, and Gio's X11
// WM_DELETE_WINDOW handler calls shutdown() synchronously at the C level
// before Go code can intervene. Intercepting window close would require
// patching Gio itself.
func (g *CloseGuard) SetGuarded(_ bool) {}

// Close is a no-op on Linux/BSD (no platform resources to release).
// Sets the closed flag for consistency with other platforms.
func (g *CloseGuard) Close() { g.closed.CompareAndSwap(false, true) }
