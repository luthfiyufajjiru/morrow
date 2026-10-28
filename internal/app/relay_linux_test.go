package app

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// orphan starts a stand-in for an app left behind by a dead relay: a shell
// that is its own session leader with a child in its process group.
func orphan(t *testing.T) (leader *exec.Cmd, childAlive func() bool) {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "sleep 300 & echo $! ; wait")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var childPID int
	buf := make([]byte, 32)
	n, _ := out.Read(buf)
	for _, c := range buf[:n] {
		if c >= '0' && c <= '9' {
			childPID = childPID*10 + int(c-'0')
		}
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })
	go func() { _ = cmd.Wait() }()
	return cmd, func() bool { return syscall.Kill(childPID, 0) == nil }
}

func TestDeadRelayStopsTheOrphanedApp(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	release, _ := acquireRelay("api")
	release()
	leader, childAlive := orphan(t)
	setRunning(t, "api", leader.Process.Pid, time.Now().Add(-time.Second))

	if st, _, _ := GetAppStatus("api"); st != "exited" {
		t.Errorf("status = %s, want exited", st)
	}
	if !waitExit(leader.Process.Pid, 2*time.Second) {
		t.Error("orphaned app still running")
	}
	time.Sleep(200 * time.Millisecond)
	if childAlive() {
		t.Error("the app's child process still running")
	}
}

// After a reboot the recorded PID may belong to an unrelated process.
func TestDeadRelayLeavesReusedPIDAlone(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	release, _ := acquireRelay("api")
	release()
	other, _ := orphan(t)
	setRunning(t, "api", other.Process.Pid, time.Now().Add(-time.Hour)) // the app started long before this process

	if st, _, _ := GetAppStatus("api"); st != "exited" {
		t.Errorf("status = %s, want exited", st)
	}
	time.Sleep(300 * time.Millisecond)
	if !isProcessAlive(other.Process.Pid) {
		t.Error("killed an unrelated process that reused the app's PID")
	}
}

func TestStartedBy(t *testing.T) {
	p, _ := orphan(t)
	if !startedBy(p.Process.Pid, time.Now().Add(2*time.Second)) {
		t.Error("process not seen as started before now")
	}
	if startedBy(p.Process.Pid, time.Now().Add(-time.Hour)) {
		t.Error("process seen as started an hour ago")
	}
}

func TestSinceBoot(t *testing.T) {
	if !sinceBoot(time.Now()) || sinceBoot(time.Unix(0, 0)) {
		t.Error("sinceBoot is wrong about now or 1970")
	}
}
