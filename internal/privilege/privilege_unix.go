//go:build !windows

package privilege

import "os"

// IsElevated reports whether the process runs as root.
func IsElevated() bool {
	return os.Geteuid() == 0
}

// RestrictKeyFile makes the key readable and writable by its owner only. The
// owner is whoever ran init: root for a system install, the user otherwise.
func RestrictKeyFile(path string, _ bool) error {
	return os.Chmod(path, 0600)
}

// PrepareSystemDir creates a system install directory owned by whoever runs
// init (root). Shared directories are 0755 so regular users can still run
// read-only commands; private ones (holding the key) are 0700.
func PrepareSystemDir(path string, private bool) error {
	mode := os.FileMode(0755)
	if private {
		mode = 0700
	}
	if err := os.MkdirAll(path, mode); err != nil {
		return err
	}
	// MkdirAll is subject to the umask and skips existing directories.
	return os.Chmod(path, mode)
}

// MigrateHint tells how to run an elevated `morrow init` on this platform.
const MigrateHint = "run 'sudo morrow init'"
