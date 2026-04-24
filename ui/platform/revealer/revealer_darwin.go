//go:build darwin

package revealer

import (
	"context"
	"os/exec"
)

// revealFile opens macOS Finder with the specified file selected.
func revealFile(path string) {
	ctx, cancel := context.WithTimeout(context.Background(), revealTimeout)

	go func() {
		defer cancel()

		_ = exec.CommandContext(ctx, "open", "-R", path).Run() //nolint:gosec // path is validated by resolve() in revealer.go
	}()
}
