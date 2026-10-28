package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"morrow/internal/db"
)

func TestSetHealthCheck(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	if err := SetHealthCheck("api", "http://127.0.0.1:8080/healthz", 5*time.Second, 2); err != nil {
		t.Fatal(err)
	}
	if d, _ := GetAppDetail("api"); d.HealthURL != "http://127.0.0.1:8080/healthz" || d.HealthInterval != 5 || d.HealthFailures != 2 {
		t.Errorf("stored %q every %ds after %d", d.HealthURL, d.HealthInterval, d.HealthFailures)
	}
	for _, bad := range []struct {
		url      string
		interval time.Duration
		failures int
	}{
		{"ftp://x/", time.Second, 1}, {"127.0.0.1:8080", time.Second, 1}, {"http:///x", time.Second, 1},
		{"http://x/", 500 * time.Millisecond, 1}, {"http://x/", 1500 * time.Millisecond, 1}, {"http://x/", 2 * time.Hour, 1},
		{"http://x/", time.Second, 0}, {"http://x/", time.Second, 101},
	} {
		if err := SetHealthCheck("api", bad.url, bad.interval, bad.failures); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if err := SetHealthCheck("api", "", DefaultHealthInterval, DefaultHealthFailures); err != nil {
		t.Fatal(err)
	}
	if d, _ := GetAppDetail("api"); d.HealthURL != "" {
		t.Error("empty URL did not remove the check")
	}
	if err := SetHealthCheck("nope", "", DefaultHealthInterval, DefaultHealthFailures); err == nil {
		t.Error("no error for an unknown app")
	}
}

func TestProbe(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
		case "/redirect":
			http.Redirect(w, r, "/broken", http.StatusFound)
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	c := &http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if err := probe(c, srv.URL+"/ok"); err != nil {
		t.Errorf("200: %v", err)
	}
	if err := probe(c, srv.URL+"/redirect"); err != nil {
		t.Errorf("302 (not followed): %v", err)
	}
	if err := probe(c, srv.URL+"/down"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("503: %v", err)
	}
	srv.Close()
	if err := probe(c, srv.URL+"/ok"); err == nil {
		t.Error("no error for a closed port")
	}
}

// watchHealth stops an app that keeps failing its check, and leaves alone
// one that recovers.
func TestWatchHealth(t *testing.T) {
	var healthy atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !healthy.Load() {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	setupDB(t)
	mustCreate(t, "api", nil, "")

	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), "MORROW_HELPER_EXIT=sleep")
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = forceKill(cmd.Process.Pid); <-exited })
	pid := cmd.Process.Pid
	if _, err := db.DB.Exec("UPDATE applications SET application_status = 'running', application_pid = ? WHERE application_name = 'api'", pid); err != nil {
		t.Fatal(err)
	}
	a := &Application{Name: "api", HealthURL: srv.URL, HealthInterval: 1, HealthFailures: 2}
	lines, logf := collect()
	var unhealthy atomic.Bool
	done := make(chan struct{})
	go func() { watchHealth(a, pid, exited, &unhealthy, logf); close(done) }()
	select {
	case <-exited:
	case <-time.After(15 * time.Second):
		t.Fatal("unhealthy app was not stopped")
	}
	<-done
	log := strings.Join(*lines, "\n")
	if !unhealthy.Load() || !strings.Contains(log, "health check failed (2/2): HTTP 500") || !strings.Contains(log, "unhealthy after 2 failed health checks") {
		t.Errorf("unhealthy=%v log:\n%s", unhealthy.Load(), log)
	}
}
