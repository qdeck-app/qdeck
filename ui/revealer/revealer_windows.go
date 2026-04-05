//go:build windows

package revealer

import (
	"context"
	"os/exec"
)

// revealFile opens Windows Explorer with the specified file selected.
// The /select, flag tells Explorer to open the parent folder and highlight the file.
func revealFile(path string) {
	ctx, cancel := context.WithTimeout(context.Background(), revealTimeout)

	go func() {
		defer cancel()

		_ = exec.CommandContext(ctx, "explorer", `/select,`+path).Run() //nolint:gosec // Path is pre-validated by resolve(): Clean, Abs, EvalSymlinks.
	}()
}
