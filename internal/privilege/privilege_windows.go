//go:build windows

package privilege

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// IsElevated reports whether the process runs with an elevated (Administrator) token.
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// RestrictKeyFile replaces the key file's inherited ACL (which grants
// BUILTIN\Users read access under Program Files) with an explicit, protected
// one: SYSTEM and Administrators for a system install, or only the current
// user for a per-user install. Unix permission bits have no effect on Windows.
func RestrictKeyFile(path string, perUser bool) error {
	sddl := "D:P(A;;FA;;;SY)(A;;FA;;;BA)"
	if perUser {
		user, err := windows.GetCurrentProcessToken().GetTokenUser()
		if err != nil {
			return fmt.Errorf("failed to look up current user: %w", err)
		}
		sddl = fmt.Sprintf("D:P(A;;FA;;;%s)", user.User.Sid.String())
	}
	return setDACL(path, sddl)
}

// setDACL replaces the DACL of a file or directory with the protected one in sddl.
func setDACL(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("failed to build ACL: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("failed to build ACL: %w", err)
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("failed to restrict access to %s: %w", path, err)
	}
	return nil
}

// PrepareSystemDir creates a system install directory with an explicit,
// protected ACL inherited by everything init puts in it: full control for
// SYSTEM and Administrators, and read/execute for Users unless private. This
// drops the create-files right Users inherit from %ProgramData%. The key file
// inside is locked down further by RestrictKeyFile.
func PrepareSystemDir(path string, private bool) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	sddl := "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	if !private {
		sddl += "(A;OICI;0x1200a9;;;BU)"
	}
	return setDACL(path, sddl)
}

// MigrateHint tells how to run an elevated `morrow init` on this platform.
const MigrateHint = "run 'morrow init' from an Administrator terminal"
