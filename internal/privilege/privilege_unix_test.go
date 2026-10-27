//go:build !windows

package privilege

import (
	"os"
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
