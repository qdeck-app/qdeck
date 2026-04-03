//go:build !(darwin || windows || linux || freebsd || openbsd) || ios || android

package nativedrop

import "gioui.org/io/event"

type platformState struct{}

func newPlatformState(_ *Target) platformState {
	return platformState{}
}

// ListenEvents is a no-op on platforms without native drag-and-drop support.
func (t *Target) ListenEvents(_ event.Event) {}

// Close is a no-op on unsupported platforms.
func (t *Target) Close() {}
