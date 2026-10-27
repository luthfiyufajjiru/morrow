//go:build windows

package inbox

import (
	"fmt"
	"os/exec"
)

// TaskName is the scheduled task that applies submissions.
const TaskName = `Morrow\Inbox`

// TaskArgs returns the schtasks arguments that run `morrow apply` as SYSTEM
// every minute. Windows has no simple "folder not empty" trigger.
func TaskArgs(exe string) []string {
	return []string{"/Create", "/TN", TaskName, "/SC", "MINUTE", "/MO", "1",
		"/RU", "SYSTEM", "/RL", "HIGHEST", "/TR", fmt.Sprintf(`"%s" apply`, exe), "/F"}
}

// InstallHook registers the scheduled task.
func InstallHook(exe, _ string) (string, error) {
	if out, err := exec.Command("schtasks", TaskArgs(exe)...).CombinedOutput(); err != nil {
		return "", fmt.Errorf("schtasks failed: %v: %s", err, out)
	}
	return fmt.Sprintf("scheduled task %s (applies submissions every minute, as SYSTEM)", TaskName), nil
}

// HookInstalled reports whether the scheduled task exists.
func HookInstalled() (string, bool) {
	if exec.Command("schtasks", "/Query", "/TN", TaskName).Run() == nil {
		return "scheduled task " + TaskName, true
	}
	return "", false
}

// RemoveHook deletes the scheduled task. A missing task is not an error.
func RemoveHook() error {
	if _, ok := HookInstalled(); !ok {
		return nil
	}
	if out, err := exec.Command("schtasks", "/Delete", "/TN", TaskName, "/F").CombinedOutput(); err != nil {
		return fmt.Errorf("schtasks /Delete failed: %v: %s", err, out)
	}
	return nil
}
