// Package hooks runs alerts when an app exits unexpectedly: a shell command
// or an HTTP POST. Targets are stored encrypted with the master key because
// commands and webhook URLs often carry tokens, so only administrators can
// add, read or run them.
package hooks

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"morrow/internal/crypto"
	"morrow/internal/db"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Events a hook can subscribe to.
const (
	EventCrash  = "crash"  // exited unexpectedly with a non-zero code or a signal
	EventExit   = "exit"   // exited unexpectedly with code 0
	EventGiveUp = "giveup" // the restart policy stopped restarting it
	// EventUnhealthy: the health check failed too often; the app is restarted.
	EventUnhealthy = "unhealthy"
)

// Hook kinds.
const (
	KindExec    = "exec"
	KindWebhook = "webhook"
)

const (
	execTimeout    = 30 * time.Second
	webhookTimeout = 10 * time.Second
	maxOutput      = 2048
)

// Event describes an exit, passed to hooks.
type Event struct {
	Name         string    `json:"event"`
	App          string    `json:"app"`
	AppID        string    `json:"app_id"`
	ExitCode     int       `json:"exit_code"`
	Signal       string    `json:"signal,omitempty"`
	RestartCount int       `json:"restart_count"`
	Time         time.Time `json:"time"`
	Host         string    `json:"host"`
}

// Hook is a stored alert. Target is "****" when it cannot be decrypted.
type Hook struct {
	ID     string
	App    string
	Events []string
	Kind   string
	Target string
}

// ParseEvents validates a comma-separated event list.
func ParseEvents(list []string) ([]string, error) {
	var out []string
	for _, e := range list {
		e = strings.TrimSpace(strings.ToLower(e))
		switch e {
		case EventCrash, EventExit, EventGiveUp, EventUnhealthy:
			out = append(out, e)
		case "":
		default:
			return nil, fmt.Errorf("unknown event %q (use %s, %s, %s or %s)", e, EventCrash, EventExit, EventGiveUp, EventUnhealthy)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no events given")
	}
	return out, nil
}

// CheckTarget validates a hook target for its kind.
func CheckTarget(kind, target string) error {
	switch kind {
	case KindExec:
		if strings.TrimSpace(target) == "" {
			return fmt.Errorf("the command is empty")
		}
	case KindWebhook:
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("the webhook must be an http(s) URL")
		}
	default:
		return fmt.Errorf("unknown hook kind %q", kind)
	}
	return nil
}

func appID(app string) (string, error) {
	var id string
	err := db.DB.QueryRow("SELECT application_id FROM applications WHERE application_name = ?", app).Scan(&id)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("application %s not found", app)
	}
	return id, err
}

// Add stores a hook for app and returns its ID.
func Add(app string, events []string, kind, target string) (string, error) {
	evs, err := ParseEvents(events)
	if err != nil {
		return "", err
	}
	if err := CheckTarget(kind, target); err != nil {
		return "", err
	}
	id, err := appID(app)
	if err != nil {
		return "", err
	}
	enc, err := crypto.Encrypt(target)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt the hook target: %w", err)
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	hookID := hex.EncodeToString(b)
	_, err = db.DB.Exec(`INSERT INTO hooks (hook_id, application_id, hook_events, hook_kind, hook_target, hook_creation_time)
		VALUES (?, ?, ?, ?, ?, ?)`, hookID, id, strings.Join(evs, ","), kind, enc, time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	return hookID, err
}

// List returns the hooks of app, or of every app when app is empty.
func List(app string) ([]Hook, error) {
	q := `SELECT hooks.hook_id, applications.application_name, hooks.hook_events, hooks.hook_kind, hooks.hook_target
		FROM hooks JOIN applications ON hooks.application_id = applications.application_id`
	var args []any
	if app != "" {
		q += " WHERE applications.application_name = ?"
		args = append(args, app)
	}
	q += " ORDER BY applications.application_name, hooks.hook_creation_time, hooks.hook_id"
	rows, err := db.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hook
	for rows.Next() {
		var h Hook
		var evs, enc string
		if err := rows.Scan(&h.ID, &h.App, &evs, &h.Kind, &enc); err != nil {
			return nil, err
		}
		h.Events = strings.Split(evs, ",")
		if t, err := crypto.Decrypt(enc); err == nil {
			h.Target = t
		} else {
			h.Target = "****"
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Remove deletes one hook of app.
func Remove(app, id string) error {
	aid, err := appID(app)
	if err != nil {
		return err
	}
	res, err := db.DB.Exec("DELETE FROM hooks WHERE application_id = ? AND hook_id = ?", aid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("hook %s not found for %s", id, app)
	}
	return nil
}

// Fire runs every hook of the event's app subscribed to the event, one after
// the other, and writes each outcome to logf. Errors never stop the caller.
func Fire(ev Event, logf func(string)) {
	hs, err := List(ev.App)
	if err != nil {
		logf(fmt.Sprintf("[morrow] cannot load hooks: %v", err))
		return
	}
	if ev.Host == "" {
		ev.Host, _ = os.Hostname()
	}
	for _, h := range hs {
		if !subscribed(h, ev.Name) {
			continue
		}
		if h.Target == "****" {
			logf(fmt.Sprintf("[morrow] hook %s skipped: cannot decrypt it (master key unreadable)", h.ID))
			continue
		}
		if err := Run(h, ev); err != nil {
			logf(fmt.Sprintf("[morrow] hook %s (%s on %s) failed: %v", h.ID, h.Kind, ev.Name, err))
		} else {
			logf(fmt.Sprintf("[morrow] hook %s (%s on %s) ran", h.ID, h.Kind, ev.Name))
		}
	}
}

func subscribed(h Hook, event string) bool {
	for _, e := range h.Events {
		if e == event {
			return true
		}
	}
	return false
}

// Run executes one hook for ev.
func Run(h Hook, ev Event) error {
	if ev.Host == "" {
		ev.Host, _ = os.Hostname()
	}
	switch h.Kind {
	case KindExec:
		return runExec(h.Target, ev)
	case KindWebhook:
		return postWebhook(h.Target, ev)
	}
	return fmt.Errorf("unknown hook kind %q", h.Kind)
}

func runExec(command string, ev Event) error {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()
	cmd := shellCommand(ctx, command)
	cmd.Env = append(os.Environ(),
		"MORROW_EVENT="+ev.Name,
		"MORROW_APP_NAME="+ev.App,
		"MORROW_APP_ID="+ev.AppID,
		fmt.Sprintf("MORROW_EXIT_CODE=%d", ev.ExitCode),
		"MORROW_EXIT_SIGNAL="+ev.Signal,
		fmt.Sprintf("MORROW_RESTART_COUNT=%d", ev.RestartCount),
		"MORROW_EVENT_TIME="+ev.Time.Format(time.RFC3339),
		"MORROW_HOST="+ev.Host,
	)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("timed out after %s", execTimeout)
	}
	if err != nil {
		if len(out) > maxOutput {
			out = out[:maxOutput]
		}
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func postWebhook(target string, ev Event) error {
	body, _ := json.Marshal(ev)
	ctx, cancel := context.WithTimeout(context.Background(), webhookTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "morrow-hook")
	host := req.URL.Host
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// The error text contains the full URL, which may hold a token.
		return fmt.Errorf("POST to %s failed", host)
	}
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("POST to %s returned %s", host, resp.Status)
	}
	return nil
}
