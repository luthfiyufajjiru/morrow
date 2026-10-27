package app

import (
	"fmt"
	"morrow/internal/db"
	"time"
)

// startFn is replaced in tests.
var startFn = func(name string) (int, error) { return StartApp(name, nil) }

// wantedApps returns the apps the user wants up, by name.
func wantedApps() ([]string, error) {
	rows, err := db.DB.Query("SELECT application_name FROM applications WHERE application_desired_state = 'up' ORDER BY application_name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// WantedApps lists the apps that `morrow startup` starts at boot.
func WantedApps() ([]string, error) { return wantedApps() }

// BootApps starts every app the user wants up that is not already running
// (after a reboot every recorded process is gone). Apps the user stopped stay
// stopped. It returns one line per app for the boot log.
func BootApps() ([]string, error) {
	names, err := wantedApps()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, n := range names {
		status, _, err := GetAppStatus(n) // reconciles dead PIDs to stopped
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s: %v", n, err))
			continue
		}
		if status == "running" || status == "restarting" {
			lines = append(lines, fmt.Sprintf("%s: already %s", n, status))
			continue
		}
		pid, err := startFn(n)
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s: failed to start: %v", n, err))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: started with PID %d", n, pid))
	}
	return lines, nil
}

// ShutdownApps stops every running app gracefully at system shutdown and
// keeps them wanted, so BootApps brings them back.
func ShutdownApps(timeout time.Duration) ([]string, error) {
	apps, err := ListApps()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, a := range apps {
		if a.Status != "running" && a.Status != "restarting" {
			continue
		}
		if err := StopForShutdown(a.Name, timeout); err != nil {
			lines = append(lines, fmt.Sprintf("%s: failed to stop: %v", a.Name, err))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s: stopped", a.Name))
	}
	return lines, nil
}
