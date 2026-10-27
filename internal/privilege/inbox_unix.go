//go:build !windows

package privilege

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"runtime"
	"strconv"
)

// DeployGroup is the group whose members may drop submissions into the inbox.
const DeployGroup = "morrow-deploy"

// PrepareInboxDir creates the inbox as root:morrow-deploy with mode 1730:
// group members can create files but cannot list the directory, read other
// files or (sticky bit) delete or rename files they do not own.
func PrepareInboxDir(path string) error {
	gid, err := ensureGroup(DeployGroup)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	if err := os.Chown(path, 0, gid); err != nil {
		return err
	}
	return os.Chmod(path, os.ModeSticky|0730)
}

func ensureGroup(name string) (int, error) {
	g, err := user.LookupGroup(name)
	var unknown user.UnknownGroupError
	if errors.As(err, &unknown) {
		var cmd *exec.Cmd
		if runtime.GOOS == "darwin" {
			cmd = exec.Command("dseditgroup", "-o", "create", name)
		} else {
			cmd = exec.Command("groupadd", "--system", name)
		}
		if out, cerr := cmd.CombinedOutput(); cerr != nil {
			return 0, fmt.Errorf("failed to create group %s (%v): %s; create it yourself and re-run init", name, cerr, out)
		}
		g, err = user.LookupGroup(name)
	}
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(g.Gid)
}

// DeployGroupExists reports whether the deploy group exists.
func DeployGroupExists() bool {
	_, err := user.LookupGroup(DeployGroup)
	return err == nil
}

// RemoveDeployGroup deletes the deploy group. A missing group is not an error.
func RemoveDeployGroup() error {
	if !DeployGroupExists() {
		return nil
	}
	cmd := exec.Command("groupdel", DeployGroup)
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("dseditgroup", "-o", "delete", DeployGroup)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete group %s (%v): %s", DeployGroup, err, out)
	}
	return nil
}
