//go:build windows

// Package startup installs the OS hook that starts wanted apps at boot.
package startup

import (
	"fmt"
	"os/exec"
)

// TaskName is the scheduled task that starts wanted apps at boot.
const TaskName = `Morrow\Startup`

// TaskArgs registers `morrow _boot` to run as SYSTEM at system startup, after
// a short delay for networking. Apps it starts run in the services session,
// so they keep running when users log off.
func TaskArgs(exe string) []string {
	return []string{"/Create", "/TN", TaskName, "/SC", "ONSTART", "/DELAY", "0000:15",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/TR", fmt.Sprintf(`"%s" _boot`, exe), "/F"}
}

// Installed reports whether the scheduled task exists.
func Installed() (string, bool) {
	if exec.Command("schtasks", "/Query", "/TN", TaskName).Run() == nil {
		return "scheduled task " + TaskName, true
	}
	return "", false
}

// Install registers the scheduled task.
func Install(exe string) (string, error) {
	if out, err := exec.Command("schtasks", TaskArgs(exe)...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("schtasks failed: %v: %s", err, out)
	}
	return "scheduled task " + TaskName + " (runs at startup as SYSTEM)", nil
}

// Remove deletes the scheduled task. A missing task is not an error.
func Remove() error {
	if _, ok := Installed(); !ok {
		return nil
	}
	if out, err := exec.Command("schtasks", "/Delete", "/TN", TaskName, "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Delete failed: %v: %s", err, out)
	}
	return nil
}

// ShuttingDown is not used on Windows: the task only runs at startup.
func ShuttingDown() bool { return false }
