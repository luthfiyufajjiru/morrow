package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"morrow/internal/db"
	"morrow/internal/env"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Application struct {
	ID                   string            `json:"application_id"`
	Name                 string            `json:"application_name"`
	ExecutablePath       string            `json:"application_executable_path"`
	Arguments            []string          `json:"application_arguments"`
	EnvironmentVariables map[string]string `json:"application_environment_variables"`
	Status               string            `json:"application_status"`
	PID                  int               `json:"application_pid"`
	CreationTime         time.Time         `json:"application_creation_time"`
	UpdateTime           time.Time         `json:"application_update_time"`
	StatusTime           *time.Time        `json:"application_status_time,omitempty"`
	LastRunTime          *time.Time        `json:"application_last_run_time,omitempty"`
	CommandLine          string            `json:"application_command_line"`
}

// GetAppDetail retrieves full information for a named application.
func GetAppDetail(name string) (*Application, error) {
	var app Application
	var argsJSON string
	var creationTimeStr, updateTimeStr, statusTimeStr string
	var lastRunTimeStr sql.NullString

	err := db.DB.QueryRow(`
		SELECT application_id, application_name, application_executable_path, application_arguments, 
		       application_status, application_pid, application_creation_time, application_update_time,
		       application_status_time, application_last_run_time
		FROM applications WHERE application_name = ?
	`, name).Scan(
		&app.ID, &app.Name, &app.ExecutablePath, &argsJSON,
		&app.Status, &app.PID, &creationTimeStr, &updateTimeStr,
		&statusTimeStr, &lastRunTimeStr,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("application %s not found", name)
		}
		return nil, err
	}

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

	if lastRunTimeStr.Valid {
		lr, _ := parseTime(lastRunTimeStr.String)
		app.LastRunTime = &lr
	}

	// Fetch environment variables
	envs, err := env.ListEnv(name)
	if err != nil {
		return nil, err
	}

	isRoot := os.Geteuid() == 0 || os.Geteuid() == -1

	app.EnvironmentVariables = make(map[string]string)
	for _, e := range envs {
		val := e.Value
		if e.IsSecured && !isRoot {
			val = "****"
		}
		app.EnvironmentVariables[e.Name] = val
	}

	// Generate the CommandLine string
	envPairs := ""
	for k, v := range app.EnvironmentVariables {
		envPairs += fmt.Sprintf("%s=%s ", k, v)
	}
	app.CommandLine = fmt.Sprintf("%s%s %s", envPairs, app.ExecutablePath, strings.Join(app.Arguments, " "))

	return &app, nil
}

// CreateApp initializes a new managed application.
func CreateApp(name string, execPath string, args []string) error {
	var count int
	err := db.DB.QueryRow("SELECT count(*) FROM applications WHERE application_name = ?", name).Scan(&count)
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
		                         application_status, application_creation_time, application_update_time)
		VALUES (?, ?, ?, ?, 'stopped', ?, ?)
	`, appID, name, execPath, string(argsJSON), now, now)
	
	return err
}

// StartApp launches the application.
func StartApp(name string, inlineEnvs map[string]string) (int, error) {
	app, err := GetAppDetail(name)
	if err != nil {
		return 0, err
	}

	cmd := exec.Command(app.ExecutablePath, app.Arguments...)
	cmd.Env = os.Environ()
	
	cmd.Env = append(cmd.Env, fmt.Sprintf("MORROW_APP_ID=%s", app.ID))
	cmd.Env = append(cmd.Env, fmt.Sprintf("MORROW_APP_NAME=%s", app.Name))

	for k, v := range app.EnvironmentVariables {
		if v == "****" {
			return 0, fmt.Errorf("cannot start app with secured variables as a non-root user")
		}
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	for k, v := range inlineEnvs {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("failed to start process: %w", err)
	}

	pid := cmd.Process.Pid
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	_, err = db.DB.Exec(`
		UPDATE applications
		SET application_status = 'running', application_pid = ?, application_status_time = ?, 
		    application_last_run_time = ?, application_update_time = ?
		WHERE application_name = ?
	`, pid, now, now, now, name)

	return pid, err
}

// StopApp terminates a running application.
func StopApp(name string) error {
	app, err := GetAppDetail(name)
	if err != nil {
		return err
	}

	if app.Status != "running" || app.PID == 0 {
		return fmt.Errorf("application %s is not running", name)
	}

	// Call platform-specific termination logic
	_ = terminateProcess(app.PID)

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err = db.DB.Exec(`
		UPDATE applications
		SET application_status = 'stopped', application_pid = 0, 
		    application_status_time = ?, application_update_time = ?
		WHERE application_name = ?
	`, now, now, name)

	return err
}

// RestartApp stops and then starts an application.
func RestartApp(name string) (int, error) {
	_ = StopApp(name)
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
	return apps, nil
}

func UpdateApp(name string, execPath string, args []string) error {
	argsJSON, _ := json.Marshal(args)
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")

	res, err := db.DB.Exec(`
		UPDATE applications
		SET application_executable_path = ?, application_arguments = ?, application_update_time = ?
		WHERE application_name = ?
	`, execPath, string(argsJSON), now, name)
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
	return status, pid, nil
}
