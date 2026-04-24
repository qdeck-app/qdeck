//go:build ios || !(darwin || windows)

package closeguard

import "gioui.org/io/event"

// Close interception has no cross-platform implementation on Linux/BSD or
// mobile: Wayland's xdg-shell has no close-veto, Gio's X11 WM_DELETE_WINDOW
// handler calls shutdown() synchronously at the C level before Go code can
// intervene, and iOS/Android have no concept of user-initiated window close
// to veto. Intercepting would require patching Gio itself.
//
// The `ios ||` guard is needed because GOOS=ios sets both the `darwin` and
// `ios` build tags; without it this file would be excluded on iOS and leave
// the package without a platformState implementation.

type platformState struct{}

func newPlatformState(_ *CloseGuard) platformState {
	return platformState{}
}

// ListenEvents is a no-op on unsupported platforms.
func (g *CloseGuard) ListenEvents(_ event.Event) {}

// IsSupported reports whether close interception is available on this platform.
func (g *CloseGuard) IsSupported() bool { return false }

// SetGuarded is a no-op on unsupported platforms.
func (g *CloseGuard) SetGuarded(_ bool) {}

// Close releases no platform resources but flips the closed flag for
// consistency with supported platforms.
func (g *CloseGuard) Close() { g.closed.CompareAndSwap(false, true) }
