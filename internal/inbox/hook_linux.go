//go:build linux

package inbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// systemdDir is where the units are written; tests point it elsewhere.
var systemdDir = "/etc/systemd/system"

// SystemdUnits renders a path unit that starts the service whenever the
// inbox is not empty, and the oneshot service that runs `morrow apply`.
func SystemdUnits(exe, inboxDir string) (pathUnit, serviceUnit string) {
	pathUnit = fmt.Sprintf(`[Unit]
Description=Watch the Morrow inbox for submitted secrets

[Path]
DirectoryNotEmpty=%s

[Install]
WantedBy=multi-user.target
`, inboxDir)
	serviceUnit = fmt.Sprintf(`[Unit]
Description=Apply secrets submitted to the Morrow inbox

[Service]
Type=oneshot
ExecStart=%s apply
`, exe)
	return pathUnit, serviceUnit
}

// InstallHook installs and starts the systemd units. Without systemd it
// returns an error explaining how to run `morrow apply` another way.
func InstallHook(exe, inboxDir string) (string, error) {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return "", errors.New("systemd is not running here; run 'morrow apply' as root from cron or your init system instead")
	}
	pathUnit, serviceUnit := SystemdUnits(exe, inboxDir)
	if err := os.WriteFile(filepath.Join(systemdDir, "morrow-inbox.path"), []byte(pathUnit), 0644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(systemdDir, "morrow-inbox.service"), []byte(serviceUnit), 0644); err != nil {
		return "", err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", "morrow-inbox.path"}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("systemctl %v failed: %v: %s", args, err, out)
		}
	}
	return "systemd unit morrow-inbox.path (applies submissions as soon as they arrive)", nil
}
