//go:build linux || darwin

package app

import (
	"os"
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
