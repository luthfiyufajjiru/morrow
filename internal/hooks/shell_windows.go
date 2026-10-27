//go:build windows

package hooks

import (
	"context"
	"os/exec"
	"syscall"
)

// shellCommand runs command through cmd.exe. The command line is passed raw:
// Go's argument quoting (\") is not understood by cmd.exe and would break
// commands that contain quotes.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /S /C "` + command + `"`}
	return cmd
}
