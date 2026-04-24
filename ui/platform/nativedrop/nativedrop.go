package nativedrop

import (
	"log/slog"

	"gioui.org/app"
)

// DragState represents the current drag-over state.
type DragState int

const (
	DragNone    DragState = iota
	DragEntered           // File(s) hovering over the window.
)

const (
	// dropChanSize is larger than 1 to avoid silently discarding file drop
	// events when multiple drops arrive before the UI frame polls them.
	// The drag-state channel stays at 1 because only the latest value
	// matters — stale intermediates are harmless.
	dropChanSize = 4
	dragChanSize = 1
)

// DropEvent carries the file paths from a native drag-and-drop operation.
type DropEvent struct {
	Paths []string
	// PositionY is the vertical drop coordinate in window pixels (from top).
	// Negative when unavailable.
	PositionY float32
}

// Target listens for OS-level file drag-and-drop events
// and delivers them to the Gio event loop via buffered channels.
type Target struct {
	// DropSupported is true when the platform supports drag-and-drop.
	// Set by platform-specific ListenEvents once the display server is known.
	DropSupported bool

	window *app.Window
	drops  chan DropEvent
	drags  chan DragState

	platform platformState
}

func New(w *app.Window) *Target {
	t := &Target{
		window: w,
		drops:  make(chan DropEvent, dropChanSize),
		drags:  make(chan DragState, dragChanSize),
	}

	t.platform = newPlatformState(t)

	return t
}

// PollDrop returns a drop event if one is available (non-blocking).
func (t *Target) PollDrop() (DropEvent, bool) {
	select {
	case ev := <-t.drops:
		return ev, true
	default:
		return DropEvent{}, false
	}
}

// PollDragState returns a drag state change if one is available (non-blocking).
func (t *Target) PollDragState() (DragState, bool) {
	select {
	case s := <-t.drags:
		return s, true
	default:
		return DragNone, false
	}
}

// sendDrop sends a drop event non-blockingly and invalidates the window.
// Invalidate runs in a separate goroutine because sendDrop is called from
// a CGo //export callback on the main thread; calling Invalidate directly
// would create a nested CGo call (Invalidate → C.gio_wakeupMainThread)
// that deadlocks the main thread.
func (t *Target) sendDrop(paths []string, posY float32) { //nolint:unused // called from platform-specific CGo callbacks (_macos.go, _windows.go)
	select {
	case t.drops <- DropEvent{Paths: paths, PositionY: posY}:
	default:
		slog.Warn("drop event discarded, channel full", "paths", len(paths))
	}

	go t.window.Invalidate()
}

// sendDragState sends a drag state change and invalidates the window.
// The channel is drained first so the latest state always wins — OS drag
// callbacks are serialized on the main thread, so there is no writer race.
func (t *Target) sendDragState(s DragState) { //nolint:unused // called from platform-specific CGo callbacks (_macos.go, _windows.go)
	// Drain stale value so the latest state always lands.
	select {
	case <-t.drags:
	default:
	}

	select {
	case t.drags <- s:
	default:
	}

	go t.window.Invalidate()
}
