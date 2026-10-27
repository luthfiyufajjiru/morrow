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
