// Package privilege holds the OS-specific checks that decide who may set up
// Morrow and who may read its master key.
package privilege

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ElevationHint is the platform-appropriate instruction for running elevated.
const ElevationHint = "run it with sudo (Linux/macOS) or from an Administrator terminal (Windows)"

// CheckInit decides whether `morrow init` may run. A system install (data next
// to the binary) must be initialized elevated so its files end up owned by
// root/Administrators; a per-user install (MORROW_HOME set) needs no elevation.
func CheckInit(perUser bool, elevated bool, dataDir string) error {
	if perUser || elevated {
		return nil
	}
	return fmt.Errorf("initializing the system install in %s requires elevation: %s.\n"+
		"For a per-user install instead, set MORROW_HOME to a directory you own", dataDir, ElevationHint)
}

// Hint adds an actionable message to errors caused by missing write or read
// access to Morrow's files, and returns other errors unchanged.
func Hint(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if errors.Is(err, os.ErrPermission) ||
		strings.Contains(msg, "readonly database") ||
		strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "Access is denied") {
		return fmt.Errorf("%w\nthis needs more access to Morrow's files than you have: %s", err, ElevationHint)
	}
	return err
}
