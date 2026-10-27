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
