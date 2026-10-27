package app

import (
	"database/sql"
	"fmt"
	"morrow/internal/db"
	"time"
)

// Restart policies, applied when an app exits without being stopped.
const (
	PolicyNo            = "no"             // record it as exited (default)
	PolicyUnlessStopped = "unless-stopped" // restart it, with backoff, up to the max
)

const (
	// stableAfter: an app that ran at least this long before exiting starts
	// counting restarts from zero again.
	stableAfter = 60 * time.Second
	maxBackoff  = 60 * time.Second
	// MaxRestartsLimit bounds --max-restarts.
	MaxRestartsLimit = 1000
)

// sleep and restartFn are replaced in tests.
var (
	sleep     = time.Sleep
	restartFn = Restart
)

// backoff is 1s, 2s, 4s, ... capped at one minute, for the nth restart.
func backoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	d := time.Second
	for i := 1; i < n && d < maxBackoff; i++ {
		d *= 2
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

// SetRestartPolicy sets what happens when the app exits without being stopped.
func SetRestartPolicy(name, policy string, maxRestarts int) error {
	if policy != PolicyNo && policy != PolicyUnlessStopped {
		return fmt.Errorf("unknown restart policy %q (use %q or %q)", policy, PolicyNo, PolicyUnlessStopped)
	}
	if maxRestarts < 1 || maxRestarts > MaxRestartsLimit {
		return fmt.Errorf("max restarts must be between 1 and %d", MaxRestartsLimit)
	}
	res, err := db.DB.Exec(`
		UPDATE applications SET application_restart_policy = ?, application_max_restarts = ?, application_update_time = ?
		WHERE application_name = ?
	`, policy, maxRestarts, time.Now().UTC().Format("2006-01-02T15:04:05Z"), name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("application %s not found", name)
	}
	return nil
}

// HandleExit is called by the log relay when the app it watches (pid) has
// closed its output, i.e. exited. A stop requested through morrow is recorded
// as stopped; anything else is an unexpected exit, handled by the app's
// restart policy. logf receives lines for the app's log. Every update is
// conditional on pid, so a relay for an older instance changes nothing.
func HandleExit(name string, pid int, uptime time.Duration, logf func(string)) error {
	// StartApp records the PID right after starting the relay; an app that
	// exits immediately can get here first.
	var status, policy string
	var maxRestarts, count int
	found := false
	for i := 0; i < 30; i++ {
		err := db.DB.QueryRow(`
			SELECT application_status, application_restart_policy, application_max_restarts, application_restart_count
			FROM applications WHERE application_name = ? AND application_pid = ?
		`, name, pid).Scan(&status, &policy, &maxRestarts, &count)
		if err == nil {
			found = true
			break
		}
		if err != sql.ErrNoRows {
			return err
		}
		sleep(100 * time.Millisecond)
	}
	if !found {
		return nil // another instance took over, or the app was deleted
	}

	cond := fmt.Sprintf("application_pid = %d", pid)
	switch status {
	case "stopping":
		logf("[morrow] stopped")
		return setStatusPID0(name, "stopped", cond)
	case "running":
	default:
		return nil
	}

	if uptime >= stableAfter {
		count = 0
	}
	if policy != PolicyUnlessStopped || count >= maxRestarts {
		reason := "restart policy is 'no'"
		if policy == PolicyUnlessStopped {
			reason = fmt.Sprintf("gave up after %d restarts", count)
		}
		logf(fmt.Sprintf("[morrow] exited unexpectedly after %s; not restarting (%s)", uptime.Round(time.Second), reason))
		return setStatusPID0(name, "exited", cond)
	}

	count++
	if _, err := db.DB.Exec(`
		UPDATE applications SET application_status = 'restarting', application_pid = 0, application_restart_count = ?,
		       application_status_time = ?, application_update_time = ?
		WHERE application_name = ? AND application_pid = ?
	`, count, time.Now().UTC().Format("2006-01-02T15:04:05Z"), time.Now().UTC().Format("2006-01-02T15:04:05Z"), name, pid); err != nil {
		return err
	}
	wait := backoff(count)
	logf(fmt.Sprintf("[morrow] exited unexpectedly after %s; restarting in %s (attempt %d/%d)",
		uptime.Round(time.Second), wait, count, maxRestarts))
	sleep(wait)

	// The user may have stopped, started or deleted the app meanwhile.
	if err := db.DB.QueryRow("SELECT application_status FROM applications WHERE application_name = ?", name).Scan(&status); err != nil || status != "restarting" {
		logf("[morrow] restart cancelled")
		return nil
	}
	newPID, err := restartFn(name)
	if err != nil {
		logf(fmt.Sprintf("[morrow] restart failed: %v", err))
		return setStatus(name, "exited", "application_status = 'restarting'")
	}
	logf(fmt.Sprintf("[morrow] restarted with PID %d", newPID))
	return nil
}
