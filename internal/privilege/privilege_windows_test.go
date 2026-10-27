//go:build windows

package privilege

import (
	"os"
	"path/filepath"
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

// userACE returns the full-access ACE for sid exactly as Windows prints it.
// Well-known accounts are printed as aliases (e.g. the built-in Administrator,
// which CI runners use, becomes "LA"), so the raw SID string cannot be matched.
func userACE(t *testing.T, sid string) string {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString("D:(A;;FA;;;" + sid + ")")
	if err != nil {
		t.Fatal(err)
	}
	s := sd.String()
	return s[strings.Index(s, "("):]
}

func TestRestrictKeyFile(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	ace := userACE(t, user.User.Sid.String())

	t.Run("per-user", func(t *testing.T) {
		path := writeTempKey(t)
		if err := RestrictKeyFile(path, true); err != nil {
			t.Fatal(err)
		}
		sddl := keySDDL(t, path)
		if !strings.HasPrefix(sddl, "D:P") || !strings.Contains(sddl, ace) {
			t.Errorf("want protected DACL with %s only, got %s", ace, sddl)
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
		if strings.Contains(sddl, ";BU)") || strings.Contains(sddl, ace) {
			t.Errorf("users must not have access: %s", sddl)
		}
	})
}

func TestPrepareSystemDir(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []bool{false, true} {
		dir := filepath.Join(t.TempDir(), "Morrow")
		if err := PrepareSystemDir(dir, private); err != nil {
			t.Fatal(err)
		}
		sddl := keySDDL(t, dir)
		// Give the directory back to the test user (who keeps WRITE_DAC as its
		// owner) so TempDir cleanup works when the test does not run elevated.
		t.Cleanup(func() { _ = setDACL(dir, "D:P(A;OICI;FA;;;"+user.User.Sid.String()+")") })
		if !strings.HasPrefix(sddl, "D:P") || !strings.Contains(sddl, ";SY)") || !strings.Contains(sddl, ";BA)") {
			t.Errorf("private=%v: want protected DACL for SYSTEM+Administrators, got %s", private, sddl)
		}
		if hasUsers := strings.Contains(sddl, ";BU)"); hasUsers == private {
			t.Errorf("private=%v: Users access = %v in %s", private, hasUsers, sddl)
		}
		if strings.Contains(sddl, "FA;;;BU)") || strings.Contains(sddl, "WD") {
			t.Errorf("Users must not be able to write: %s", sddl)
		}
	}
}
