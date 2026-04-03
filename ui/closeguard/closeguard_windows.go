//go:build windows

package closeguard

import (
	"log/slog"
	"sync/atomic"
	"syscall"

	"gioui.org/app"
	"gioui.org/io/event"
)

// Window procedure subclassing constants.
const wmClose = 0x0010

// gwlpWndProc is GWLP_WNDPROC (-4) for SetWindowLongPtrW.
// Expressed as ^uintptr(3) because Go does not allow negative uintptr
// constants; ^3 is the bitwise NOT of 3, which equals -4 in two's complement.
//
//nolint:mnd // Windows API constant.
var gwlpWndProc = ^uintptr(3)

//nolint:gochecknoglobals // Windows DLL handles must be package-level.
var (
	user32               = syscall.NewLazyDLL("user32.dll")
	procSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProc   = user32.NewProc("CallWindowProcW")
)

// Package-level globals are required because Windows allows only one window
// procedure per HWND, and syscall.NewCallback needs a package-level function.
// Gio creates a single window, so this matches the single-instance constraint
// documented on CloseGuard.
//
//nolint:gochecknoglobals // Global close-guard state for the window procedure callback.
var (
	closeGuarded    atomic.Bool
	origWndProc     uintptr
	closeSubclassWP = syscall.NewCallback(closeGuardWndProc)
	globalCG        *CloseGuard
)

// closeGuardWndProc intercepts WM_CLOSE when the close guard is active.
func closeGuardWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if msg == wmClose && closeGuarded.Load() && globalCG != nil {
		globalCG.sendCloseAttempt()

		return 0
	}

	ret, _, _ := procCallWindowProc.Call(origWndProc, hwnd, msg, wParam, lParam)

	return ret
}

// platformState holds Windows-specific close guard state.
type platformState struct {
	installed bool
	hwnd      uintptr
}

func newPlatformState(_ *CloseGuard) platformState {
	return platformState{}
}

// restoreWndProc restores the original window procedure via SetWindowLongPtrW.
// Logs a warning if the restore fails.
func restoreWndProc(hwnd uintptr) {
	ret, _, _ := procSetWindowLongPtr.Call(hwnd, gwlpWndProc, origWndProc)
	if ret == 0 {
		slog.Warn("SetWindowLongPtrW failed to restore original window procedure")
	}
}

// ListenEvents captures the Win32ViewEvent and installs the close guard.
// No-op after Close has been called.
func (g *CloseGuard) ListenEvents(evt event.Event) {
	if g.closed.Load() {
		return
	}

	e, ok := evt.(app.Win32ViewEvent)
	if !ok {
		return
	}

	if e.HWND != 0 && !g.platform.installed {
		g.platform.hwnd = e.HWND
		globalCG = g

		orig, _, _ := procSetWindowLongPtr.Call(
			e.HWND, gwlpWndProc, closeSubclassWP,
		)
		if orig != 0 {
			origWndProc = orig
			g.platform.installed = true
		}
	} else if e.HWND == 0 && g.platform.installed {
		if origWndProc != 0 {
			restoreWndProc(g.platform.hwnd)
		}

		g.platform.installed = false
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

	closeGuarded.Store(guarded)
}

// Close disables the guard and restores the original window procedure.
// Safe to call multiple times; only the first call releases resources.
func (g *CloseGuard) Close() {
	if !g.closed.CompareAndSwap(false, true) {
		return
	}

	// Disable the guard before restoring the window procedure so that
	// closeGuardWndProc never calls into a closed guard.
	closeGuarded.Store(false)

	globalCG = nil

	if g.platform.installed && origWndProc != 0 {
		restoreWndProc(g.platform.hwnd)

		g.platform.installed = false
	}
}
