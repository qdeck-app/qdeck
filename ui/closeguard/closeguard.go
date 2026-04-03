package closeguard

import (
	"sync/atomic"

	"gioui.org/app"
)

const closeChanSize = 1

// CloseGuard intercepts the native window close button and delivers
// close-attempted signals to the Gio event loop via a buffered channel.
//
// It must be created exactly once per application lifetime. The guard
// relies on global state (method swizzling on macOS, window procedure
// subclassing on Windows), so multiple instances would conflict.
type CloseGuard struct {
	window   *app.Window
	closes   chan struct{}
	closed   atomic.Bool
	platform platformState
}

func New(w *app.Window) *CloseGuard {
	g := &CloseGuard{
		window: w,
		closes: make(chan struct{}, closeChanSize),
	}

	g.platform = newPlatformState(g)

	return g
}

// PollCloseAttempt returns true if the user tried to close the window
// while the guard was active. Returns false after Close has been called.
func (g *CloseGuard) PollCloseAttempt() bool {
	if g.closed.Load() {
		return false
	}

	select {
	case <-g.closes:
		return true
	default:
		return false
	}
}

// sendCloseAttempt sends a close-attempted signal non-blockingly and
// invalidates the window so the next frame can show the confirmation dialog.
// Invalidate runs in a separate goroutine because sendCloseAttempt may be
// called from a CGo callback or Windows callback on the main thread.
// No-op after Close has been called.
func (g *CloseGuard) sendCloseAttempt() {
	if g.closed.Load() {
		return
	}

	select {
	case g.closes <- struct{}{}:
	default:
	}

	go g.window.Invalidate()
}
