package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"morrow/internal/config"
	"morrow/internal/crypto"
	"morrow/internal/db"
	"morrow/internal/env"
)

func setupDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MORROW_HOME", dir)
	t.Setenv("MORROW_KEY_PATH", filepath.Join(dir, "test.key"))
	if err := db.InitDB(filepath.Join(dir, "morrow.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.DB.Close() })
	if err := db.EnsureSchemaEmbedded(); err != nil {
		t.Fatalf("EnsureSchemaEmbedded: %v", err)
	}
	// Mirror `morrow init`: only init creates the master key.
	if err := crypto.InitMasterKey(); err != nil {
		t.Fatalf("InitMasterKey: %v", err)
	}
}

func mustCreate(t *testing.T, name string, args []string, wd string) {
	t.Helper()
	if err := CreateApp(name, "/bin/echo", args, wd); err != nil {
		t.Fatalf("CreateApp: %v", err)
	}
}

// sameFile compares paths tolerant of symlink/8.3 differences.
func sameFile(a, b string) bool {
	if a == b {
		return true
	}
	ia, e1 := os.Stat(a)
	ib, e2 := os.Stat(b)
	return e1 == nil && e2 == nil && os.SameFile(ia, ib)
}

func TestResolveWorkingDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	if err := os.Mkdir("sub", 0755); err != nil {
		t.Fatal(err)
	}
	wantSub, _ := filepath.Abs("sub")

	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty", "", "", false},
		{"relative existing", "sub", wantSub, false},
		{"absolute existing", dir, dir, false},
		{"nonexistent", "nope", "", true},
		{"file", file, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := resolveWorkingDir(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if !c.wantErr && !sameFile(got, c.want) {
				t.Errorf("got %q want %q", got, c.want)
			}
			if got != "" && !filepath.IsAbs(got) {
				t.Errorf("result %q not absolute", got)
			}
		})
	}
}

func TestCreateApp(t *testing.T) {
	setupDB(t)
	base := t.TempDir()
	t.Chdir(base)
	if err := os.Mkdir("work", 0755); err != nil {
		t.Fatal(err)
	}

	mustCreate(t, "a", []string{"x"}, "work")
	d, err := GetAppDetail("a")
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs("work")
	if !filepath.IsAbs(d.WorkingDir) || !sameFile(d.WorkingDir, abs) {
		t.Errorf("WorkingDir = %q, want abs of %q", d.WorkingDir, abs)
	}

	mustCreate(t, "nodir", nil, "")
	d, _ = GetAppDetail("nodir")
	if d.WorkingDir != "" {
		t.Errorf("WorkingDir = %q, want empty", d.WorkingDir)
	}

	if err := CreateApp("bad", "/bin/echo", nil, filepath.Join(base, "missing")); err == nil {
		t.Error("expected error for invalid workdir")
	}
	if _, err := GetAppDetail("bad"); err == nil {
		t.Error("app with invalid workdir must not be created")
	}

	err = CreateApp("a", "/bin/echo", nil, "")
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("duplicate: err = %v", err)
	}
}

func TestGetAppDetailMasking(t *testing.T) {
	setupDB(t)
	mustCreate(t, "a", []string{"--port", "8080"}, "")
	// Generated at runtime so no secret-looking literal lives in the source.
	secret := "test-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	for _, e := range []struct {
		k, v string
		sec  bool
	}{{"ZED", "last", false}, {"ALPHA", "first", false}, {"MID_SECRET", secret, true}} {
		if err := env.SetEnv("a", e.k, e.v, e.sec); err != nil {
			t.Fatal(err)
		}
	}

	d, err := GetAppDetail("a")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.EnvironmentVariables["MID_SECRET"]; got != "****" {
		t.Errorf("secured env = %q, want ****", got)
	}
	if d.EnvironmentVariables["ALPHA"] != "first" || d.EnvironmentVariables["ZED"] != "last" {
		t.Errorf("unsecured envs wrong: %v", d.EnvironmentVariables)
	}
	wantCmd := "ALPHA=first MID_SECRET=**** ZED=last /bin/echo --port 8080"
	if d.CommandLine != wantCmd {
		t.Errorf("CommandLine = %q, want %q", d.CommandLine, wantCmd)
	}
	blob, _ := json.Marshal(d)
	if strings.Contains(string(blob), secret) || strings.Contains(d.CommandLine, secret) {
		t.Error("plaintext secret leaked")
	}

	// Still masked when the key is unavailable.
	t.Setenv("MORROW_KEY_PATH", filepath.Join(t.TempDir(), "missing.key"))
	d, err = GetAppDetail("a")
	if err != nil {
		t.Fatal(err)
	}
	if d.EnvironmentVariables["MID_SECRET"] != "****" {
		t.Errorf("locked secured env = %q", d.EnvironmentVariables["MID_SECRET"])
	}
}

func TestCommandLineNoTrailingSpace(t *testing.T) {
	setupDB(t)
	mustCreate(t, "a", nil, "")
	d, err := GetAppDetail("a")
	if err != nil {
		t.Fatal(err)
	}
	if d.CommandLine != "/bin/echo" {
		t.Errorf("CommandLine = %q", d.CommandLine)
	}
}

func TestGetAppDetailUnknown(t *testing.T) {
	setupDB(t)
	if _, err := GetAppDetail("nope"); err == nil {
		t.Error("expected error")
	}
}

func TestUpdateApp(t *testing.T) {
	setupDB(t)
	dir := t.TempDir()
	other := t.TempDir()
	mustCreate(t, "a", nil, dir)

	// nil keeps
	if err := UpdateApp("a", "/bin/true", []string{"1"}, nil); err != nil {
		t.Fatal(err)
	}
	d, _ := GetAppDetail("a")
	if d.WorkingDir == "" || d.ExecutablePath != "/bin/true" || len(d.Arguments) != 1 {
		t.Errorf("nil workdir should keep: %+v", d)
	}

	// invalid dir errors and changes nothing
	bad := filepath.Join(dir, "missing")
	if err := UpdateApp("a", "/bin/false", nil, &bad); err == nil {
		t.Error("expected error for invalid dir")
	}
	d, _ = GetAppDetail("a")
	if d.ExecutablePath != "/bin/true" || d.WorkingDir == "" {
		t.Errorf("failed update modified row: %+v", d)
	}

	// set
	if err := UpdateApp("a", "/bin/true", nil, &other); err != nil {
		t.Fatal(err)
	}
	d, _ = GetAppDetail("a")
	if !sameFile(d.WorkingDir, other) {
		t.Errorf("WorkingDir = %q, want %q", d.WorkingDir, other)
	}

	// clear
	empty := ""
	if err := UpdateApp("a", "/bin/true", nil, &empty); err != nil {
		t.Fatal(err)
	}
	d, _ = GetAppDetail("a")
	if d.WorkingDir != "" {
		t.Errorf("WorkingDir = %q, want cleared", d.WorkingDir)
	}

	// unknown
	if err := UpdateApp("nope", "x", nil, nil); err == nil {
		t.Error("nil workdir: expected not-found error")
	}
	if err := UpdateApp("nope", "x", nil, &empty); err == nil {
		t.Error("ptr workdir: expected not-found error")
	}
}

func TestStartAppErrors(t *testing.T) {
	setupDB(t)
	if _, err := StartApp("nope", nil); err == nil {
		t.Error("expected not-found error")
	}

	mustCreate(t, "run", nil, "")
	if _, err := db.DB.Exec("UPDATE applications SET application_status='running', application_pid=? WHERE application_name='run'", os.Getpid()); err != nil {
		t.Fatal(err)
	}
	_, err := StartApp("run", nil)
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("err = %v, want already running", err)
	}

	mustCreate(t, "sec", nil, "")
	if err := env.SetEnv("sec", "TOKEN", "abc", true); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MORROW_KEY_PATH", filepath.Join(t.TempDir(), "missing.key"))
	_, err = StartApp("sec", nil)
	if err == nil || !strings.Contains(err.Error(), "cannot decrypt secured variable TOKEN") {
		t.Errorf("err = %v, want decrypt error", err)
	}
	if st, pid, _ := GetAppStatus("sec"); st != "stopped" || pid != 0 {
		t.Errorf("status changed to %s/%d", st, pid)
	}
}

func TestDeleteApp(t *testing.T) {
	setupDB(t)
	mustCreate(t, "run", nil, "")
	if _, err := db.DB.Exec("UPDATE applications SET application_status='running', application_pid=? WHERE application_name='run'", os.Getpid()); err != nil {
		t.Fatal(err)
	}
	err := DeleteApp("run", false)
	if err == nil || !strings.Contains(err.Error(), "running") {
		t.Errorf("err = %v", err)
	}
	if _, _, err := GetAppStatus("run"); err != nil {
		t.Error("app should still exist")
	}

	mustCreate(t, "stopped", nil, "")
	if err := env.SetEnv("stopped", "K", "v", false); err != nil {
		t.Fatal(err)
	}
	if err := DeleteApp("stopped", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := GetAppStatus("stopped"); err == nil {
		t.Error("app should be deleted")
	}
	var n int
	db.DB.QueryRow("SELECT count(*) FROM application_environment_variables").Scan(&n)
	if n != 0 {
		t.Errorf("%d orphan env rows", n)
	}
	if err := DeleteApp("nope", false); err == nil {
		t.Error("expected not-found error")
	}
}

func TestListAppsAndStatus(t *testing.T) {
	setupDB(t)
	mustCreate(t, "b", nil, "")
	mustCreate(t, "a", []string{"x"}, "")
	apps, err := ListApps()
	if err != nil || len(apps) != 2 || apps[0].Name != "a" || apps[1].Name != "b" {
		t.Fatalf("ListApps = %+v, %v", apps, err)
	}
	st, pid, err := GetAppStatus("a")
	if err != nil || st != "stopped" || pid != 0 {
		t.Errorf("GetAppStatus = %s %d %v", st, pid, err)
	}
	if _, _, err := GetAppStatus("nope"); err == nil {
		t.Error("expected error")
	}
}

// deadPID returns the PID of a process that has already exited.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatalf("helper process: %v", err)
	}
	return cmd.Process.Pid
}

func TestIsProcessAlive(t *testing.T) {
	cases := []struct {
		name string
		pid  int
		want bool
	}{
		{"self", os.Getpid(), true},
		{"zero", 0, false},
		{"negative", -1, false},
		{"exited", deadPID(t), false},
	}
	for _, c := range cases {
		if got := isProcessAlive(c.pid); got != c.want {
			t.Errorf("%s (pid %d): got %v, want %v", c.name, c.pid, got, c.want)
		}
	}
}

func TestStaleRunningTreatedAsStopped(t *testing.T) {
	setupDB(t)
	mustCreate(t, "ghost", nil, "")
	pid := deadPID(t)
	if _, err := db.DB.Exec("UPDATE applications SET application_status='running', application_pid=? WHERE application_name='ghost'", pid); err != nil {
		t.Fatal(err)
	}

	if st, p, err := GetAppStatus("ghost"); err != nil || st != "stopped" || p != 0 {
		t.Fatalf("GetAppStatus = %s/%d/%v, want stopped/0", st, p, err)
	}
	var st string
	var p int
	if err := db.DB.QueryRow("SELECT application_status, application_pid FROM applications WHERE application_name='ghost'").Scan(&st, &p); err != nil {
		t.Fatal(err)
	}
	if st != "stopped" || p != 0 {
		t.Errorf("not persisted: row is %s/%d", st, p)
	}

	// Reset to stale and check the other read paths and the commands that act on status.
	if _, err := db.DB.Exec("UPDATE applications SET application_status='running', application_pid=? WHERE application_name='ghost'", pid); err != nil {
		t.Fatal(err)
	}
	apps, err := ListApps()
	if err != nil || len(apps) != 1 || apps[0].Status != "stopped" || apps[0].PID != 0 {
		t.Fatalf("ListApps = %+v, %v", apps, err)
	}
	if _, err := db.DB.Exec("UPDATE applications SET application_status='running', application_pid=? WHERE application_name='ghost'", pid); err != nil {
		t.Fatal(err)
	}
	if d, err := GetAppDetail("ghost"); err != nil || d.Status != "stopped" || d.PID != 0 {
		t.Fatalf("GetAppDetail = %+v, %v", d, err)
	}
	if _, err := db.DB.Exec("UPDATE applications SET application_status='running', application_pid=? WHERE application_name='ghost'", pid); err != nil {
		t.Fatal(err)
	}
	if err := StopApp("ghost"); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Errorf("StopApp err = %v, want not running (must not signal a dead PID)", err)
	}
	if _, err := db.DB.Exec("UPDATE applications SET application_status='running', application_pid=? WHERE application_name='ghost'", pid); err != nil {
		t.Fatal(err)
	}
	if err := DeleteApp("ghost", false); err != nil {
		t.Errorf("DeleteApp without force on stale app: %v", err)
	}
}

func TestStartAppReadOnlyDBSpawnsNothing(t *testing.T) {
	setupDB(t)
	mustCreate(t, "ro", nil, "")
	dbPath := filepath.Join(os.Getenv("MORROW_HOME"), "morrow.db")
	rw := db.DB
	ro, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	db.DB = ro
	t.Cleanup(func() { ro.Close(); db.DB = rw })

	_, err = StartApp("ro", nil)
	if err == nil || !strings.Contains(err.Error(), "readonly") {
		t.Fatalf("err = %v, want readonly database error", err)
	}
	if _, statErr := os.Stat(config.GetRelayPIDFilePath("ro")); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("relay was started before the write check (relay PID file exists)")
	}
}

func TestRestartApp(t *testing.T) {
	setupDB(t)

	// Not running: restart behaves like start, so the start error surfaces.
	mustCreate(t, "sec", nil, "")
	if err := env.SetEnv("sec", "TOKEN", "abc", true); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MORROW_KEY_PATH", filepath.Join(t.TempDir(), "missing.key"))
	if _, err := RestartApp("sec"); err == nil || !strings.Contains(err.Error(), "cannot decrypt") {
		t.Errorf("err = %v, want the start error", err)
	}

	if _, err := RestartApp("nope"); err == nil || errors.Is(err, ErrNotRunning) {
		t.Errorf("unknown app: err = %v, want not-found", err)
	}
}

func TestStopAppNotRunning(t *testing.T) {
	setupDB(t)
	mustCreate(t, "idle", nil, "")
	if err := StopApp("idle"); !errors.Is(err, ErrNotRunning) {
		t.Errorf("err = %v, want ErrNotRunning", err)
	}
}
