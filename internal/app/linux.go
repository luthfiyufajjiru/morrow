//go:build linux || darwin

package app

import (
	"os"
	"os/exec"
	"syscall"
)

// terminateProcess on Linux sends a SIGTERM signal to the process
// for a graceful shutdown.
func terminateProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}

// detachProcess creates a new session for the managed app process,
// making it immune to terminal SIGHUP.
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}

// detachRelayProcess is identical on Linux — Setsid works for both
// the app and the relay since the relay reads from a pipe, not the console.
func detachRelayProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}
