package app

import (
	"database/sql"
	"errors"
	"fmt"
	"morrow/internal/config"
	"morrow/internal/db"
	"os"
	"strconv"
	"strings"
	"time"
)

// errLocked means another process holds the lock.
var errLocked = errors.New("locked")

const (
	// relayLockWait bounds how long a new relay waits for the app's lock,
	// which a status check holds for an instant and a finishing relay
	// releases right after recording the exit.
	relayLockWait = 2 * time.Second
	// orphanStopTimeout bounds how long cleanup waits for the processes of an
	// app whose relay died to exit after SIGTERM before killing them.
	orphanStopTimeout = 5 * time.Second
)

// acquireRelay takes the app's relay lock, so at most one relay supervises an
// app, and records the relay's PID. The OS releases the lock when the relay
// dies, however it dies, which is how other commands tell a live relay from
// a dead one without trusting a PID.
func acquireRelay(name string) (release func(), err error) {
	deadline := time.Now().Add(relayLockWait)
	for {
		f, err := lockFile(config.GetRelayLockPath(name))
		if err == nil {
			pidFile := config.GetRelayPIDFilePath(name)
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
			return func() { removeOwnPIDFile(pidFile); f.Close() }, nil
		}
		if err != errLocked {
			return nil, fmt.Errorf("cannot lock %s: %w", config.GetRelayLockPath(name), err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("application %s is already supervised by another relay (PID %d)", name, relayPID(name))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// removeOwnPIDFile deletes the relay PID file only if it still holds this
// process's PID.
func removeOwnPIDFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid == os.Getpid() {
		_ = os.Remove(path)
	}
}

type relayStatus int

const (
	relayUnknown relayStatus = iota // no access to the lock, or a relay older than the lock
	relayAlive
	relayGone
)

func relayState(name string) relayStatus {
	held, known := probeLock(config.GetRelayLockPath(name))
	switch {
	case !known:
		return relayUnknown
	case held:
		return relayAlive
	}
	return relayGone
}

// relayPID is the PID the current relay recorded, or 0.
func relayPID(name string) int {
	data, err := os.ReadFile(config.GetRelayPIDFilePath(name))
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

// endRelay kills the app's relay if one is alive (e.g. waiting to restart a
// crashed app) and waits until its lock is free.
func endRelay(name string, timeout time.Duration) bool {
	if relayState(name) != relayAlive {
		return true
	}
	if pid := relayPID(name); pid > 0 {
		_ = forceKill(pid)
	}
	deadline := time.Now().Add(timeout)
	for relayState(name) == relayAlive {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}

// reconcileStatus corrects the recorded status of an app that is running,
// stopping or restarting. While its relay lives, the relay records what
// happens, so nothing is changed. If the relay died (killed, OOM), nobody
// will: see orphaned. If the relay's state cannot be told (no access, or an
// app started before relays took a lock), a running app whose PID no longer
// exists is treated as stopped. Writes are best effort: callers without
// write access still get the corrected view.
func reconcileStatus(name string, status string, pid int) (string, int) {
	if status != "running" && status != "stopping" && status != "restarting" {
		return status, pid
	}
	switch relayState(name) {
	case relayAlive:
		return status, pid
	case relayGone:
		return orphaned(name, status, pid)
	}
	if status == "restarting" || isProcessAlive(pid) {
		return status, pid
	}
	_, _ = db.DB.Exec(`
		UPDATE applications
		SET application_status = 'stopped', application_pid = 0,
		    application_status_time = ?, application_update_time = ?
		WHERE application_name = ? AND application_status = ? AND application_pid = ?
	`, now(), now(), name, status, pid)
	return "stopped", 0
}

// orphaned handles an app whose relay died: its exit would never be recorded
// and it would never be restarted. It is recorded as exited (stopped, if a
// stop was under way) and whatever is left of its processes is stopped, so
// no unsupervised copy runs beside the next start. It stays wanted, so the
// next boot starts it again.
func orphaned(name, status string, pid int) (string, int) {
	newStatus := "exited"
	if status == "stopping" {
		newStatus = "stopped"
	}
	var lastRun sql.NullString
	_ = db.DB.QueryRow("SELECT application_last_run_time FROM applications WHERE application_name = ?", name).Scan(&lastRun)

	// Only the command that records the change cleans up; a caller without
	// write access changes nothing.
	res, err := db.DB.Exec(`
		UPDATE applications
		SET application_status = ?, application_pid = 0, application_status_time = ?, application_update_time = ?
		WHERE application_name = ? AND application_status = ? AND application_pid = ?
	`, newStatus, now(), now(), name, status, pid)
	if err != nil {
		return newStatus, 0
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return newStatus, 0
	}

	note := "[morrow] the supervising relay died"
	if pid > 0 && lastRun.Valid {
		if started, err := time.Parse("2006-01-02T15:04:05Z", lastRun.String); err == nil && stopOrphan(pid, started) {
			note += fmt.Sprintf("; stopped what was left of the app (PID %d)", pid)
		}
	}
	appendAppLog(name, note+"; run 'morrow start "+name+"' to start it again")
	return newStatus, 0
}

// appendAppLog adds a line to the app's log, if it has one.
func appendAppLog(name, line string) {
	f, err := os.OpenFile(config.GetLogFilePath(name), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s\n", now(), line)
}
