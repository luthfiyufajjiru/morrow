//go:build windows

package app

import (
	"time"

	"golang.org/x/sys/windows"
)

// stopOrphan stops what is left of an app whose relay died: the app and its
// child processes. A live process with the app's PID that started after the
// app did is another process that reused the PID and is left alone. It
// reports whether anything was stopped.
func stopOrphan(pid int, appStarted time.Time) bool {
	if !isProcessAlive(pid) || !startedBy(pid, appStarted.Add(2*time.Second)) {
		return false
	}
	_ = gracefulStop(pid, orphanStopTimeout)
	return true
}

// startedBy reports whether process pid started no later than t.
func startedBy(pid int, t time.Time) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return false
	}
	return !time.Unix(0, created.Nanoseconds()).After(t)
}
