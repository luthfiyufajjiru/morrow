//go:build !linux && !windows

// Package startup installs the OS hook that starts wanted apps at boot.
package startup

import "errors"

// Installed is always false on this OS.
func Installed() (string, bool) { return "", false }

// Install is not available on this OS.
func Install(string) (string, error) {
	return "", errors.New("no automatic startup on this OS; run 'sudo morrow _boot' at boot, " +
		"e.g. from a launchd daemon with RunAtLoad")
}

// Remove has nothing to remove on this OS.
func Remove() error { return nil }

// ShuttingDown is not detectable here.
func ShuttingDown() bool { return false }
