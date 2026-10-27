//go:build !windows

package hooks

import (
	"context"
	"os/exec"
)

// shellCommand runs command through /bin/sh.
func shellCommand(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "/bin/sh", "-c", command)
}
