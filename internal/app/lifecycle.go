package app

import "time"

const (
	// DefaultStopTimeout is how long stop waits after asking an app to exit
	// before killing it.
	DefaultStopTimeout = 10 * time.Second
	// killWait is how long to wait for the process to disappear after a kill.
	killWait = 5 * time.Second
)

// waitExit polls until the process is gone or timeout passes.
func waitExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !isProcessAlive(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
