//go:build !windows

package app

import (
	"bytes"
	"os"
	"os/user"
	"strings"
	"testing"
)

func TestRunAsUnix(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if err := SetRunAs("api", me.Username, "secret"); err == nil || !strings.Contains(err.Error(), "only used on Windows") {
		t.Errorf("password accepted on Unix: %v", err)
	}
	if err := SetRunAs("api", me.Username, ""); err != nil {
		t.Fatal(err)
	}
	if d, _ := GetAppDetail("api"); d.RunAs != me.Username || storedSecret(t) != "" {
		t.Errorf("RunAs = %q, secret %q", d.RunAs, storedSecret(t))
	}
}

// Running as one's own account goes through the run-as path without root.
func TestSuperviseRunAsSelf(t *testing.T) {
	me, _ := user.Current()
	setupDB(t)
	if err := CreateApp("api", os.Args[0], []string{"-test.run=^TestHelperProcess$"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := SetRunAs("api", me.Username, ""); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MORROW_HELPER_EXIT", "4")
	var rep bytes.Buffer
	var logs syncBuffer
	if err := Supervise("api", StartRequest{UserStart: true}, nopCloser{&rep}, &logs); err != nil {
		t.Fatalf("Supervise: %v (log %s)", err, logs.String())
	}
	if !strings.HasPrefix(rep.String(), "started ") || !strings.Contains(logs.String(), "helper output") {
		t.Errorf("report %q, log %q", rep.String(), logs.String())
	}
	if _, _, _, code, _ := row(t); code.Int64 != 4 {
		t.Errorf("exit code = %v, want 4", code)
	}
}

func TestRunAsOtherAccountNeedsRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	setupDB(t)
	if err := CreateApp("api", "/bin/true", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := SetRunAs("api", "root", ""); err != nil {
		t.Fatal(err)
	}
	var rep bytes.Buffer
	var logs syncBuffer
	err := Supervise("api", StartRequest{UserStart: true}, nopCloser{&rep}, &logs)
	if err == nil || !strings.Contains(err.Error(), "requires root") || !strings.HasPrefix(rep.String(), "error ") {
		t.Errorf("Supervise as root without root: err=%v report=%q", err, rep.String())
	}
}
