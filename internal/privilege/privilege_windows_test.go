//go:build windows

package privilege

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func keySDDL(t *testing.T, path string) string {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	return sd.String()
}

func TestRestrictKeyFile(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := user.User.Sid.String()

	t.Run("per-user", func(t *testing.T) {
		path := writeTempKey(t)
		if err := RestrictKeyFile(path, true); err != nil {
			t.Fatal(err)
		}
		sddl := keySDDL(t, path)
		if !strings.HasPrefix(sddl, "D:P") || !strings.Contains(sddl, sid) {
			t.Errorf("want protected DACL for %s only, got %s", sid, sddl)
		}
		if strings.Contains(sddl, ";BU)") || strings.Contains(sddl, ";SY)") {
			t.Errorf("unexpected extra principals: %s", sddl)
		}
		if _, err := os.ReadFile(path); err != nil {
			t.Errorf("owner can no longer read key: %v", err)
		}
	})

	t.Run("system", func(t *testing.T) {
		path := writeTempKey(t)
		if err := RestrictKeyFile(path, false); err != nil {
			t.Fatal(err)
		}
		sddl := keySDDL(t, path)
		// Give the file back to the test user so TempDir cleanup can delete it
		// when the test does not run elevated. The owner keeps WRITE_DAC.
		t.Cleanup(func() { _ = RestrictKeyFile(path, true) })
		if !strings.HasPrefix(sddl, "D:P") || !strings.Contains(sddl, ";SY)") || !strings.Contains(sddl, ";BA)") {
			t.Errorf("want protected DACL for SYSTEM+Administrators, got %s", sddl)
		}
		if strings.Contains(sddl, ";BU)") || strings.Contains(sddl, sid) {
			t.Errorf("users must not have access: %s", sddl)
		}
	})
}
