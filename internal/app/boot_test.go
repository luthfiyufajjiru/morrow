package app

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"morrow/internal/db"
)

func desired(t *testing.T, name string) string {
	t.Helper()
	var d string
	if err := db.DB.QueryRow("SELECT application_desired_state FROM applications WHERE application_name = ?", name).Scan(&d); err != nil {
		t.Fatal(err)
	}
	return d
}

func setRow(t *testing.T, name, status, want string, pid int) {
	t.Helper()
	if _, err := db.DB.Exec(`UPDATE applications SET application_status = ?, application_desired_state = ?, application_pid = ?
		WHERE application_name = ?`, status, want, pid, name); err != nil {
		t.Fatal(err)
	}
}

func TestNewAppIsNotWanted(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	if d := desired(t, "api"); d != "down" {
		t.Errorf("new app desired = %s, want down", d)
	}
}

func TestUserStopMarksDownEvenIfExited(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	setRow(t, "api", "exited", "up", 0)
	if err := StopApp("api"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("StopApp = %v", err)
	}
	if d := desired(t, "api"); d != "down" {
		t.Errorf("after a user stop desired = %s, want down (it must not come back at boot)", d)
	}
}

func TestShutdownStopKeepsWanted(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	setRow(t, "api", "restarting", "up", 0)
	if err := StopForShutdown("api", DefaultStopTimeout); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := GetAppStatus("api"); st != "stopped" {
		t.Errorf("status = %s, want stopped", st)
	}
	if d := desired(t, "api"); d != "up" {
		t.Errorf("after a shutdown stop desired = %s, want up (it must come back at boot)", d)
	}
}

func TestBootApps(t *testing.T) {
	setupDB(t)
	for _, n := range []string{"api", "web", "worker", "stopped-by-user", "broken"} {
		mustCreate(t, n, nil, "")
	}
	// After a reboot: wanted apps have dead PIDs or exited; one the user
	// stopped; one already running (e.g. started by hand before the hook ran).
	setRow(t, "api", "running", "up", deadPID(t))
	setRow(t, "web", "exited", "up", 0)
	setRow(t, "worker", "running", "up", 0)
	setRow(t, "stopped-by-user", "stopped", "down", 0)
	setRow(t, "broken", "stopped", "up", 0)
	if _, err := db.DB.Exec("UPDATE applications SET application_pid = ? WHERE application_name = 'worker'", selfPID()); err != nil {
		t.Fatal(err)
	}

	var started []string
	orig := startFn
	startFn = func(name string) (int, error) {
		if name == "broken" {
			return 0, errors.New("exec: not found")
		}
		started = append(started, name)
		return 1000 + len(started), nil
	}
	t.Cleanup(func() { startFn = orig })

	lines, err := BootApps()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(started, []string{"api", "web"}) {
		t.Errorf("started %v, want [api web] (not the running worker, not the app the user stopped)", started)
	}
	out := strings.Join(lines, "\n")
	for _, want := range []string{"api: started with PID 1001", "web: started with PID 1002", "worker: already running", "broken: failed to start"} {
		if !strings.Contains(out, want) {
			t.Errorf("boot log missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "stopped-by-user") {
		t.Errorf("boot touched an app the user stopped:\n%s", out)
	}
	if got, _ := WantedApps(); !reflect.DeepEqual(got, []string{"api", "broken", "web", "worker"}) {
		t.Errorf("WantedApps = %v", got)
	}
}

func selfPID() int { return os.Getpid() }
