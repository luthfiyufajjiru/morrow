//go:build linux || darwin

package app

import (
	"syscall"
	"time"
)

// stopOrphan stops what is left of an app whose relay died: the app (if
// alive) and the rest of its process group, with SIGTERM, then SIGKILL after
// orphanStopTimeout. A live process with the app's PID that started after the
// app did is another process that reused the PID (e.g. after a reboot) and is
// left alone. Without a live leader the group is the app's if the app
// started during this boot: a process group ID is not reused while any
// member lives. It reports whether anything was signalled.
func stopOrphan(pid int, appStarted time.Time) bool {
	if isProcessAlive(pid) {
		if !startedBy(pid, appStarted.Add(2*time.Second)) {
			return false
		}
	} else if !sinceBoot(appStarted) {
		return false
	}
	if !groupAlive(pid) {
		return false
	}
	_ = signalGroup(pid, syscall.SIGTERM)
	deadline := time.Now().Add(orphanStopTimeout)
	for groupAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if groupAlive(pid) {
		_ = signalGroup(pid, syscall.SIGKILL)
	}
	return true
}

// groupAlive reports whether the process or any member of its process group
// exists.
func groupAlive(pid int) bool {
	err := syscall.Kill(-pid, 0)
	return err == nil || err == syscall.EPERM || isProcessAlive(pid)
}
