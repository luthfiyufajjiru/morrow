//go:build windows

package app

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// gracefulStop first asks the app and its children (/T) to close and waits up
// to timeout. Console apps without a window cannot be asked; taskkill refuses
// at once, and the tree is then terminated forcefully (/F).
func gracefulStop(pid int, timeout time.Duration) error {
	if exec.Command("taskkill", "/T", "/PID", fmt.Sprintf("%d", pid)).Run() == nil && waitExit(pid, timeout) {
		return nil
	}
	if err := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid)).Run(); err != nil && isProcessAlive(pid) {
		return err
	}
	if waitExit(pid, killWait) {
		return nil
	}
	return fmt.Errorf("process %d is still running after a forced kill", pid)
}

// forceKill terminates a process and its children immediately.
func forceKill(pid int) error {
	return exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid)).Run()
}

// isProcessAlive reports whether a process with this PID exists and has not
// exited. Access denied means it exists but belongs to another user.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const processQueryLimitedInformation = 0x1000
	const stillActive = 259
	h, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return err == syscall.ERROR_ACCESS_DENIED
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}

// detachProcess fully severs the child from the parent console using CREATE_NO_WINDOW.
// This prevents it from being killed if the terminal closes, but UNLIKE DETACHED_PROCESS,
// it preserves flawless inheritance of background pipe handles.
// CREATE_NO_WINDOW = 0x08000000
func detachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000,
	}
}

// detachRelayProcess uses the precise same flag as detachProcess to guarantee it stays alive.
func detachRelayProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000,
	}
}
