// Package revealer opens the platform's native file manager with a specific
// file selected. On macOS this uses Finder, on Windows it uses Explorer,
// and on Linux/BSD it uses the freedesktop FileManager1 D-Bus interface
// with an xdg-open fallback.
package revealer

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const revealTimeout = 5 * time.Second

// RevealFile opens the native file manager with the given file selected.
// The path must be an absolute path to an existing file or directory.
// On unsupported platforms this is a no-op.
func RevealFile(path string) {
	resolved, err := resolve(path)
	if err != nil {
		return
	}

	revealFile(resolved)
}

// resolve validates the path and returns a clean, absolute, symlink-resolved path.
func resolve(path string) (string, error) {
	if path == "" {
		return "", os.ErrInvalid
	}

	clean := filepath.FromSlash(filepath.Clean(path))

	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path: %w", err)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("evaluating symlinks: %w", err)
	}

	return resolved, nil
}
