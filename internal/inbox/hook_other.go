//go:build !linux && !windows

package inbox

import "errors"

// InstallHook is not available on this OS.
func InstallHook(_, _ string) (string, error) {
	return "", errors.New("no automatic hook on this OS; run 'sudo morrow apply' yourself, " +
		"or from a launchd job with WatchPaths set to the inbox directory")
}

// HookInstalled is always false on this OS: InstallHook installs nothing.
func HookInstalled() (string, bool) { return "", false }

// RemoveHook has nothing to remove on this OS.
func RemoveHook() error { return nil }
