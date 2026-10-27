package install

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"morrow/internal/config"
	"morrow/internal/privilege"
)

func layoutIn(dir string) config.Layout {
	return config.Layout{
		DBPath:  filepath.Join(dir, "lib", "morrow.db"),
		KeyPath: filepath.Join(dir, "etc", ".morrow.key"),
		LogsDir: filepath.Join(dir, "log"),
	}
}

func legacyIn(t *testing.T, dir string) config.Layout {
	t.Helper()
	l := config.Layout{
		DBPath:  filepath.Join(dir, "morrow.db"),
		KeyPath: filepath.Join(dir, ".morrow.key"),
		LogsDir: filepath.Join(dir, "logs"),
	}
	write(t, l.DBPath, "db")
	write(t, l.KeyPath, strings.Repeat("k", 32))
	write(t, filepath.Join(l.LogsDir, "api.log"), "hello")
	write(t, filepath.Join(l.LogsDir, "api.relay.pid"), "123")
	return l
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func gone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("%s should have been removed (err %v)", path, err)
	}
}

// reclaim gives a key restricted for a system install back to the test user,
// who keeps WRITE_DAC as its owner, so tests that do not run elevated
// (Windows) can read it and TempDir cleanup can delete it.
func reclaim(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		if err := privilege.RestrictKeyFile(path, true); err != nil {
			t.Fatal(err)
		}
	}
}

// checkKeyRestricted checks the key mode; the Windows ACL itself is covered
// by the privilege package tests.
func checkKeyRestricted(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("key mode = %o, want 600", mode)
	}
}

// prepared returns an empty target layout. Its directories are created
// plainly: PrepareSystem would lock a non-elevated test out on Windows.
func prepared(t *testing.T) config.Layout {
	t.Helper()
	to := layoutIn(t.TempDir())
	for _, dir := range to.Dirs() {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { reclaim(t, to.KeyPath) })
	return to
}

func TestMigrateLegacy(t *testing.T) {
	from := legacyIn(t, t.TempDir())
	to := prepared(t)

	res, err := MigrateLegacy(from, to, true)
	if err != nil {
		t.Fatal(err)
	}
	checkKeyRestricted(t, to.KeyPath)
	reclaim(t, to.KeyPath)
	if !res.Migrated || len(res.Warnings) != 0 {
		t.Fatalf("got %+v", res)
	}
	if read(t, to.DBPath) != "db" || read(t, to.KeyPath) != strings.Repeat("k", 32) {
		t.Error("database or key not copied")
	}
	if read(t, filepath.Join(to.LogsDir, "api.log")) != "hello" || read(t, filepath.Join(to.LogsDir, "api.relay.pid")) != "123" {
		t.Error("logs or relay PID files not copied")
	}
	gone(t, from.DBPath)
	gone(t, from.KeyPath)
	gone(t, to.DBPath+".migrating")
	if read(t, filepath.Join(from.LogsDir, "api.log")) != "hello" {
		t.Error("legacy logs must stay for apps still writing to them")
	}

	// Running init again is a no-op.
	if res, err := MigrateLegacy(from, to, true); err != nil || res.Migrated {
		t.Errorf("second run: %+v, %v", res, err)
	}
}

func TestMigrateLegacyKeepsOverriddenKey(t *testing.T) {
	from := legacyIn(t, t.TempDir())
	to := prepared(t)

	if _, err := MigrateLegacy(from, to, false); err != nil {
		t.Fatal(err)
	}
	if read(t, from.KeyPath) == "" {
		t.Error("key outside the managed layout must stay")
	}
	gone(t, to.KeyPath)
}

func TestMigrateLegacySkips(t *testing.T) {
	t.Run("nothing to migrate", func(t *testing.T) {
		to := prepared(t)
		empty := layoutIn(t.TempDir())
		if res, err := MigrateLegacy(empty, to, true); err != nil || res.Migrated {
			t.Errorf("got %+v, %v", res, err)
		}
	})
	t.Run("new location already in use", func(t *testing.T) {
		from := legacyIn(t, t.TempDir())
		to := prepared(t)
		write(t, to.DBPath, "new")
		if res, err := MigrateLegacy(from, to, true); err != nil || res.Migrated {
			t.Errorf("got %+v, %v", res, err)
		}
		if read(t, to.DBPath) != "new" || read(t, from.DBPath) != "db" {
			t.Error("existing data was touched")
		}
	})
}

func TestMigrateLegacyRefusesDifferentKey(t *testing.T) {
	from := legacyIn(t, t.TempDir())
	to := prepared(t)
	write(t, to.KeyPath, strings.Repeat("x", 32))

	_, err := MigrateLegacy(from, to, true)
	if err == nil || !strings.Contains(err.Error(), "different master key") {
		t.Fatalf("want different-key error, got %v", err)
	}
	if read(t, to.KeyPath) != strings.Repeat("x", 32) || read(t, from.DBPath) != "db" {
		t.Error("keys or legacy database changed")
	}
	gone(t, to.DBPath)
}

func TestMigrateLegacyResumesAfterInterruption(t *testing.T) {
	from := legacyIn(t, t.TempDir())
	to := prepared(t)
	// A previous run copied the key and logs but died before the database.
	write(t, to.KeyPath, strings.Repeat("k", 32))
	write(t, to.DBPath+".migrating", "partial")

	res, err := MigrateLegacy(from, to, true)
	if err != nil || !res.Migrated {
		t.Fatalf("got %+v, %v", res, err)
	}
	if read(t, to.DBPath) != "db" {
		t.Error("database not taken from the legacy location")
	}
}

func TestMigrateLegacyCopiesJournal(t *testing.T) {
	from := legacyIn(t, t.TempDir())
	write(t, from.DBPath+"-journal", "hot")
	to := prepared(t)

	if _, err := MigrateLegacy(from, to, true); err != nil {
		t.Fatal(err)
	}
	if read(t, to.DBPath+"-journal") != "hot" {
		t.Error("journal not copied with the database")
	}
	gone(t, from.DBPath+"-journal")
}

func TestPrepareUser(t *testing.T) {
	home := filepath.Join(t.TempDir(), "not", "yet", ".morrow")
	l := config.Layout{
		DBPath:  filepath.Join(home, "morrow.db"),
		KeyPath: filepath.Join(home, ".morrow.key"),
		LogsDir: filepath.Join(home, "logs"),
	}
	if err := PrepareUser(l); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{home, l.LogsDir} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Fatalf("%s not created: %v", dir, err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0700 {
			t.Errorf("%s mode = %o, want 700", dir, info.Mode().Perm())
		}
	}
}
