// Package nativedrop provides native OS-level file drag-and-drop for Gio
// applications.
//
// Drag-and-drop is implemented via platform-specific hooks: Objective-C
// NSDraggingDestination on macOS, COM IDropTarget on Windows, and Gio's
// built-in transfer events on Wayland. X11 is not supported (Gio v0.9.0
// lacks XDND).
package nativedrop
