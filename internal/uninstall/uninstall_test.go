package uninstall

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"morrow/internal/app"
	"morrow/internal/config"
	"morrow/internal/crypto"
	"morrow/internal/db"
	"morrow/internal/env"
	"morrow/internal/inbox"
)

// setup creates a per-user install in a temp MORROW_HOME with an app, a
// secured variable, a log file, inbox keys and folder, plus a file that is
// not Morrow's (like a binary next to a legacy install).
func setup(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("MORROW_HOME", home)
	t.Setenv("MORROW_KEY_PATH", "")
	if err := db.InitDB(config.GetDBPath()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if db.DB != nil {
			db.DB.Close()
		}
	})
	if err := db.EnsureSchemaEmbedded(); err != nil {
		t.Fatal(err)
	}
	if err := crypto.InitMasterKey(); err != nil {
		t.Fatal(err)
	}
	if err := crypto.InitInboxKeys(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.GetInboxDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := app.CreateApp("api", "/bin/true", nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := env.SetEnv("api", "API_TOKEN", "secret-123", true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.GetLogFilePath("api"), []byte("log"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "morrow.exe"), []byte("not ours"), 0755); err != nil {
		t.Fatal(err)
	}
	return home
}

func has(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestBuildKeep(t *testing.T) {
	home := setup(t)
	p, err := Build(false)
	if err != nil {
		t.Fatal(err)
	}
	if p.System || len(p.Delete) != 0 || len(p.Secured) != 0 {
		t.Errorf("plan = %+v", p)
	}
	for _, f := range []string{filepath.Join(home, "morrow.db"), filepath.Join(home, ".morrow.key"), filepath.Join(home, "logs")} {
		if !has(p.Keep, f) {
			t.Errorf("Keep is missing %s: %v", f, p.Keep)
		}
	}
	if !has(p.Inbox, config.GetInboxDir()) || !has(p.Inbox, config.GetInboxPrivateKeyPath()) {
		t.Errorf("Inbox = %v", p.Inbox)
	}
	if p.Hook != "" || p.Group != "" {
		t.Errorf("per-user install must not touch system hook/group: %+v", p)
	}
}

func TestExecuteKeepThenResume(t *testing.T) {
	home := setup(t)
	p, _ := Build(false)
	if err := Execute(p, ""); err != nil {
		t.Fatal(err)
	}
	RemoveEmptyDirs(p)
	for _, f := range []string{config.GetInboxDir(), config.GetInboxPublicKeyPath(), config.GetInboxPrivateKeyPath()} {
		if exists(f) {
			t.Errorf("%s should be removed", f)
		}
	}
	for _, f := range []string{"morrow.db", ".morrow.key", "logs", "morrow.exe"} {
		if !exists(filepath.Join(home, f)) {
			t.Errorf("%s should be kept", f)
		}
	}
	// Reopening resumes with the app and its secret.
	if v, _, err := env.GetEnv("api", "API_TOKEN"); err != nil || v != "secret-123" {
		t.Errorf("after uninstall without --purge: %q, %v", v, err)
	}
}

func TestPurge(t *testing.T) {
	home := setup(t)
	p, err := Build(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Secured) != 1 || p.Secured[0].Name != "API_TOKEN" {
		t.Errorf("Secured = %v", p.Secured)
	}
	if !strings.Contains(strings.Join(p.Lines(), "\n"), "unrecoverable") {
		t.Errorf("plan does not warn about secured variables:\n%s", strings.Join(p.Lines(), "\n"))
	}
	if err := Execute(p, ""); err != nil {
		t.Fatal(err)
	}
	RemoveEmptyDirs(p)
	entries, _ := os.ReadDir(home)
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if len(left) != 1 || left[0] != "morrow.exe" {
		t.Errorf("after purge %s contains %v, want only the file that is not Morrow's", home, left)
	}
}

func TestPurgeRemovesEmptyHome(t *testing.T) {
	home := setup(t)
	if err := os.Remove(filepath.Join(home, "morrow.exe")); err != nil {
		t.Fatal(err)
	}
	p, _ := Build(true)
	if err := Execute(p, ""); err != nil {
		t.Fatal(err)
	}
	RemoveEmptyDirs(p)
	if exists(home) {
		t.Errorf("empty MORROW_HOME %s should be removed", home)
	}
}

func TestPurgeBackupKey(t *testing.T) {
	setup(t)
	key, _ := os.ReadFile(config.GetKeyPath())
	backup := filepath.Join(t.TempDir(), "morrow.key.bak")

	p, _ := Build(true)
	if err := Execute(p, backup); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(backup)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("backup = %d bytes, %v; want the 32-byte key", len(got), err)
	}
}

func TestPurgeBackupFailureDeletesNothing(t *testing.T) {
	home := setup(t)
	existingFile := filepath.Join(t.TempDir(), "taken")
	if err := os.WriteFile(existingFile, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	p, _ := Build(true)
	if err := Execute(p, existingFile); err == nil || !strings.Contains(err.Error(), "nothing purged") {
		t.Fatalf("Execute = %v, want a refused backup", err)
	}
	for _, f := range []string{"morrow.db", ".morrow.key", "logs"} {
		if !exists(filepath.Join(home, f)) {
			t.Errorf("%s was deleted although the key backup failed", f)
		}
	}
}

func TestExecuteRefusesDuringApply(t *testing.T) {
	setup(t)
	unlock, err := inbox.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	p, _ := Build(true)
	if err := Execute(p, ""); err == nil || !strings.Contains(err.Error(), "morrow apply") {
		t.Fatalf("Execute = %v, want refusal while apply holds the lock", err)
	}
	if !exists(config.GetKeyPath()) {
		t.Error("key deleted while apply was running")
	}
}
