//go:build windows

package app

import (
	"fmt"
	"os/exec"
	"syscall"
)

// terminateProcess on Windows uses taskkill to ensure the process
// and all its sub-processes (/T) are forcefully (/F) stopped.
func terminateProcess(pid int) error {
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
