//go:build !linux && !windows

package inbox

import "errors"

// InstallHook is not available on this OS.
func InstallHook(_, _ string) (string, error) {
	return "", errors.New("no automatic hook on this OS; run 'sudo morrow apply' yourself, " +
		"or from a launchd job with WatchPaths set to the inbox directory")
}
