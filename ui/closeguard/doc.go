// Package closeguard intercepts the native window close button so that
// Go code can show a confirmation dialog before the window is destroyed.
//
// On macOS this works by swizzling the window delegate's windowShouldClose:
// method. On Windows it subclasses the window procedure to intercept
// WM_CLOSE. Linux/BSD and other platforms are no-ops because neither
// Wayland's xdg-shell nor Gio's X11 backend expose a close-veto hook.
// Use [CloseGuard.IsSupported] to check whether the current platform
// supports close interception.
//
// # Lifecycle
//
// A single CloseGuard must be created per application and used as follows:
//
//	guard := closeguard.New(w)
//	defer guard.Close()
//
//	for {
//	    e := w.Event()
//	    guard.ListenEvents(e)
//
//	    switch e := e.(type) {
//	    case app.FrameEvent:
//	        guard.SetGuarded(hasUnsavedChanges)
//	        if guard.PollCloseAttempt() {
//	            showConfirmDialog()
//	        }
//	        // ... layout and frame ...
//	    }
//	}
//
// [CloseGuard.ListenEvents] must be called for every window event so the
// guard can capture the platform view handle. The remaining methods should
// be called once per frame.
//
// Close is idempotent and safe to call multiple times. After Close, all
// methods become no-ops.
package closeguard
