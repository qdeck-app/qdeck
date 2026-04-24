//go:build darwin && !ios

package closeguard

/*
#cgo CFLAGS: -Werror -xobjective-c -fmodules -fobjc-arc

#import <AppKit/AppKit.h>

// Defined in closeguard_macos.m.
extern void installCloseGuard(CFTypeRef viewRef, uintptr_t handle);
extern void setCloseGuard(_Bool guarded);
*/
import "C"

import (
	"runtime/cgo"

	"gioui.org/app"
	"gioui.org/io/event"
)

type platformState struct {
	handle    cgo.Handle
	installed bool
}

func newPlatformState(g *CloseGuard) platformState {
	return platformState{
		handle: cgo.NewHandle(g),
	}
}

// ListenEvents must be called for every event from the Gio window
// in order to capture the AppKitViewEvent and install the close guard.
// No-op after Close has been called.
func (g *CloseGuard) ListenEvents(evt event.Event) {
	if g.closed.Load() {
		return
	}

	if e, ok := evt.(app.AppKitViewEvent); ok && e.View != 0 && !g.platform.installed {
		g.platform.installed = true
		C.installCloseGuard(C.CFTypeRef(e.View), C.uintptr_t(g.platform.handle))
	}
}

// IsSupported reports whether close interception is available on this platform.
func (g *CloseGuard) IsSupported() bool { return true }

// SetGuarded enables or disables the native window-close interception.
// When enabled, clicking the window's close button notifies Go instead of closing.
// No-op after Close has been called.
func (g *CloseGuard) SetGuarded(guarded bool) {
	if g.closed.Load() {
		return
	}

	C.setCloseGuard(C.bool(guarded))
}

// Close disables the ObjC-side guard and releases the cgo.Handle.
// Safe to call multiple times; only the first call releases resources.
func (g *CloseGuard) Close() {
	if !g.closed.CompareAndSwap(false, true) {
		return
	}

	// Disable the ObjC guard before deleting the handle so that
	// imp_windowShouldClose never calls closeGuardAttempted with
	// a deleted handle.
	C.setCloseGuard(C.bool(false))

	g.platform.handle.Delete()
}

//export closeGuardAttempted
func closeGuardAttempted(handle C.uintptr_t) {
	h := cgo.Handle(handle)
	g, ok := h.Value().(*CloseGuard)

	if !ok || g.closed.Load() {
		return
	}

	g.sendCloseAttempt()
}
