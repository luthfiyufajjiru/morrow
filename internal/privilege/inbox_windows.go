//go:build windows

package privilege

import (
	"fmt"
	"os"
	"os/exec"

	"golang.org/x/sys/windows"
)

// DeployGroup is the local group whose members may drop submissions into the inbox.
const DeployGroup = "Morrow Deployers"

// InboxSDDL is the protected DACL of the inbox: SYSTEM and Administrators have
// full control; the deploy group may only create files (FILE_ADD_FILE |
// SYNCHRONIZE), with no right to list the directory or open other files; the
// creator of a file gets write access to it (CREATOR OWNER, files only).
func InboxSDDL(groupSID string) string {
	return fmt.Sprintf("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;;0x100002;;;%s)(A;OIIO;0x120116;;;CO)", groupSID)
}

// PrepareInboxDir creates the inbox and applies InboxSDDL, creating the
// deploy group if needed.
func PrepareInboxDir(path string) error {
	sid, err := ensureGroup(DeployGroup)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	return setDACL(path, InboxSDDL(sid.String()))
}

func ensureGroup(name string) (*windows.SID, error) {
	sid, _, _, err := windows.LookupSID("", name)
	if err == nil {
		return sid, nil
	}
	out, cerr := exec.Command("net", "localgroup", name, "/add",
		"/comment:Members may submit write-only secrets to Morrow").CombinedOutput()
	if cerr != nil {
		return nil, fmt.Errorf("failed to create local group %q (%v): %s", name, cerr, out)
	}
	sid, _, _, err = windows.LookupSID("", name)
	return sid, err
}

// DeployGroupExists reports whether the deploy group exists.
func DeployGroupExists() bool {
	_, _, _, err := windows.LookupSID("", DeployGroup)
	return err == nil
}

// RemoveDeployGroup deletes the deploy group. A missing group is not an error.
func RemoveDeployGroup() error {
	if !DeployGroupExists() {
		return nil
	}
	if out, err := exec.Command("net", "localgroup", DeployGroup, "/delete").CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete local group %q (%v): %s", DeployGroup, err, out)
	}
	return nil
}
