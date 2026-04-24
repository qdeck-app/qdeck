//go:build windows

package revealer

import (
	"context"
	"os/exec"

	"github.com/qdeck-app/qdeck/infrastructure/executil"
)

// revealFile opens Windows Explorer with the specified file selected.
// The /select, flag tells Explorer to open the parent folder and highlight the file.
func revealFile(path string) {
	ctx, cancel := context.WithTimeout(context.Background(), revealTimeout)

	go func() {
		defer cancel()

		cmd := exec.CommandContext(ctx, "explorer", `/select,`+path) //nolint:gosec // Path is pre-validated by resolve(): Clean, Abs, EvalSymlinks.
		executil.HideWindow(cmd)

		_ = cmd.Run()
	}()
}
