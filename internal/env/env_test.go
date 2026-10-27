package env

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"morrow/internal/crypto"
	"morrow/internal/db"
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

func addApp(t *testing.T, name string) {
	t.Helper()
	_, err := db.DB.Exec(`INSERT INTO applications (application_id, application_name, application_executable_path, application_arguments)
		VALUES (?, ?, 'x', '[]')`, "id-"+name, name)
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnknownApp(t *testing.T) {
	setupDB(t)
	if err := SetEnv("nope", "K", "v", false); err == nil {
		t.Error("SetEnv: expected error")
	}
	if _, _, err := GetEnv("nope", "K"); err == nil {
		t.Error("GetEnv: expected error")
	}
	if err := DelEnv("nope", "K"); err == nil {
		t.Error("DelEnv: expected error")
	}
	if _, err := ListEnv("nope"); err == nil {
		t.Error("ListEnv: expected error")
	}
}

func TestUnsecuredSetGet(t *testing.T) {
	setupDB(t)
	addApp(t, "a")
	if err := SetEnv("a", "K", "plain", false); err != nil {
		t.Fatal(err)
	}
	v, sec, err := GetEnv("a", "K")
	if err != nil || v != "plain" || sec {
		t.Fatalf("got (%q,%v,%v)", v, sec, err)
	}
	if _, _, err := GetEnv("a", "MISSING"); err == nil {
		t.Error("expected error for missing key")
	}
}

func TestSecuredStoredEncrypted(t *testing.T) {
	setupDB(t)
	addApp(t, "a")
	if err := SetEnv("a", "S", "topsecret", true); err != nil {
		t.Fatal(err)
	}
	var raw string
	var flag int
	if err := db.DB.QueryRow("SELECT env_value, env_is_secured FROM application_environment_variables WHERE env_name='S'").Scan(&raw, &flag); err != nil {
		t.Fatal(err)
	}
	if raw == "topsecret" || raw == "" {
		t.Errorf("stored value not encrypted: %q", raw)
	}
	if flag != 1 {
		t.Errorf("env_is_secured = %d, want 1", flag)
	}
	v, sec, err := GetEnv("a", "S")
	if err != nil || v != "topsecret" || !sec {
		t.Fatalf("got (%q,%v,%v)", v, sec, err)
	}
}

func TestUpsert(t *testing.T) {
	setupDB(t)
	addApp(t, "a")
	steps := []struct {
		val     string
		secured bool
	}{{"one", false}, {"two", true}, {"three", false}}
	for _, s := range steps {
		if err := SetEnv("a", "K", s.val, s.secured); err != nil {
			t.Fatal(err)
		}
		v, sec, err := GetEnv("a", "K")
		if err != nil || v != s.val || sec != s.secured {
			t.Fatalf("after set %+v got (%q,%v,%v)", s, v, sec, err)
		}
	}
	var n int
	db.DB.QueryRow("SELECT count(*) FROM application_environment_variables").Scan(&n)
	if n != 1 {
		t.Errorf("rows = %d, want 1", n)
	}
}

func byName(vars []EnvVar) map[string]EnvVar {
	m := map[string]EnvVar{}
	for _, v := range vars {
		m[v.Name] = v
	}
	return m
}

func TestListEnvAndLocked(t *testing.T) {
	setupDB(t)
	addApp(t, "a")
	addApp(t, "b")
	if err := SetEnv("a", "PLAIN", "p", false); err != nil {
		t.Fatal(err)
	}
	if err := SetEnv("a", "SEC", "s", true); err != nil {
		t.Fatal(err)
	}
	if err := SetEnv("b", "OTHER", "o", false); err != nil {
		t.Fatal(err)
	}

	vars, err := ListEnv("a")
	if err != nil {
		t.Fatal(err)
	}
	m := byName(vars)
	if len(m) != 2 {
		t.Fatalf("got %d vars, want 2 (no leakage from app b)", len(m))
	}
	if v := m["SEC"]; v.Value != "s" || !v.IsSecured || v.Locked {
		t.Errorf("SEC = %+v", v)
	}
	if v := m["PLAIN"]; v.Value != "p" || v.IsSecured || v.Locked {
		t.Errorf("PLAIN = %+v", v)
	}

	t.Setenv("MORROW_KEY_PATH", filepath.Join(t.TempDir(), "missing.key"))
	vars, err = ListEnv("a")
	if err != nil {
		t.Fatalf("ListEnv must not fail when locked: %v", err)
	}
	m = byName(vars)
	if v := m["SEC"]; !v.Locked || v.Value != "" || !v.IsSecured {
		t.Errorf("locked SEC = %+v", v)
	}
	if v := m["PLAIN"]; v.Locked || v.Value != "p" {
		t.Errorf("locked PLAIN = %+v", v)
	}
	if _, _, err := GetEnv("a", "SEC"); err == nil {
		t.Error("GetEnv should fail when key is unreadable")
	}
	if v, _, err := GetEnv("a", "PLAIN"); err != nil || v != "p" {
		t.Errorf("GetEnv PLAIN = %q, %v", v, err)
	}
}

func TestListEnvEmpty(t *testing.T) {
	setupDB(t)
	addApp(t, "a")
	vars, err := ListEnv("a")
	if err != nil || len(vars) != 0 {
		t.Fatalf("got %v, %v", vars, err)
	}
}

func TestDelEnv(t *testing.T) {
	setupDB(t)
	addApp(t, "a")
	if err := DelEnv("a", "K"); err == nil {
		t.Error("expected error deleting missing key")
	}
	if err := SetEnv("a", "K", "v", false); err != nil {
		t.Fatal(err)
	}
	if err := DelEnv("a", "K"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := GetEnv("a", "K"); err == nil {
		t.Error("key should be gone")
	}
}

func TestSetSecuredWithoutKeyFails(t *testing.T) {
	setupDB(t)
	addApp(t, "a")
	keyPath := filepath.Join(os.Getenv("MORROW_HOME"), "missing.key")
	t.Setenv("MORROW_KEY_PATH", keyPath)

	err := SetEnv("a", "TOKEN", "v", true)
	if !errors.Is(err, crypto.ErrNoMasterKey) {
		t.Fatalf("err = %v, want ErrNoMasterKey", err)
	}
	if _, statErr := os.Stat(keyPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("SetEnv must not create a master key")
	}
	var n int
	if err := db.DB.QueryRow("SELECT count(*) FROM application_environment_variables").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d rows written despite encryption failure", n)
	}
	// Unsecured values do not need the key.
	if err := SetEnv("a", "PLAIN", "v", false); err != nil {
		t.Errorf("unsecured SetEnv: %v", err)
	}
}
