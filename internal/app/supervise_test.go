package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"morrow/internal/db"
	"morrow/internal/hooks"
)

// TestHelperProcess is not a real test: Supervise tests run the test binary
// as the supervised app, and it exits with MORROW_HELPER_EXIT.
func TestHelperProcess(t *testing.T) {
	code := os.Getenv("MORROW_HELPER_EXIT")
	if code == "" {
		return
	}
	if code == "sleep" { // a long-running app
		time.Sleep(time.Minute)
		return
	}
	fmt.Println("helper output")
	n, _ := strconv.Atoi(code)
	os.Exit(n)
}

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
	for _, p := range []string{PolicyNo, PolicyOnFailure, PolicyUnlessStopped} {
		if err := SetRestartPolicy("api", p, 7); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
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

func TestExitInfo(t *testing.T) {
	cases := []struct {
		e      ExitInfo
		failed bool
		text   string
	}{
		{ExitInfo{Code: 0}, false, "exit code 0"},
		{ExitInfo{Code: 3}, true, "exit code 3"},
		{ExitInfo{Code: -1, Signal: "SIGKILL"}, true, "killed by SIGKILL"},
	}
	for _, c := range cases {
		if c.e.Failed() != c.failed || c.e.String() != c.text {
			t.Errorf("%+v: Failed=%v String=%q", c.e, c.e.Failed(), c.e.String())
		}
	}
}

// running creates app "api" recorded as running with pid under a policy.
func running(t *testing.T, policy string, max, count, pid int, status string) {
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
}

func row(t *testing.T) (status string, pid, count int, code sql.NullInt64, signal string) {
	t.Helper()
	if err := db.DB.QueryRow(`SELECT application_status, application_pid, application_restart_count,
		application_last_exit_code, application_last_exit_signal FROM applications WHERE application_name = 'api'`).
		Scan(&status, &pid, &count, &code, &signal); err != nil {
		t.Fatal(err)
	}
	return
}

func collect() (*[]string, func(string)) {
	var lines []string
	return &lines, func(s string) { lines = append(lines, s) }
}

func TestDecide(t *testing.T) {
	type want struct {
		status  string
		restart bool
		events  string
		count   int
		log     string
	}
	cases := []struct {
		name       string
		policy     string
		max, count int
		status     string
		uptime     time.Duration
		exit       ExitInfo
		w          want
	}{
		{"requested stop", PolicyUnlessStopped, 5, 0, "stopping", time.Second, ExitInfo{Code: -1, Signal: "SIGTERM"},
			want{"stopped", false, "", 0, "stopped (killed by SIGTERM)"}},
		{"policy no, crash", PolicyNo, 5, 0, "running", time.Second, ExitInfo{Code: 3},
			want{"exited", false, "crash", 0, "restart policy is 'no'"}},
		{"policy no, clean exit", PolicyNo, 5, 0, "running", time.Second, ExitInfo{Code: 0},
			want{"exited", false, "exit", 0, "(exit code 0)"}},
		{"on-failure, clean exit", PolicyOnFailure, 5, 0, "running", time.Second, ExitInfo{Code: 0},
			want{"exited", false, "exit", 0, "restart policy is 'on-failure'"}},
		{"on-failure, crash", PolicyOnFailure, 5, 1, "running", time.Second, ExitInfo{Code: 3},
			want{"restarting", true, "crash", 2, "restarting in 2s (attempt 2/5)"}},
		{"on-failure, signal", PolicyOnFailure, 5, 0, "running", time.Second, ExitInfo{Code: -1, Signal: "SIGKILL"},
			want{"restarting", true, "crash", 1, "(killed by SIGKILL); restarting in 1s"}},
		{"unless-stopped, clean exit", PolicyUnlessStopped, 5, 0, "running", time.Second, ExitInfo{Code: 0},
			want{"restarting", true, "exit", 1, "attempt 1/5"}},
		{"gives up", PolicyUnlessStopped, 3, 3, "running", time.Second, ExitInfo{Code: 1},
			want{"exited", false, "crash,giveup", 3, "gave up after 3 restarts"}},
		{"stable run resets count", PolicyUnlessStopped, 3, 3, "running", 5 * time.Minute, ExitInfo{Code: 1},
			want{"restarting", true, "crash", 1, "attempt 1/3"}},
		{"unhealthy, policy no", PolicyNo, 5, 0, "running", time.Minute, ExitInfo{Code: -1, Signal: "SIGTERM", Unhealthy: true},
			want{"restarting", true, "unhealthy", 1, "was stopped as unhealthy after 1m0s (killed by SIGTERM); restarting in 1s"}},
		{"unhealthy, budget used up", PolicyOnFailure, 2, 2, "running", time.Second, ExitInfo{Code: 0, Unhealthy: true},
			want{"exited", false, "unhealthy,giveup", 2, "was stopped as unhealthy after 1s (exit code 0); gave up after 2 restarts"}},
		{"unhealthy, but stopped by the user", PolicyNo, 5, 0, "stopping", time.Second, ExitInfo{Code: -1, Signal: "SIGTERM", Unhealthy: true},
			want{"stopped", false, "", 0, "stopped"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			running(t, c.policy, c.max, c.count, 1111, c.status)
			lines, logf := collect()
			d, err := decide("api", 1111, c.uptime, c.exit, logf)
			if err != nil {
				t.Fatal(err)
			}
			st, _, count, code, sig := row(t)
			if st != c.w.status || d.restart != c.w.restart || strings.Join(d.events, ",") != c.w.events {
				t.Errorf("status=%s restart=%v events=%v, want %+v", st, d.restart, d.events, c.w)
			}
			if c.w.restart && (count != c.w.count || d.wait != backoff(c.w.count)) {
				t.Errorf("count=%d wait=%s, want count %d", count, d.wait, c.w.count)
			}
			if !code.Valid || int(code.Int64) != c.exit.Code || sig != c.exit.Signal {
				t.Errorf("recorded exit = %v/%q, want %+v", code, sig, c.exit)
			}
			if !strings.Contains(strings.Join(*lines, "\n"), c.w.log) {
				t.Errorf("log %v does not contain %q", *lines, c.w.log)
			}
		})
	}
}

func TestDecideOtherInstanceUntouched(t *testing.T) {
	running(t, PolicyUnlessStopped, 5, 0, 2222, "running")
	_, logf := collect()
	d, err := decide("api", 1111, time.Second, ExitInfo{Code: 1}, logf) // stale relay of an older instance
	if err != nil {
		t.Fatal(err)
	}
	if st, pid, _, code, _ := row(t); st != "running" || pid != 2222 || code.Valid || d.restart || len(d.events) != 0 {
		t.Errorf("status=%s pid=%d exit=%v decision=%+v: a stale relay changed the newer instance", st, pid, code, d)
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

type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }

// syncBuffer is safe for the app-output copier and log lines written at once,
// like the lumberjack logger the relay uses.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}
func (s *syncBuffer) String() string { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

// superviseHelper runs Supervise on the test binary acting as an app that
// exits with code, with backoff sleeps skipped and hooks recorded.
func superviseHelper(t *testing.T, code, policy string, max int) (report, log string, events []hooks.Event) {
	t.Helper()
	setupDB(t)
	if err := CreateApp("api", os.Args[0], []string{"-test.run=^TestHelperProcess$"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := SetRestartPolicy("api", policy, max); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MORROW_HELPER_EXIT", code)
	var mu sync.Mutex
	origSleep, origFire := sleep, fireHooks
	sleep = func(time.Duration) {}
	fireHooks = func(ev hooks.Event, logf func(string)) { mu.Lock(); events = append(events, ev); mu.Unlock() }
	t.Cleanup(func() { sleep, fireHooks = origSleep, origFire })

	var rep bytes.Buffer
	var logs syncBuffer
	if err := Supervise("api", StartRequest{UserStart: true, InlineEnvs: map[string]string{"X": "1"}}, nopCloser{&rep}, &logs); err != nil {
		t.Fatalf("Supervise: %v", err)
	}
	return rep.String(), logs.String(), events
}

func TestSuperviseCrashLoopGivesUp(t *testing.T) {
	report, log, events := superviseHelper(t, "3", PolicyOnFailure, 2)
	if !strings.HasPrefix(report, "started ") {
		t.Errorf("report = %q", report)
	}
	if strings.Count(log, "helper output") != 3 {
		t.Errorf("want 3 runs (start + 2 restarts), log:\n%s", log)
	}
	for _, want := range []string{"(exit code 3); restarting in 1s (attempt 1/2)", "restarted with PID", "gave up after 2 restarts"} {
		if !strings.Contains(log, want) {
			t.Errorf("log missing %q:\n%s", want, log)
		}
	}
	st, pid, count, code, _ := row(t)
	if st != "exited" || pid != 0 || count != 2 || !code.Valid || code.Int64 != 3 {
		t.Errorf("status=%s pid=%d count=%d code=%v", st, pid, count, code)
	}
	var names []string
	for _, e := range events {
		names = append(names, e.Name)
		if e.App != "api" || e.ExitCode != 3 {
			t.Errorf("event %+v", e)
		}
	}
	if len(names) != 4 || strings.Count(strings.Join(names, ","), "crash") != 3 || !strings.Contains(strings.Join(names, ","), "giveup") {
		t.Errorf("events = %v, want three crashes and a giveup", names)
	}
}

func TestSuperviseCleanExitOnFailure(t *testing.T) {
	_, log, events := superviseHelper(t, "0", PolicyOnFailure, 5)
	if strings.Count(log, "helper output") != 1 || !strings.Contains(log, "not restarting (restart policy is 'on-failure')") {
		t.Errorf("a clean exit must not be restarted under on-failure:\n%s", log)
	}
	if st, _, _, code, _ := row(t); st != "exited" || code.Int64 != 0 {
		t.Errorf("status=%s code=%v", st, code)
	}
	if len(events) != 1 || events[0].Name != hooks.EventExit {
		t.Errorf("events = %+v, want one exit event", events)
	}
}

func TestSuperviseStartFailureIsReported(t *testing.T) {
	setupDB(t)
	if err := CreateApp("api", "/nonexistent/morrow-test-binary", nil, ""); err != nil {
		t.Fatal(err)
	}
	var rep, logs bytes.Buffer
	if err := Supervise("api", StartRequest{UserStart: true}, nopCloser{&rep}, &logs); err == nil {
		t.Fatal("Supervise succeeded starting a missing executable")
	}
	if !strings.HasPrefix(rep.String(), "error ") {
		t.Errorf("report = %q, want an error line", rep.String())
	}
	if st, _, _, _, _ := row(t); st == "running" {
		t.Error("a failed start was recorded as running")
	}
}

func TestStartRequestJSON(t *testing.T) {
	b, _ := json.Marshal(StartRequest{UserStart: true, InlineEnvs: map[string]string{"A": "1"}})
	var r StartRequest
	if err := json.Unmarshal(b, &r); err != nil || !r.UserStart || r.InlineEnvs["A"] != "1" {
		t.Errorf("round trip = %+v, %v", r, err)
	}
}
