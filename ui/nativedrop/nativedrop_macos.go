//go:build darwin && !ios

package nativedrop

/*
#cgo CFLAGS: -Werror -xobjective-c -fmodules -fobjc-arc

#import <AppKit/AppKit.h>

// Defined in nativedrop_macos.m.
extern void registerDropTarget(CFTypeRef viewRef, uintptr_t handle);
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"

	"gioui.org/app"
	"gioui.org/io/event"
)

type platformState struct {
	handle     cgo.Handle
	registered bool
	closed     bool
}

func newPlatformState(t *Target) platformState {
	return platformState{
		handle: cgo.NewHandle(t),
	}
}

// ListenEvents must be called for every event from the Gio window
// in order to capture the AppKitViewEvent and register the drop target.
func (t *Target) ListenEvents(evt event.Event) {
	if e, ok := evt.(app.AppKitViewEvent); ok && e.View != 0 && !t.platform.registered {
		t.platform.registered = true
		t.DropSupported = true

		C.registerDropTarget(C.CFTypeRef(e.View), C.uintptr_t(t.platform.handle))
	}
}

// Close releases the cgo.Handle allocated during construction.
// Safe to call more than once.
func (t *Target) Close() {
	if !t.platform.closed {
		t.platform.closed = true
		t.platform.handle.Delete()
	}
}

//export nativeDropCallback
func nativeDropCallback(handle C.uintptr_t, paths **C.char, count C.int) {
	if count <= 0 {
		return
	}

	h := cgo.Handle(handle)
	t := h.Value().(*Target)

	n := int(count)
	goPaths := make([]string, n)

	cSlice := unsafe.Slice(paths, n)
	for i := range n {
		goPaths[i] = C.GoString(cSlice[i])
	}

	t.sendDrop(goPaths)
}

//export nativeDragEntered
func nativeDragEntered(handle C.uintptr_t) {
	h := cgo.Handle(handle)
	t := h.Value().(*Target)
	t.sendDragState(DragEntered)
}

//export nativeDragExited
func nativeDragExited(handle C.uintptr_t) {
	h := cgo.Handle(handle)
	t := h.Value().(*Target)
	t.sendDragState(DragNone)
}
