//go:build windows

package privilege

import (
	"fmt"

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

	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("failed to build key file ACL: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("failed to build key file ACL: %w", err)
	}
	err = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
	if err != nil {
		return fmt.Errorf("failed to restrict access to %s: %w", path, err)
	}
	return nil
}
