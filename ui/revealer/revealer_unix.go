//go:build (linux || freebsd || openbsd) && !android

package revealer

import (
	"context"
	"net/url"
	"os/exec"
	"path/filepath"
)

// revealFile attempts to reveal the file in the native file manager.
// It first tries the freedesktop FileManager1 D-Bus interface (supported by
// GNOME Files, Dolphin, Thunar, and most freedesktop-compliant file managers).
// If that fails, it falls back to xdg-open on the parent directory.
func revealFile(path string) {
	go func() {
		u := url.URL{Scheme: "file", Path: path}
		ctx, cancel := context.WithTimeout(context.Background(), revealTimeout)

		defer cancel()

		err := exec.CommandContext(ctx, //nolint:gosec // fixed executable, path from internal app state
			"dbus-send", "--session",
			"--dest=org.freedesktop.FileManager1",
			"--type=method_call",
			"/org/freedesktop/FileManager1",
			"org.freedesktop.FileManager1.ShowItems",
			"array:string:"+u.String(), "string:",
		).Run()
		if err != nil {
			// Fallback with a fresh timeout: open the parent directory (no file selection).
			fallbackCtx, fallbackCancel := context.WithTimeout(context.Background(), revealTimeout)
			defer fallbackCancel()

			_ = exec.CommandContext(fallbackCtx, "xdg-open", filepath.Dir(path)).Run() //nolint:gosec // fixed executable, path from internal app state
		}
	}()
}
