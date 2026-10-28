//go:build linux

package app

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// clockTicks is USER_HZ, the unit of /proc/<pid>/stat start times; it is 100
// on every architecture Linux supports.
const clockTicks = 100

// startedBy reports whether process pid started no later than t.
func startedBy(pid int, t time.Time) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	// Fields after the command name, which is in parentheses and may contain
	// spaces: state is field 3, starttime field 22.
	s := string(stat)
	fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
	if len(fields) < 20 {
		return false
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return false
	}
	boot, ok := bootTime()
	if !ok {
		return false
	}
	return !boot.Add(time.Duration(ticks) * time.Second / clockTicks).After(t)
}

// sinceBoot reports whether t is after the system booted.
func sinceBoot(t time.Time) bool {
	boot, ok := bootTime()
	return ok && t.After(boot)
}

func bootTime() (time.Time, bool) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			if sec, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return time.Unix(sec, 0), true
			}
		}
	}
	return time.Time{}, false
}

// setDeathSignal makes the kernel send the app SIGTERM when its relay dies,
// so a killed relay does not leave the app running unsupervised. The signal
// is tied to the thread that started the app; the relay locks its main
// goroutine to one thread for its lifetime.
func setDeathSignal(cmd *exec.Cmd) {
	cmd.SysProcAttr.Pdeathsig = syscall.SIGTERM
}
