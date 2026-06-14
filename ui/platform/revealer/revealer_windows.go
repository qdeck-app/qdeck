//go:build windows

package revealer

import (
	"context"
	"os/exec"
	"syscall"
)

// revealFile opens Windows Explorer with the specified file selected.
func revealFile(path string) {
	ctx, cancel := context.WithTimeout(context.Background(), revealTimeout)

	go func() {
		defer cancel()

		// CommandContext resolves explorer.exe on PATH into cmd.Path; the
		// explicit CmdLine then controls the exact quoting passed to it.
		cmd := exec.CommandContext(ctx, "explorer")
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CmdLine: `explorer /select,"` + path + `"`, //nolint:gosec // Path pre-validated by resolve(): Clean, Abs, EvalSymlinks.
		}

		// Explorer returns exit code 1 even on success
		_ = cmd.Run()
	}()
}
