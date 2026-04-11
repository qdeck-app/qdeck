//go:build windows

package executil

import (
	"os/exec"
	"syscall"
)

// HideWindow prevents the executed command from creating a visible console window.
func HideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
