// Package nativedrop provides native OS-level file drag-and-drop for Gio
// applications.
//
// Drag-and-drop is implemented via platform-specific hooks: Objective-C
// NSDraggingDestination on macOS and COM IDropTarget on Windows.
// Linux is not supported: Gio v0.9.0 lacks XDND (X11) and discards
// Wayland DnD data offers in its flushOffers handler.
package nativedrop
