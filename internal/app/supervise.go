package app

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"morrow/internal/config"
	"morrow/internal/db"
	"morrow/internal/env"
	"morrow/internal/hooks"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Restart policies, applied when an app exits without being stopped.
const (
	PolicyNo            = "no"             // record it as exited (default)
	PolicyOnFailure     = "on-failure"     // restart after a non-zero exit code or a signal
	PolicyUnlessStopped = "unless-stopped" // restart after any exit
)

const (
	// stableAfter: an app that ran at least this long before exiting starts
	// counting restarts from zero again.
	stableAfter = 60 * time.Second
	maxBackoff  = 60 * time.Second
	// MaxRestartsLimit bounds --max-restarts.
	MaxRestartsLimit = 1000
	// reportTimeout bounds how long `start` waits for the relay's report.
	reportTimeout = 30 * time.Second
	// drainTimeout bounds how long the relay keeps copying output after the
	// app exited (its own children may still hold the pipe open).
	drainTimeout = 2 * time.Second
	// hookWait bounds how long the relay waits for alert hooks before exiting.
	hookWait = 45 * time.Second
)

// sleep and fireHooks are replaced in tests.
var (
	sleep     = time.Sleep
	fireHooks = hooks.Fire
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

// ValidPolicy reports whether p is a known restart policy.
func ValidPolicy(p string) bool {
	return p == PolicyNo || p == PolicyOnFailure || p == PolicyUnlessStopped
}

// SetRestartPolicy sets what happens when the app exits without being stopped.
func SetRestartPolicy(name, policy string, maxRestarts int) error {
	if !ValidPolicy(policy) {
		return fmt.Errorf("unknown restart policy %q (use %s, %s or %s)", policy, PolicyNo, PolicyOnFailure, PolicyUnlessStopped)
	}
	if maxRestarts < 1 || maxRestarts > MaxRestartsLimit {
		return fmt.Errorf("max restarts must be between 1 and %d", MaxRestartsLimit)
	}
	res, err := db.DB.Exec(`
		UPDATE applications SET application_restart_policy = ?, application_max_restarts = ?, application_update_time = ?
		WHERE application_name = ?
	`, policy, maxRestarts, now(), name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("application %s not found", name)
	}
	return nil
}

func now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

// StartRequest is what `start` hands to the supervising relay on its stdin.
type StartRequest struct {
	InlineEnvs map[string]string `json:"inline_envs,omitempty"`
	// UserStart resets the restart counter and marks the app as wanted.
	UserStart bool `json:"user_start"`
}

// launchSupervisor starts `morrow _relay <name>`, which launches the app as its
// own child, and waits for its one-line report: "started <pid>" or "error <msg>".
func launchSupervisor(name string, req StartRequest) (int, error) {
	selfExe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("failed to locate morrow executable: %w", err)
	}
	cmd := exec.Command(selfExe, "_relay", name)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	detachRelayProcess(cmd)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start the supervising relay: %w", err)
	}
	relayPID := cmd.Process.Pid
	_ = os.WriteFile(config.GetRelayPIDFilePath(name), []byte(strconv.Itoa(relayPID)), 0644)

	_ = json.NewEncoder(stdin).Encode(req)
	stdin.Close()

	line := make(chan string, 1)
	go func() {
		l, _ := bufio.NewReader(stdout).ReadString('\n')
		line <- strings.TrimSpace(l)
	}()
	var report string
	select {
	case report = <-line:
	case <-time.After(reportTimeout):
		_ = forceKill(relayPID)
		return 0, fmt.Errorf("the supervising relay did not report within %s", reportTimeout)
	}
	stdout.Close()
	_ = cmd.Process.Release() // the relay outlives this command

	if pidText, ok := strings.CutPrefix(report, "started "); ok {
		return strconv.Atoi(pidText)
	}
	_ = os.Remove(config.GetRelayPIDFilePath(name))
	if msg, ok := strings.CutPrefix(report, "error "); ok {
		return 0, fmt.Errorf("failed to start process: %s", msg)
	}
	return 0, fmt.Errorf("the supervising relay exited without starting the app (report %q)", report)
}

// buildCommand prepares the app's process: executable, arguments, working
// directory and environment (stored variables, then one-off ones).
func buildCommand(a *Application, envs []env.EnvVar, inline map[string]string) *exec.Cmd {
	cmd := exec.Command(a.ExecutablePath, a.Arguments...)
	cmd.Dir = a.WorkingDir
	cmd.Env = append(os.Environ(), "MORROW_APP_ID="+a.ID, "MORROW_APP_NAME="+a.Name)
	for _, e := range envs {
		cmd.Env = append(cmd.Env, e.Name+"="+e.Value)
	}
	for k, v := range inline {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	return cmd
}

// ExitInfo describes how a process ended.
type ExitInfo struct {
	Code   int    // exit code; -1 when killed by a signal
	Signal string // e.g. "SIGKILL"; empty unless killed by a signal
}

// Failed reports a non-zero exit code or death by signal.
func (e ExitInfo) Failed() bool { return e.Code != 0 || e.Signal != "" }

func (e ExitInfo) String() string {
	if e.Signal != "" {
		return "killed by " + e.Signal
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

func exitInfo(ps *os.ProcessState) ExitInfo {
	if ps == nil {
		return ExitInfo{Code: -1}
	}
	return ExitInfo{Code: ps.ExitCode(), Signal: exitSignal(ps)}
}

// decision is what the supervisor does after an exit.
type decision struct {
	restart bool
	wait    time.Duration
	events  []string // hook events to fire
	count   int      // restart count after this exit
}

// decide records the exit of instance pid and applies the restart policy.
// Every update is conditional on pid, so a relay of an older instance changes
// nothing.
func decide(name string, pid int, uptime time.Duration, exit ExitInfo, logf func(string)) (decision, error) {
	var status, policy string
	var maxRestarts, count int
	err := db.DB.QueryRow(`
		SELECT application_status, application_restart_policy, application_max_restarts, application_restart_count
		FROM applications WHERE application_name = ? AND application_pid = ?
	`, name, pid).Scan(&status, &policy, &maxRestarts, &count)
	if err == sql.ErrNoRows {
		return decision{}, nil // another instance took over, or the app was deleted
	}
	if err != nil {
		return decision{}, err
	}
	if _, err := db.DB.Exec(`
		UPDATE applications SET application_last_exit_code = ?, application_last_exit_signal = ?, application_last_exit_time = ?
		WHERE application_name = ? AND application_pid = ?
	`, exit.Code, exit.Signal, now(), name, pid); err != nil {
		return decision{}, err
	}

	cond := fmt.Sprintf("application_pid = %d", pid)
	if status == "stopping" {
		logf(fmt.Sprintf("[morrow] stopped (%s)", exit))
		return decision{}, setStatusPID0(name, "stopped", cond)
	}
	if status != "running" {
		return decision{}, nil
	}

	event := hooks.EventExit
	if exit.Failed() {
		event = hooks.EventCrash
	}
	if uptime >= stableAfter {
		count = 0
	}
	ran := uptime.Round(time.Second)
	wants := policy == PolicyUnlessStopped || (policy == PolicyOnFailure && exit.Failed())
	if !wants {
		logf(fmt.Sprintf("[morrow] exited unexpectedly after %s (%s); not restarting (restart policy is '%s')", ran, exit, policy))
		return decision{events: []string{event}, count: count}, setStatusPID0(name, "exited", cond)
	}
	if count >= maxRestarts {
		logf(fmt.Sprintf("[morrow] exited unexpectedly after %s (%s); gave up after %d restarts", ran, exit, count))
		return decision{events: []string{event, hooks.EventGiveUp}, count: count}, setStatusPID0(name, "exited", cond)
	}
	count++
	if _, err := db.DB.Exec(`
		UPDATE applications SET application_status = 'restarting', application_pid = 0, application_restart_count = ?,
		       application_status_time = ?, application_update_time = ?
		WHERE application_name = ? AND application_pid = ?
	`, count, now(), now(), name, pid); err != nil {
		return decision{}, err
	}
	wait := backoff(count)
	logf(fmt.Sprintf("[morrow] exited unexpectedly after %s (%s); restarting in %s (attempt %d/%d)",
		ran, exit, wait, count, maxRestarts))
	return decision{restart: true, wait: wait, events: []string{event}, count: count}, nil
}

// recordRunning marks instance pid as running. A user start also resets the
// restart counter and marks the app as wanted.
func recordRunning(name string, pid int, userStart bool) error {
	extra := ""
	if userStart {
		extra = ", application_restart_count = 0, application_desired_state = 'up'"
	}
	t := now()
	_, err := db.DB.Exec(`
		UPDATE applications
		SET application_status = 'running', application_pid = ?, application_status_time = ?,
		    application_last_run_time = ?, application_update_time = ?`+extra+`
		WHERE application_name = ?
	`, pid, t, t, t, name)
	return err
}

// Supervise runs in the relay process: it starts the app as its child,
// copies its output to logw, waits for it to exit, records how it exited and
// restarts it according to the restart policy, firing alert hooks along the
// way. The first start is reported on report ("started <pid>" or "error ...").
func Supervise(name string, req StartRequest, report io.WriteCloser, logw io.Writer) error {
	logf := func(line string) { _, _ = fmt.Fprintf(logw, "%s %s\n", now(), line) }
	reported := false
	reportf := func(s string) {
		if !reported {
			reported = true
			_, _ = fmt.Fprintln(report, s)
			_ = report.Close()
		}
	}
	defer reportf("error the supervising relay stopped before starting the app")

	var hooksRunning sync.WaitGroup
	defer func() {
		done := make(chan struct{})
		go func() { hooksRunning.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(hookWait):
			logf("[morrow] alert hooks still running; not waiting for them")
		}
	}()

	first := true
	for {
		a, err := GetAppDetail(name)
		if err != nil {
			reportf("error " + err.Error())
			return err
		}
		envs, err := env.ListEnv(name)
		if err == nil {
			for _, e := range envs {
				if e.Locked {
					err = fmt.Errorf("cannot decrypt secured variable %s: no read access to the master key", e.Name)
				}
			}
		}
		var cmd *exec.Cmd
		var pr, pw *os.File
		if err == nil {
			cmd = buildCommand(a, envs, req.InlineEnvs)
			pr, pw, err = os.Pipe()
		}
		if err == nil {
			cmd.Stdout, cmd.Stderr = pw, pw
			detachProcess(cmd)
			err = cmd.Start()
			pw.Close()
			if err != nil {
				pr.Close()
			}
		}
		if err != nil {
			if first {
				reportf("error " + err.Error())
				return err
			}
			logf(fmt.Sprintf("[morrow] restart failed: %v", err))
			_ = setStatus(name, "exited", "application_status = 'restarting'")
			return err
		}

		copied := make(chan struct{})
		go func() { _, _ = io.Copy(logw, pr); pr.Close(); close(copied) }()
		pid := cmd.Process.Pid
		if err := recordRunning(name, pid, first && req.UserStart); err != nil {
			_ = forceKill(pid)
			reportf("error " + err.Error())
			return err
		}
		if first {
			reportf(fmt.Sprintf("started %d", pid))
			first = false
		} else {
			logf(fmt.Sprintf("[morrow] restarted with PID %d", pid))
		}

		started := time.Now()
		_ = cmd.Wait()
		exit := exitInfo(cmd.ProcessState)
		select {
		case <-copied:
		case <-time.After(drainTimeout):
		}

		d, err := decide(name, pid, time.Since(started), exit, logf)
		if err != nil {
			logf(fmt.Sprintf("[morrow] failed to record the exit: %v", err))
			return err
		}
		// Hooks run in the background (a restart must not wait for them), but
		// the events of one exit run in order: crash, then giveup.
		if len(d.events) > 0 {
			hooksRunning.Add(1)
			go func(events []string, exit ExitInfo, count int) {
				defer hooksRunning.Done()
				for _, ev := range events {
					fireHooks(hooks.Event{Name: ev, App: name, AppID: a.ID, ExitCode: exit.Code, Signal: exit.Signal,
						RestartCount: count, Time: time.Now().UTC()}, logf)
				}
			}(d.events, exit, d.count)
		}
		if !d.restart {
			return nil
		}
		sleep(d.wait)
		var status string
		if err := db.DB.QueryRow("SELECT application_status FROM applications WHERE application_name = ?", name).Scan(&status); err != nil || status != "restarting" {
			logf("[morrow] restart cancelled")
			return nil
		}
	}
}
