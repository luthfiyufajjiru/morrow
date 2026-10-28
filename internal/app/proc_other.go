//go:build !linux && !windows

package app

import (
	"os/exec"
	"time"
)

// startedBy cannot tell a process's start time here, so it never claims a
// process as the app's: cleanup leaves a live PID alone.
func startedBy(int, time.Time) bool { return false }

// sinceBoot cannot tell the boot time here, so it never claims t is after it.
func sinceBoot(time.Time) bool { return false }

// setDeathSignal has no equivalent here; a dead relay's app is stopped by the
// next command that sees it.
func setDeathSignal(*exec.Cmd) {}
