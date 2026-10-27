//go:build windows

package inbox

import (
	"fmt"
	"morrow/internal/grant"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// fileOwner returns who created the file, from its owner SID. A regular user
// cannot set another account as a file's owner. Files owned by SYSTEM or
// Administrators (the default owner of files an elevated admin creates) are
// privileged.
func fileOwner(path string, _ os.FileInfo) (submitter, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return submitter{}, fmt.Errorf("cannot read file owner: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return submitter{}, fmt.Errorf("cannot read file owner: %v", err)
	}
	if owner.IsWellKnown(windows.WinLocalSystemSid) || owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return submitter{name: owner.String(), privileged: true}, nil
	}
	account, domain, _, err := owner.LookupAccount("")
	if err != nil {
		return submitter{name: owner.String()}, nil
	}
	return submitter{name: grant.NormalizePrincipal(domain + `\` + account)}, nil
}

// openNoFollow opens path for reading without following a reparse point
// (symlink or junction) swapped in after the Lstat check.
func openNoFollow(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("reparse points are rejected")
	}
	if info.NumberOfLinks != 1 {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("file has %d hard links; only plain files are accepted", info.NumberOfLinks)
	}
	return os.NewFile(uintptr(h), path), nil
}
