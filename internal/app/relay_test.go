package app

import (
	"os"
	"strings"
	"testing"
	"time"

	"morrow/internal/config"
	"morrow/internal/db"
)

func setRunning(t *testing.T, name string, pid int, lastRun time.Time) {
	t.Helper()
	if _, err := db.DB.Exec(`UPDATE applications SET application_status = 'running', application_pid = ?, application_last_run_time = ?
		WHERE application_name = ?`, pid, lastRun.UTC().Format("2006-01-02T15:04:05Z"), name); err != nil {
		t.Fatal(err)
	}
}

func TestRelayLockIsExclusive(t *testing.T) {
	setupDB(t)
	if relayState("api") != relayUnknown {
		t.Fatal("no lock file yet: state must be unknown")
	}
	release, err := acquireRelay("api")
	if err != nil {
		t.Fatal(err)
	}
	if relayState("api") != relayAlive {
		t.Error("held lock not seen as a live relay")
	}
	if relayPID("api") != os.Getpid() {
		t.Errorf("relay PID file = %d, want %d", relayPID("api"), os.Getpid())
	}
	start := time.Now()
	if _, err := acquireRelay("api"); err == nil || !strings.Contains(err.Error(), "already supervised") {
		t.Errorf("second relay err = %v, want already supervised", err)
	}
	if waited := time.Since(start); waited < relayLockWait {
		t.Errorf("second relay gave up after %s, want it to wait %s", waited, relayLockWait)
	}
	release()
	if relayState("api") != relayGone {
		t.Error("released lock not seen as a dead relay")
	}
	if _, err := os.Stat(config.GetRelayPIDFilePath("api")); !os.IsNotExist(err) {
		t.Error("relay PID file not removed on release")
	}
}

// While the relay lives, it records the app's exit: a status check between
// the exit and the relay's decision must not mark the app stopped, which
// would cancel its restart.
func TestReconcileLeavesLiveRelayAlone(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	release, err := acquireRelay("api")
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	setRunning(t, "api", deadPID(t), time.Now())
	if st, _, _ := GetAppStatus("api"); st != "running" {
		t.Errorf("status = %s, want running until the relay records the exit", st)
	}
}

func TestReconcileDeadRelay(t *testing.T) {
	for _, tc := range []struct{ from, want string }{
		{"running", "exited"}, {"restarting", "exited"}, {"stopping", "stopped"},
	} {
		t.Run(tc.from, func(t *testing.T) {
			setupDB(t)
			mustCreate(t, "api", nil, "")
			release, err := acquireRelay("api")
			if err != nil {
				t.Fatal(err)
			}
			release() // the relay died
			if err := os.WriteFile(config.GetLogFilePath("api"), nil, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := db.DB.Exec("UPDATE applications SET application_status = ?, application_pid = ? WHERE application_name = 'api'",
				tc.from, deadPID(t)); err != nil {
				t.Fatal(err)
			}
			if st, pid, _ := GetAppStatus("api"); st != tc.want || pid != 0 {
				t.Errorf("status = %s/%d, want %s/0", st, pid, tc.want)
			}
			var st string
			_ = db.DB.QueryRow("SELECT application_status FROM applications WHERE application_name = 'api'").Scan(&st)
			if st != tc.want {
				t.Errorf("not persisted: %s", st)
			}
			if log, _ := os.ReadFile(config.GetLogFilePath("api")); !strings.Contains(string(log), "the supervising relay died") {
				t.Errorf("log does not say what happened: %q", log)
			}
		})
	}
}
