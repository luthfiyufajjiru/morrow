//go:build linux || darwin

package app

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// signalGroup signals the app's process group (apps are started with
// Setsid, so their PID is the group ID and children are included), falling
// back to the single process.
func signalGroup(pid int, sig syscall.Signal) error {
	if err := syscall.Kill(-pid, sig); err == nil {
		return nil
	}
	return syscall.Kill(pid, sig)
}

// gracefulStop sends SIGTERM to the app and its children, waits up to
// timeout for it to exit, then sends SIGKILL.
func gracefulStop(pid int, timeout time.Duration) error {
	if err := signalGroup(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return err
	}
	if waitExit(pid, timeout) {
		return nil
	}
	if err := signalGroup(pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return err
	}
	if waitExit(pid, killWait) {
		return nil
	}
	return fmt.Errorf("process %d is still running after SIGKILL", pid)
}

// forceKill kills a single process immediately (used for the log relay and
// to clean up after a failed start).
func forceKill(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGKILL)
}

// isProcessAlive reports whether a process with this PID exists. Signal 0
// performs only the existence/permission check; EPERM means it exists but
// belongs to another user.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
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

// exitSignal names the signal that killed the process, if any.
func exitSignal(ps *os.ProcessState) string {
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return unix.SignalName(ws.Signal())
	}
	return ""
}
