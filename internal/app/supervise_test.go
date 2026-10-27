package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"morrow/internal/db"
)

func TestBackoff(t *testing.T) {
	want := map[int]time.Duration{0: time.Second, 1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second,
		6: 32 * time.Second, 7: time.Minute, 50: time.Minute}
	for n, d := range want {
		if got := backoff(n); got != d {
			t.Errorf("backoff(%d) = %s, want %s", n, got, d)
		}
	}
}

func TestSetRestartPolicy(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	if err := SetRestartPolicy("api", PolicyUnlessStopped, 7); err != nil {
		t.Fatal(err)
	}
	d, _ := GetAppDetail("api")
	if d.RestartPolicy != PolicyUnlessStopped || d.MaxRestarts != 7 {
		t.Errorf("detail = %s/%d", d.RestartPolicy, d.MaxRestarts)
	}
	for _, c := range []struct {
		app, policy string
		max         int
	}{{"api", "always", 5}, {"api", PolicyNo, 0}, {"api", PolicyNo, MaxRestartsLimit + 1}, {"nope", PolicyNo, 5}} {
		if err := SetRestartPolicy(c.app, c.policy, c.max); err == nil {
			t.Errorf("SetRestartPolicy(%v) accepted", c)
		}
	}
}

// supervised creates an app recorded as running with the given PID, and stubs
// sleeping and restarting. It returns a pointer to the number of restarts.
func supervised(t *testing.T, policy string, max, count, pid int, status string) *int {
	t.Helper()
	setupDB(t)
	mustCreate(t, "api", nil, "")
	if err := SetRestartPolicy("api", policy, max); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE applications SET application_status = ?, application_pid = ?, application_restart_count = ?
		WHERE application_name = 'api'`, status, pid, count); err != nil {
		t.Fatal(err)
	}
	restarts := 0
	origSleep, origRestart := sleep, restartFn
	sleep = func(time.Duration) {}
	restartFn = func(string) (int, error) { restarts++; return 4242, nil }
	t.Cleanup(func() { sleep, restartFn = origSleep, origRestart })
	return &restarts
}

func row(t *testing.T) (status string, pid, count int) {
	t.Helper()
	if err := db.DB.QueryRow(`SELECT application_status, application_pid, application_restart_count
		FROM applications WHERE application_name = 'api'`).Scan(&status, &pid, &count); err != nil {
		t.Fatal(err)
	}
	return
}

func collect() (*[]string, func(string)) {
	var lines []string
	return &lines, func(s string) { lines = append(lines, s) }
}

func TestHandleExitRequestedStop(t *testing.T) {
	restarts := supervised(t, PolicyUnlessStopped, 5, 0, 1111, "stopping")
	lines, logf := collect()
	if err := HandleExit("api", 1111, time.Second, logf); err != nil {
		t.Fatal(err)
	}
	if st, pid, _ := row(t); st != "stopped" || pid != 0 || *restarts != 0 {
		t.Errorf("status=%s pid=%d restarts=%d, want stopped without restart", st, pid, *restarts)
	}
	if len(*lines) != 1 || !strings.Contains((*lines)[0], "stopped") {
		t.Errorf("log = %v", *lines)
	}
}

func TestHandleExitPolicyNo(t *testing.T) {
	restarts := supervised(t, PolicyNo, 5, 0, 1111, "running")
	lines, logf := collect()
	if err := HandleExit("api", 1111, 3*time.Second, logf); err != nil {
		t.Fatal(err)
	}
	if st, pid, _ := row(t); st != "exited" || pid != 0 || *restarts != 0 {
		t.Errorf("status=%s pid=%d restarts=%d, want exited", st, pid, *restarts)
	}
	if !strings.Contains(strings.Join(*lines, "\n"), "restart policy is 'no'") {
		t.Errorf("log = %v", *lines)
	}
}

func TestHandleExitRestarts(t *testing.T) {
	restarts := supervised(t, PolicyUnlessStopped, 3, 1, 1111, "running")
	var slept time.Duration
	sleep = func(d time.Duration) { slept += d }
	lines, logf := collect()
	if err := HandleExit("api", 1111, 2*time.Second, logf); err != nil {
		t.Fatal(err)
	}
	if *restarts != 1 {
		t.Fatalf("restarts = %d, want 1", *restarts)
	}
	if _, _, count := row(t); count != 2 {
		t.Errorf("restart count = %d, want 2", count)
	}
	if slept != 2*time.Second {
		t.Errorf("backoff before restart = %s, want 2s for the 2nd attempt", slept)
	}
	if !strings.Contains(strings.Join(*lines, "\n"), "attempt 2/3") || !strings.Contains(strings.Join(*lines, "\n"), "PID 4242") {
		t.Errorf("log = %v", *lines)
	}
}

func TestHandleExitGivesUp(t *testing.T) {
	restarts := supervised(t, PolicyUnlessStopped, 3, 3, 1111, "running")
	lines, logf := collect()
	_ = HandleExit("api", 1111, 2*time.Second, logf)
	if st, _, _ := row(t); st != "exited" || *restarts != 0 {
		t.Errorf("status=%s restarts=%d, want exited without restart", st, *restarts)
	}
	if !strings.Contains(strings.Join(*lines, "\n"), "gave up after 3 restarts") {
		t.Errorf("log = %v", *lines)
	}
}

func TestHandleExitStableRunResetsCount(t *testing.T) {
	restarts := supervised(t, PolicyUnlessStopped, 3, 3, 1111, "running")
	_, logf := collect()
	_ = HandleExit("api", 1111, 5*time.Minute, logf)
	if _, _, count := row(t); count != 1 || *restarts != 1 {
		t.Errorf("count=%d restarts=%d, want a fresh first attempt", count, *restarts)
	}
}

func TestHandleExitStopDuringBackoff(t *testing.T) {
	restarts := supervised(t, PolicyUnlessStopped, 5, 0, 1111, "running")
	sleep = func(time.Duration) {
		if err := StopApp("api"); err != nil { // the user runs 'morrow stop' while we wait
			t.Errorf("stop during backoff: %v", err)
		}
	}
	lines, logf := collect()
	_ = HandleExit("api", 1111, time.Second, logf)
	if st, _, _ := row(t); st != "stopped" || *restarts != 0 {
		t.Errorf("status=%s restarts=%d, want the restart cancelled", st, *restarts)
	}
	if !strings.Contains(strings.Join(*lines, "\n"), "restart cancelled") {
		t.Errorf("log = %v", *lines)
	}
}

func TestHandleExitOtherInstanceUntouched(t *testing.T) {
	restarts := supervised(t, PolicyUnlessStopped, 5, 0, 2222, "running")
	_, logf := collect()
	if err := HandleExit("api", 1111, time.Second, logf); err != nil { // stale relay of an older instance
		t.Fatal(err)
	}
	if st, pid, _ := row(t); st != "running" || pid != 2222 || *restarts != 0 {
		t.Errorf("status=%s pid=%d restarts=%d: a stale relay changed the newer instance", st, pid, *restarts)
	}
}

func TestHandleExitRestartFailure(t *testing.T) {
	supervised(t, PolicyUnlessStopped, 5, 0, 1111, "running")
	restartFn = func(string) (int, error) { return 0, errors.New("exec: not found") }
	lines, logf := collect()
	_ = HandleExit("api", 1111, time.Second, logf)
	if st, _, _ := row(t); st != "exited" {
		t.Errorf("status = %s, want exited after a failed restart", st)
	}
	if !strings.Contains(strings.Join(*lines, "\n"), "restart failed") {
		t.Errorf("log = %v", *lines)
	}
}

func TestReconcileStoppingDeadPID(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	pid := deadPID(t)
	if _, err := db.DB.Exec("UPDATE applications SET application_status = 'stopping', application_pid = ? WHERE application_name = 'api'", pid); err != nil {
		t.Fatal(err)
	}
	if st, p, _ := GetAppStatus("api"); st != "stopped" || p != 0 {
		t.Errorf("status = %s/%d, want stopped", st, p)
	}
}
