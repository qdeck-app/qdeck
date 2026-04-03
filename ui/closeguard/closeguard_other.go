//go:build !(darwin || windows || linux || freebsd || openbsd) || ios || android

package closeguard

import "gioui.org/io/event"

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

// Close is a no-op on unsupported platforms.
// Sets the closed flag for consistency with other platforms.
func (g *CloseGuard) Close() { g.closed.CompareAndSwap(false, true) }
