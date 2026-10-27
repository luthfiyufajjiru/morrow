//go:build linux

// Package startup installs the OS hook that starts wanted apps at boot.
package startup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// unitDir is where the unit is written; tests point it elsewhere.
var unitDir = "/etc/systemd/system"

const unitName = "morrow.service"

// Unit renders the systemd unit. It is a oneshot that stays active, so
// systemd runs ExecStop at shutdown; KillMode=process keeps the detached
// apps out of systemd's own stop, letting ExecStop stop them gracefully.
func Unit(exe string) string {
	return fmt.Sprintf(`[Unit]
Description=Morrow: start apps at boot, stop them gracefully at shutdown
Documentation=https://github.com/luthfiyufajjiru/morrow/wiki
Wants=network-online.target
After=network-online.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=%[1]s _boot
ExecStop=%[1]s _shutdown
TimeoutStopSec=120
KillMode=process

[Install]
WantedBy=multi-user.target
`, exe)
}

func systemdRunning() bool {
	_, err := os.Stat("/run/systemd/system")
	return err == nil
}

// Installed reports whether the unit is installed.
func Installed() (string, bool) {
	if _, err := os.Stat(filepath.Join(unitDir, unitName)); err == nil {
		return "systemd unit " + unitName, true
	}
	return "", false
}

// Install writes, enables and starts the unit (starting it runs _boot, which
// only starts wanted apps that are not running).
func Install(exe string) (string, error) {
	if !systemdRunning() {
		return "", errors.New("systemd is not running here; run 'morrow _boot' as root from your init system at boot instead")
	}
	if err := os.WriteFile(filepath.Join(unitDir, unitName), []byte(Unit(exe)), 0644); err != nil {
		return "", err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", unitName}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("systemctl %s failed: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	return "systemd unit " + unitName, nil
}

// Remove disables and deletes the unit without stopping any app: _shutdown
// only stops apps when the whole system is shutting down.
func Remove() error {
	if systemdRunning() {
		_ = exec.Command("systemctl", "disable", unitName).Run()
		_ = exec.Command("systemctl", "stop", unitName).Run()
	}
	if err := os.Remove(filepath.Join(unitDir, unitName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if systemdRunning() {
		_ = exec.Command("systemctl", "daemon-reload").Run()
	}
	return nil
}

// ShuttingDown reports whether the system is shutting down or rebooting.
func ShuttingDown() bool {
	out, _ := exec.Command("systemctl", "is-system-running").Output()
	return strings.TrimSpace(string(out)) == "stopping"
}
