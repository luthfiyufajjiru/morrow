package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"morrow/internal/config"
	"morrow/internal/db"
	"morrow/internal/env"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Application struct {
	ID                   string            `json:"application_id"`
	Name                 string            `json:"application_name"`
	ExecutablePath       string            `json:"application_executable_path"`
	Arguments            []string          `json:"application_arguments"`
	WorkingDir           string            `json:"application_working_directory,omitempty"`
	EnvironmentVariables map[string]string `json:"application_environment_variables"`
	Status               string            `json:"application_status"`
	PID                  int               `json:"application_pid"`
	CreationTime         time.Time         `json:"application_creation_time"`
	UpdateTime           time.Time         `json:"application_update_time"`
	StatusTime           *time.Time        `json:"application_status_time,omitempty"`
	LastRunTime          *time.Time        `json:"application_last_run_time,omitempty"`
	CommandLine          string            `json:"application_command_line"`
	RestartPolicy        string            `json:"application_restart_policy"`
	MaxRestarts          int               `json:"application_max_restarts"`
	RestartCount         int               `json:"application_restart_count"`
	DesiredState         string            `json:"application_desired_state"`
	LastExitCode         *int              `json:"application_last_exit_code,omitempty"`
	LastExitSignal       string            `json:"application_last_exit_signal,omitempty"`
	LastExitTime         *time.Time        `json:"application_last_exit_time,omitempty"`
	RunAs                string            `json:"application_run_as,omitempty"`
	HealthURL            string            `json:"application_health_url,omitempty"`
	HealthInterval       int               `json:"application_health_interval_seconds,omitempty"`
	HealthFailures       int               `json:"application_health_failures,omitempty"`
}

// GetAppDetail retrieves full information for a named application.
func GetAppDetail(name string) (*Application, error) {
	var app Application
	var argsJSON string
	var creationTimeStr, updateTimeStr, statusTimeStr string
	var lastRunTimeStr sql.NullString
	var lastExitCode sql.NullInt64
	var lastExitTimeStr sql.NullString

	err := db.DB.QueryRow(`
		SELECT application_id, application_name, application_executable_path, application_arguments,
		       application_working_directory, application_status, application_pid,
		       application_creation_time, application_update_time,
		       application_status_time, application_last_run_time,
		       application_restart_policy, application_max_restarts, application_restart_count,
		       application_desired_state, application_last_exit_code, application_last_exit_signal, application_last_exit_time, application_run_as,
		       application_health_url, application_health_interval, application_health_failures
		FROM applications WHERE application_name = ?
	`, name).Scan(
		&app.ID, &app.Name, &app.ExecutablePath, &argsJSON,
		&app.WorkingDir, &app.Status, &app.PID, &creationTimeStr, &updateTimeStr,
		&statusTimeStr, &lastRunTimeStr,
		&app.RestartPolicy, &app.MaxRestarts, &app.RestartCount,
		&app.DesiredState, &lastExitCode, &app.LastExitSignal, &lastExitTimeStr, &app.RunAs,
		&app.HealthURL, &app.HealthInterval, &app.HealthFailures,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("application %s not found", name)
		}
		return nil, err
	}
	app.Status, app.PID = reconcileStatus(name, app.Status, app.PID)

	// Parse arguments
	if err := json.Unmarshal([]byte(argsJSON), &app.Arguments); err != nil {
		return nil, fmt.Errorf("failed to parse arguments: %w", err)
	}

	// Parse timestamps
	parseTime := func(s string) (time.Time, error) {
		t, err := time.Parse("2006-01-02 15:04:05", s)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04:05Z", s)
		}
		if err != nil {
			t, err = time.Parse(time.RFC3339, s)
		}
		return t, err
	}

	app.CreationTime, _ = parseTime(creationTimeStr)
	app.UpdateTime, _ = parseTime(updateTimeStr)
	st, _ := parseTime(statusTimeStr)
	app.StatusTime = &st

	if lastExitCode.Valid {
		c := int(lastExitCode.Int64)
		app.LastExitCode = &c
	}
	if lastExitTimeStr.Valid {
		le, _ := parseTime(lastExitTimeStr.String)
		app.LastExitTime = &le
	}
	if lastRunTimeStr.Valid {
		lr, _ := parseTime(lastRunTimeStr.String)
		app.LastRunTime = &lr
	}

	// Fetch environment variables. Secured values are always masked here because
	// the detail view is display-only; StartApp resolves real values separately.
	envs, err := env.ListEnv(name)
	if err != nil {
		return nil, err
	}

	app.EnvironmentVariables = make(map[string]string)
	envNames := make([]string, 0, len(envs))
	for _, e := range envs {
		val := e.Value
		if e.IsSecured {
			val = maskedValue
		}
		app.EnvironmentVariables[e.Name] = val
		envNames = append(envNames, e.Name)
	}
	sort.Strings(envNames)

	// Generate the CommandLine string
	var cmdLine strings.Builder
	for _, k := range envNames {
		fmt.Fprintf(&cmdLine, "%s=%s ", k, app.EnvironmentVariables[k])
	}
	cmdLine.WriteString(app.ExecutablePath)
	if len(app.Arguments) > 0 {
		cmdLine.WriteString(" " + strings.Join(app.Arguments, " "))
	}
	app.CommandLine = cmdLine.String()

	return &app, nil
}

const maskedValue = "****"

// ErrNotRunning is returned by StopApp for an app that is not running.
var ErrNotRunning = errors.New("is not running")

// resolveWorkingDir converts a user-supplied working directory to an absolute
// path and checks that it is an existing directory. Empty means "inherit".
func resolveWorkingDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("invalid working directory %s: %w", dir, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("invalid working directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory %s is not a directory", dir)
	}
	return abs, nil
}

// CreateApp initializes a new managed application.
func CreateApp(name string, execPath string, args []string, workDir string) error {
	workDir, err := resolveWorkingDir(workDir)
	if err != nil {
		return err
	}

	var count int
	err = db.DB.QueryRow("SELECT count(*) FROM applications WHERE application_name = ?", name).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("application %s already exists", name)
	}

	appID := uuid.New().String()
	argsJSON, _ := json.Marshal(args)
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	_, err = db.DB.Exec(`
		INSERT INTO applications (application_id, application_name, application_executable_path, application_arguments,
		                         application_working_directory, application_status, application_creation_time, application_update_time)
		VALUES (?, ?, ?, ?, ?, 'stopped', ?, ?)
	`, appID, name, execPath, string(argsJSON), workDir, now, now)

	return err
}

// StartApp launches the app under its supervising relay, resets the
// automatic-restart counter and marks the app as wanted.
func StartApp(name string, inlineEnvs map[string]string) (int, error) {
	return startApp(name, inlineEnvs, true)
}

func startApp(name string, inlineEnvs map[string]string, resetCount bool) (int, error) {
	app, err := GetAppDetail(name)
	if err != nil {
		return 0, err
	}
	if app.Status == "running" && app.PID != 0 {
		return 0, fmt.Errorf("application %s is already running with PID %d", name, app.PID)
	}
	if app.Status == "restarting" && relayState(name) == relayAlive {
		// Its relay is waiting to restart it: take over instead.
		if err := setStatus(name, "stopped", "application_status = 'restarting'"); err != nil {
			return 0, err
		}
		if !endRelay(name, killWait) {
			return 0, fmt.Errorf("application %s is waiting to be restarted and its relay (PID %d) did not exit", name, relayPID(name))
		}
	}

	// Resolve env values before spawning anything. Secured values need read
	// access to the master key; no root/admin check is involved.
	envs, err := env.ListEnv(name)
	if err != nil {
		return 0, err
	}
	for _, e := range envs {
		if e.Locked {
			return 0, fmt.Errorf("cannot decrypt secured variable %s: no read access to the master key", e.Name)
		}
	}

	// Make sure the status update at the end can succeed before spawning
	// anything; otherwise a caller without write access would leave an
	// untracked relay and app running. The no-op UPDATE still needs a write
	// lock, so a read-only database fails here.
	if _, err := db.DB.Exec(`UPDATE applications SET application_update_time = application_update_time WHERE application_name = ?`, name); err != nil {
		return 0, err
	}

	return launchSupervisor(name, StartRequest{InlineEnvs: inlineEnvs, UserStart: resetCount})
}

// StopApp terminates a running application.
func StopApp(name string) error {
	return StopAppTimeout(name, DefaultStopTimeout)
}

// StopAppTimeout asks the app to exit, waits up to timeout, then kills it.
// It returns once the process is gone and the app is recorded as stopped.
func StopAppTimeout(name string, timeout time.Duration) error {
	return stopApp(name, timeout, true)
}

// StopForShutdown stops the app like StopAppTimeout but keeps it wanted, so
// it comes back at the next boot (system shutdown, uninstall).
func StopForShutdown(name string, timeout time.Duration) error {
	return stopApp(name, timeout, false)
}

func stopApp(name string, timeout time.Duration, byUser bool) error {
	app, err := GetAppDetail(name)
	if err != nil {
		return err
	}
	// A stop by the user means "keep it down", even if it already exited.
	// This also fails early, before any signal, without write access.
	if byUser {
		if _, err := db.DB.Exec("UPDATE applications SET application_desired_state = 'down' WHERE application_name = ?", name); err != nil {
			return err
		}
	}

	// Waiting to be restarted after a crash: cancel the restart.
	if app.Status == "restarting" {
		return setStatus(name, "stopped", "application_status = 'restarting'")
	}
	if app.Status != "running" || app.PID == 0 {
		return fmt.Errorf("application %s %w", name, ErrNotRunning)
	}

	// Record the intent first so the relay sees a requested stop, not a crash.
	// This also fails early, before any signal, for a caller without write access.
	if err := setStatus(name, "stopping", fmt.Sprintf("application_pid = %d", app.PID)); err != nil {
		return err
	}
	if err := gracefulStop(app.PID, timeout); err != nil {
		if isProcessAlive(app.PID) {
			_ = setStatus(name, "running", fmt.Sprintf("application_pid = %d", app.PID))
			return fmt.Errorf("failed to stop process %d: %w", app.PID, err)
		}
	}

	// The relay sees the pipe close, records the stop and releases its lock;
	// give it a moment, then make sure it is gone. A relay that took no lock
	// (started by an older version) is found by its PID file.
	relayPIDFile := config.GetRelayPIDFilePath(name)
	if relayState(name) != relayUnknown {
		deadline := time.Now().Add(3 * time.Second)
		for relayState(name) == relayAlive && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		_ = endRelay(name, killWait)
	} else if data, readErr := os.ReadFile(relayPIDFile); readErr == nil {
		if relayPID, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && relayPID > 0 {
			if !waitExit(relayPID, 3*time.Second) {
				_ = forceKill(relayPID)
			}
		}
		_ = os.Remove(relayPIDFile)
	}

	return setStatusPID0(name, "stopped", fmt.Sprintf("application_pid IN (0, %d)", app.PID))
}

// setStatus updates the status where cond holds (a trusted SQL fragment).
func setStatus(name, status, cond string) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.DB.Exec(`
		UPDATE applications SET application_status = ?, application_status_time = ?, application_update_time = ?
		WHERE application_name = ? AND `+cond, status, now, now, name)
	return err
}

// setStatusPID0 is setStatus that also clears the PID.
func setStatusPID0(name, status, cond string) error {
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err := db.DB.Exec(`
		UPDATE applications SET application_status = ?, application_pid = 0, application_status_time = ?, application_update_time = ?
		WHERE application_name = ? AND `+cond, status, now, now, name)
	return err
}

// RestartApp stops and then starts an application. An app that is not running
// is simply started; any other stop failure (e.g. missing permissions) is
// returned instead of being masked by a later "already running" error.
func RestartApp(name string) (int, error) {
	return RestartAppTimeout(name, DefaultStopTimeout)
}

// RestartAppTimeout is RestartApp with a custom graceful-stop timeout.
func RestartAppTimeout(name string, timeout time.Duration) (int, error) {
	if err := StopAppTimeout(name, timeout); err != nil && !errors.Is(err, ErrNotRunning) {
		return 0, err
	}
	return StartApp(name, nil)
}

// DeleteApp removes an application.
func DeleteApp(name string, force bool) error {
	app, err := GetAppDetail(name)
	if err != nil {
		return err
	}

	if app.Status == "running" {
		if !force {
			return fmt.Errorf("application %s is currently running. Use --force to delete", name)
		}
		_ = StopApp(name)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}

	_, _ = tx.Exec("DELETE FROM application_environment_variables WHERE application_id = ?", app.ID)
	_, _ = tx.Exec("DELETE FROM grants WHERE application_id = ?", app.ID)
	_, _ = tx.Exec("DELETE FROM hooks WHERE application_id = ?", app.ID)
	_, err = tx.Exec("DELETE FROM applications WHERE application_id = ?", app.ID)

	if err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func ListApps() ([]Application, error) {
	rows, err := db.DB.Query(`
		SELECT application_id, application_name, application_executable_path, application_arguments, 
		       application_status, application_pid, application_creation_time, application_update_time
		FROM applications ORDER BY application_name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var apps []Application
	for rows.Next() {
		var app Application
		var argsJSON string
		var creationTimeStr, updateTimeStr string
		if err := rows.Scan(&app.ID, &app.Name, &app.ExecutablePath, &argsJSON, &app.Status, &app.PID, &creationTimeStr, &updateTimeStr); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(argsJSON), &app.Arguments)
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close() // release the read before reconcileStatus may write
	for i := range apps {
		apps[i].Status, apps[i].PID = reconcileStatus(apps[i].Name, apps[i].Status, apps[i].PID)
	}
	return apps, nil
}

// UpdateApp replaces an application's executable and arguments. A nil workDir
// keeps the current working directory; a pointer to "" clears it.
func UpdateApp(name string, execPath string, args []string, workDir *string) error {
	argsJSON, _ := json.Marshal(args)
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	var res sql.Result
	var err error
	if workDir == nil {
		res, err = db.DB.Exec(`
			UPDATE applications
			SET application_executable_path = ?, application_arguments = ?, application_update_time = ?
			WHERE application_name = ?
		`, execPath, string(argsJSON), now, name)
	} else {
		dir, dirErr := resolveWorkingDir(*workDir)
		if dirErr != nil {
			return dirErr
		}
		res, err = db.DB.Exec(`
			UPDATE applications
			SET application_executable_path = ?, application_arguments = ?, application_working_directory = ?,
			    application_update_time = ?
			WHERE application_name = ?
		`, execPath, string(argsJSON), dir, now, name)
	}
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("application %s not found", name)
	}
	return nil
}

func GetAppStatus(name string) (string, int, error) {
	var status string
	var pid int
	err := db.DB.QueryRow("SELECT application_status, application_pid FROM applications WHERE application_name = ?", name).Scan(&status, &pid)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", 0, fmt.Errorf("application %s not found", name)
		}
		return "", 0, err
	}
	status, pid = reconcileStatus(name, status, pid)
	return status, pid, nil
}
