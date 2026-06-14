//go:build windows

package screen

import "syscall"

//nolint:gochecknoglobals // Windows DLL handles must be package-level.
var (
	user32 = syscall.NewLazyDLL("user32.dll")

	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procGetDpiForSystem  = user32.NewProc("GetDpiForSystem")
)

const (
	smCXFullScreen = 16 // SM_CXFULLSCREEN — width of the maximized client area
	smCYFullScreen = 17 // SM_CYFULLSCREEN — height of the maximized client area
	baseDPI        = 96 // DPI at 100% scaling
)

// workAreaDp returns the primary monitor's usable area (the maximized client
// area, which already excludes the taskbar) converted from physical pixels to
// Dp via the system DPI. The metric and DPI share a coordinate space, so the
// conversion holds whether the process is per-monitor DPI aware or virtualized.
func workAreaDp() (widthDp, heightDp int, ok bool) {
	wPx := getSystemMetrics(smCXFullScreen)
	hPx := getSystemMetrics(smCYFullScreen)

	if wPx <= 0 || hPx <= 0 {
		return 0, 0, false
	}

	dpi := systemDPI()

	return wPx * baseDPI / dpi, hPx * baseDPI / dpi, true
}

func getSystemMetrics(index int) int {
	r, _, _ := procGetSystemMetrics.Call(uintptr(index))

	return int(r)
}

// systemDPI returns the system DPI, falling back to baseDPI (96) when
// GetDpiForSystem is unavailable (pre-Windows 10 1607) or fails.
func systemDPI() int {
	if procGetDpiForSystem.Find() != nil {
		return baseDPI
	}

	dpi, _, _ := procGetDpiForSystem.Call()
	if dpi == 0 {
		return baseDPI
	}

	return int(dpi)
}
