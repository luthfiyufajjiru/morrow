//go:build !windows

package privilege

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRestrictKeyFile(t *testing.T) {
	path := writeTempKey(t)
	if err := RestrictKeyFile(path, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("mode = %o, want 600", mode)
	}
}

func TestPrepareSystemDir(t *testing.T) {
	for _, c := range []struct {
		private bool
		want    os.FileMode
	}{{false, 0755}, {true, 0700}} {
		dir := filepath.Join(t.TempDir(), "a", "morrow")
		// Existing directories are fixed up too.
		if err := os.MkdirAll(dir, 0777); err != nil {
			t.Fatal(err)
		}
		if err := PrepareSystemDir(dir, c.private); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != c.want {
			t.Errorf("private=%v: mode = %o, want %o", c.private, mode, c.want)
		}
	}
}
