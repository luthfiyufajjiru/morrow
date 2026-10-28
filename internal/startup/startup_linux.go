//go:build linux

// Package startup installs the OS hook that starts wanted apps at boot.
package startup

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// unitDir is where the unit is written; tests point it elsewhere.
var unitDir = "/etc/systemd/system"

const unitName = "morrow.service"

// unitPattern is a systemd unit name: allowed characters and a unit type suffix.
var unitPattern = regexp.MustCompile(`^[A-Za-z0-9:_.\\@-]+\.(service|socket|target|mount|device|path|timer|swap|slice|scope|automount)$`)

// Units validates the units to order after; a name without a type gets
// ".service", so "postgresql" means postgresql.service.
func Units(names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n != "" && !strings.Contains(n, ".") {
			n += ".service"
		}
		if !unitPattern.MatchString(n) {
			return nil, fmt.Errorf("invalid systemd unit name %q (e.g. postgresql.service)", n)
		}
		out = append(out, n)
	}
	return out, nil
}

// Unit renders the systemd unit. It is a oneshot that stays active, so
// systemd runs ExecStop at shutdown; KillMode=process keeps the detached
// apps out of systemd's own stop, letting ExecStop stop them gracefully.
// Apps started at boot run in this unit's cgroup; OOMPolicy=continue keeps
// the unit active when the kernel kills one of them for memory, so ExecStop
// still runs at shutdown (the default would deactivate the unit).
// after lists local services (e.g. postgresql.service) that must be up
// before the apps start. Being ordered after them also means, at shutdown,
// the apps are stopped before those services.
func Unit(exe string, after []string) string {
	deps := ""
	for _, u := range after {
		deps += "Wants=" + u + "\nAfter=" + u + "\n"
	}
	return fmt.Sprintf(`[Unit]
Description=Morrow: start apps at boot, stop them gracefully at shutdown
Documentation=https://github.com/luthfiyufajjiru/morrow/wiki
Wants=network-online.target
After=network-online.target
%[2]s
[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=%[1]s _boot
ExecStop=%[1]s _shutdown
TimeoutStopSec=120
KillMode=process
OOMPolicy=continue

[Install]
WantedBy=multi-user.target
`, exe, deps)
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
// only starts wanted apps that are not running). It replaces any previous
// unit, so after must list every service to order after.
func Install(exe string, after []string) (string, error) {
	after, err := Units(after)
	if err != nil {
		return "", err
	}
	if !systemdRunning() {
		return "", errors.New("systemd is not running here; run 'morrow _boot' as root from your init system at boot instead")
	}
	if err := os.WriteFile(filepath.Join(unitDir, unitName), []byte(Unit(exe, after)), 0644); err != nil {
		return "", err
	}
	for _, args := range [][]string{{"daemon-reload"}, {"enable", "--now", unitName}} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("systemctl %s failed: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	desc := "systemd unit " + unitName
	if len(after) > 0 {
		desc += " (after " + strings.Join(after, ", ") + ")"
	}
	return desc, nil
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
