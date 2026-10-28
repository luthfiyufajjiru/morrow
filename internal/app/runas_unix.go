//go:build !windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// checkRunAsAccount validates a run-as account: it must exist, and no
// password is used on Unix.
func checkRunAsAccount(account, password string) error {
	if password != "" {
		return fmt.Errorf("a password is only used on Windows; on this system root starts the app as %s directly", account)
	}
	if _, err := user.Lookup(account); err != nil {
		return fmt.Errorf("unknown account %q: %w", account, err)
	}
	return nil
}

// startChild starts the app in its own session, as account if one is set.
// Switching account needs the supervisor to run as root.
func startChild(cmd *exec.Cmd, _ []string, account, _ string) (child, error) {
	detachProcess(cmd)
	setDeathSignal(cmd)
	if account != "" {
		u, err := user.Lookup(account)
		if err != nil {
			return nil, fmt.Errorf("unknown account %q: %w", account, err)
		}
		uid, _ := strconv.ParseUint(u.Uid, 10, 32)
		gid, _ := strconv.ParseUint(u.Gid, 10, 32)
		if os.Geteuid() != 0 && int(uid) != os.Geteuid() {
			return nil, fmt.Errorf("running the app as %s requires root", account)
		}
		if int(uid) == os.Geteuid() {
			// Already that account: nothing to switch (and setgroups would
			// fail without root).
			cmd.Env = append(cmd.Env, "HOME="+u.HomeDir, "USER="+u.Username, "LOGNAME="+u.Username)
			if err := cmd.Start(); err != nil {
				return nil, err
			}
			return execChild{cmd}, nil
		}
		var groups []uint32
		if ids, err := u.GroupIds(); err == nil {
			for _, id := range ids {
				if g, err := strconv.ParseUint(id, 10, 32); err == nil {
					groups = append(groups, uint32(g))
				}
			}
		}
		cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: groups}
		// Later entries win: the app sees its own account, not root's.
		cmd.Env = append(cmd.Env, "HOME="+u.HomeDir, "USER="+u.Username, "LOGNAME="+u.Username)
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return execChild{cmd}, nil
}
