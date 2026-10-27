package config

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeInstall points the system root and the binary's directory at temp dirs
// and clears the environment overrides.
func fakeInstall(t *testing.T) (root, bin string) {
	t.Helper()
	root, bin = t.TempDir(), t.TempDir()
	oldRoot, oldExe := systemRoot, exeDir
	systemRoot = root
	exeDir = func() (string, error) { return bin, nil }
	t.Cleanup(func() { systemRoot, exeDir = oldRoot, oldExe })
	t.Setenv("MORROW_HOME", "")
	t.Setenv("MORROW_KEY_PATH", "")
	return root, bin
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSystemLayoutIsUnderRoot(t *testing.T) {
	root, bin := fakeInstall(t)
	l := Current()
	if l != systemLayout(root) {
		t.Fatalf("fresh system install: got %+v, want %+v", l, systemLayout(root))
	}
	for _, p := range []string{l.DBPath, l.KeyPath, l.LogsDir} {
		if rel, err := filepath.Rel(root, p); err != nil || rel == "." || filepath.IsAbs(rel) || rel[:2] == ".." {
			t.Errorf("%s is not under the system root %s", p, root)
		}
		if filepath.Dir(p) == bin {
			t.Errorf("%s is next to the binary", p)
		}
	}
	if filepath.Base(l.DBPath) != dbFile || filepath.Base(l.KeyPath) != keyFile {
		t.Errorf("unexpected file names: %+v", l)
	}
	if UsingLegacy() || IsPerUser() {
		t.Error("fresh system install reported as legacy or per-user")
	}
}

func TestPerUserLayout(t *testing.T) {
	_, bin := fakeInstall(t)
	home := t.TempDir()
	t.Setenv("MORROW_HOME", home)
	// Legacy data must not matter in per-user mode.
	touch(t, filepath.Join(bin, dbFile))

	if got, want := Current(), flatLayout(home); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if !IsPerUser() || UsingLegacy() {
		t.Error("per-user install misdetected")
	}
	if GetDataDir() != home {
		t.Errorf("GetDataDir = %s, want %s", GetDataDir(), home)
	}
}

func TestKeyPathOverride(t *testing.T) {
	root, bin := fakeInstall(t)
	key := filepath.Join(t.TempDir(), "custom.key")
	t.Setenv("MORROW_KEY_PATH", key)

	if GetKeyPath() != key {
		t.Errorf("system: key = %s, want %s", GetKeyPath(), key)
	}
	if GetDBPath() != systemLayout(root).DBPath {
		t.Errorf("system: override moved the database to %s", GetDBPath())
	}

	touch(t, filepath.Join(bin, dbFile))
	if GetKeyPath() != key || GetDBPath() != filepath.Join(bin, dbFile) {
		t.Errorf("legacy: got db %s key %s", GetDBPath(), GetKeyPath())
	}

	home := t.TempDir()
	t.Setenv("MORROW_HOME", home)
	if GetKeyPath() != key || GetDBPath() != filepath.Join(home, dbFile) {
		t.Errorf("per-user: got db %s key %s", GetDBPath(), GetKeyPath())
	}
}

func TestLegacyFallback(t *testing.T) {
	root, bin := fakeInstall(t)
	touch(t, filepath.Join(bin, dbFile))

	if !UsingLegacy() {
		t.Fatal("legacy database next to the binary not detected")
	}
	if got, want := Current(), flatLayout(bin); got != want {
		t.Fatalf("got %+v, want legacy %+v", got, want)
	}
	if GetLogFilePath("api") != filepath.Join(bin, logsDir, "api.log") {
		t.Errorf("logs not in legacy dir: %s", GetLogFilePath("api"))
	}

	// Once init has migrated, the new location wins even if legacy files remain.
	touch(t, systemLayout(root).DBPath)
	if UsingLegacy() {
		t.Error("still legacy after the new database exists")
	}
	if got := Current(); got != systemLayout(root) {
		t.Errorf("got %+v after migration, want system layout", got)
	}
}

func TestLayoutDirs(t *testing.T) {
	flat := flatLayout("/x")
	if got := flat.Dirs(); len(got) != 2 {
		t.Errorf("flat layout dirs = %v, want the base and its logs dir", got)
	}
	sys := systemLayout(t.TempDir())
	if got := sys.Dirs(); got[0] != filepath.Dir(sys.KeyPath) {
		t.Errorf("key dir must come first: %v", got)
	}
}
