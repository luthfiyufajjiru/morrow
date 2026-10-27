package privilege

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"
)

func TestCheckInit(t *testing.T) {
	cases := []struct {
		name     string
		perUser  bool
		elevated bool
		wantErr  bool
	}{
		{"system install, elevated", false, true, false},
		{"system install, not elevated", false, false, true},
		{"per-user, not elevated", true, false, false},
		{"per-user, elevated", true, true, false},
	}
	for _, c := range cases {
		err := CheckInit(c.perUser, c.elevated, "/opt/morrow")
		if (err != nil) != c.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", c.name, err, c.wantErr)
		}
		if err != nil && (!strings.Contains(err.Error(), "MORROW_HOME") || !strings.Contains(err.Error(), "/opt/morrow")) {
			t.Errorf("%s: message should name the data dir and MORROW_HOME: %v", c.name, err)
		}
	}
}

func TestHint(t *testing.T) {
	if Hint(nil) != nil {
		t.Error("Hint(nil) should be nil")
	}
	permErr := &fs.PathError{Op: "open", Path: "/x/.morrow.key", Err: fs.ErrPermission}
	cases := []struct {
		name     string
		err      error
		wantHint bool
	}{
		{"sqlite readonly", errors.New("attempt to write a readonly database (8)"), true},
		{"fs permission", permErr, true},
		{"wrapped permission", fmt.Errorf("decrypt: %w", permErr), true},
		{"windows access denied", errors.New("open C:\\x: Access is denied."), true},
		{"unrelated", errors.New("application api not found"), false},
	}
	for _, c := range cases {
		got := Hint(c.err)
		if hinted := strings.Contains(got.Error(), ElevationHint); hinted != c.wantHint {
			t.Errorf("%s: hinted = %v, want %v (%v)", c.name, hinted, c.wantHint, got)
		}
		if !errors.Is(got, c.err) {
			t.Errorf("%s: original error not wrapped", c.name)
		}
	}
}

func writeTempKey(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + string(os.PathSeparator) + ".morrow.key"
	if err := os.WriteFile(path, make([]byte, 32), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
