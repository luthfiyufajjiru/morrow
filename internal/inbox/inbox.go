// Package inbox lets non-admin deployers set secured variables without being
// able to read them. A deployer seals each value with the install's public
// key and drops it into a write-only inbox directory; an administrator (or the
// OS hook installed by `morrow init --enable-inbox`) runs `morrow apply`,
// which checks who wrote each file against the grants and stores the value.
package inbox

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"morrow/internal/app"
	"morrow/internal/config"
	"morrow/internal/crypto"
	"morrow/internal/db"
	"morrow/internal/env"
	"morrow/internal/grant"
	"morrow/internal/privilege"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	fileSuffix = ".sealed"
	// MaxFileSize bounds what apply reads from an untrusted file.
	MaxFileSize = 64 << 10
	// MaxPerRun bounds how many files one apply run processes.
	MaxPerRun = 1000
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ownerOf identifies who wrote a submission file; tests replace it to act as
// different users without needing real accounts.
var ownerOf = fileOwner

// payload is what gets sealed. App and Name are inside the ciphertext so a
// file cannot be retargeted, and ID makes replays detectable.
type payload struct {
	Version int    `json:"v"`
	ID      string `json:"id"`
	App     string `json:"app"`
	Name    string `json:"name"`
	Value   string `json:"value"`
	Time    int64  `json:"ts"`
	Restart bool   `json:"restart"`
}

// Pair is one variable to submit.
type Pair struct {
	Name  string
	Value string
}

// CurrentPrincipal returns the normalized account name of this process.
func CurrentPrincipal() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return grant.NormalizePrincipal(u.Username), nil
}

// Submit seals each pair and writes it to the inbox. Grants and formats are
// checked here for early feedback; apply enforces them again with the file
// owner the OS recorded, which the submitter cannot forge. It returns the
// submission IDs.
func Submit(appName string, pairs []Pair, restart bool) ([]string, error) {
	who, err := CurrentPrincipal()
	if err != nil {
		return nil, err
	}
	for _, p := range pairs {
		if err := grant.CheckName(p.Name); err != nil {
			return nil, err
		}
		format, ok, err := grant.Lookup(who, appName, p.Name)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%s has no grant to submit %s for %s; an administrator can add one with "+
				"'morrow grant %s --app %s --vars %s'", who, p.Name, appName, who, appName, p.Name)
		}
		if err := grant.CheckValue(p.Value, format); err != nil {
			return nil, fmt.Errorf("%s: %w", p.Name, err)
		}
	}

	var ids []string
	for _, p := range pairs {
		id, err := newID()
		if err != nil {
			return ids, err
		}
		msg, err := json.Marshal(payload{Version: 1, ID: id, App: appName, Name: p.Name, Value: p.Value,
			Time: time.Now().Unix(), Restart: restart})
		if err != nil {
			return ids, err
		}
		sealed, err := crypto.Seal(msg)
		if err != nil {
			return ids, err
		}
		path := filepath.Join(config.GetInboxDir(), id+fileSuffix)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if errors.Is(err, os.ErrPermission) {
			return ids, fmt.Errorf("you cannot write to the inbox %s: ask an administrator to add you to the %q group "+
				"(then log in again so the membership applies)", config.GetInboxDir(), privilege.DeployGroup)
		}
		if err != nil {
			return ids, fmt.Errorf("failed to write to the inbox: %w", err)
		}
		_, werr := f.Write(sealed)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			_ = os.Remove(path)
			return ids, fmt.Errorf("failed to write to the inbox: %w", werr)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func newID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Result summarizes one apply run.
type Result struct {
	Applied   int
	Rejected  int
	Restarted []string
	Errors    []string // restart failures, not per-submission rejections
}

// Apply processes the inbox. It must run with administrator rights: it reads
// the private key and writes the database.
func Apply() (Result, error) {
	var res Result
	unlock, err := Lock()
	if err != nil {
		return res, err
	}
	defer unlock()

	dir := config.GetInboxDir()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return res, crypto.ErrInboxDisabled
	}
	if err != nil {
		return res, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) > MaxPerRun {
		names = names[:MaxPerRun]
	}

	restart := map[string]bool{}
	for _, name := range names {
		path := filepath.Join(dir, name)
		p, who, err := readSubmission(path, name)
		if err == nil {
			err = check(p, who)
		}
		if err == nil {
			err = env.SetEnv(p.App, p.Name, p.Value, true)
		}
		if err == nil {
			err = recordEvent(p.ID, p.App, p.Name, who.name, "applied", "")
		}
		if err != nil {
			res.Rejected++
			id, appName, envName := "file:"+name, "", ""
			if p != nil {
				appName, envName = p.App, p.Name
				if idPattern.MatchString(p.ID) {
					id = p.ID
				}
			}
			_ = recordEvent(uniqueEventID(id), appName, envName, who.name, "rejected", err.Error())
		} else {
			res.Applied++
			if p.Restart {
				restart[p.App] = true
			}
		}
		_ = os.Remove(path)
	}

	for _, a := range sortedKeys(restart) {
		status, _, err := app.GetAppStatus(a)
		if err != nil || status != "running" {
			continue // stopped apps pick the value up on their next start
		}
		if _, err := app.RestartApp(a); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("restart %s: %v", a, err))
			continue
		}
		res.Restarted = append(res.Restarted, a)
	}
	return res, nil
}

// uniqueEventID keeps a rejection (e.g. a replay) from colliding with the
// event already recorded for the same submission ID.
func uniqueEventID(id string) string {
	var n int
	_ = db.DB.QueryRow("SELECT count(*) FROM inbox_events WHERE submission_id = ?", id).Scan(&n)
	if n == 0 {
		return id
	}
	return fmt.Sprintf("%s:rejected:%d", id, time.Now().UnixNano())
}

type submitter struct {
	name       string
	privileged bool // root / Administrators / SYSTEM: may submit any variable
}

// readSubmission validates the file itself and decrypts it.
func readSubmission(path, name string) (*payload, submitter, error) {
	var who submitter
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, who, err
	}
	if !fi.Mode().IsRegular() {
		return nil, who, fmt.Errorf("not a regular file (symlinks and other special files are rejected)")
	}
	who, err = ownerOf(path, fi)
	if err != nil {
		return nil, who, err
	}
	if !strings.HasSuffix(name, fileSuffix) {
		return nil, who, fmt.Errorf("unexpected file name %q", name)
	}
	if fi.Size() > MaxFileSize {
		return nil, who, fmt.Errorf("file is larger than %d bytes", MaxFileSize)
	}
	f, err := openNoFollow(path)
	if err != nil {
		return nil, who, err
	}
	defer f.Close()
	sealed, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return nil, who, err
	}
	if len(sealed) > MaxFileSize {
		return nil, who, fmt.Errorf("file is larger than %d bytes", MaxFileSize)
	}
	msg, err := crypto.OpenSealed(sealed)
	if err != nil {
		return nil, who, err
	}
	var p payload
	dec := json.NewDecoder(bytes.NewReader(msg))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, who, fmt.Errorf("malformed submission: %w", err)
	}
	if p.Version != 1 || !idPattern.MatchString(p.ID) || name != p.ID+fileSuffix {
		return &p, who, fmt.Errorf("malformed submission (version, id or file name)")
	}
	return &p, who, nil
}

// check enforces replay protection and the grant, name and value rules for
// the file's owner. Privileged owners may set any name, like set-env.
func check(p *payload, who submitter) error {
	var exists int
	if err := db.DB.QueryRow("SELECT count(*) FROM inbox_events WHERE submission_id = ?", p.ID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return fmt.Errorf("submission %s was already processed (replay)", p.ID)
	}
	if who.privileged {
		return grant.CheckValue(p.Value, "any")
	}
	if err := grant.CheckName(p.Name); err != nil {
		return err
	}
	format, ok, err := grant.Lookup(who.name, p.App, p.Name)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%s has no grant for %s of %s", who.name, p.Name, p.App)
	}
	return grant.CheckValue(p.Value, format)
}

func recordEvent(id, appName, envName, principal, status, reason string) error {
	_, err := db.DB.Exec(`
		INSERT INTO inbox_events (submission_id, application_name, env_name, principal, status, reason, event_time)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, id, appName, envName, principal, status, reason, time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	return err
}

// Event is the outcome of one processed submission. It never holds a value.
type Event struct {
	ID, App, EnvName, Principal, Status, Reason, Time string
}

// Events returns the most recent processed submissions, newest first.
func Events(limit int) ([]Event, error) {
	rows, err := db.DB.Query(`
		SELECT submission_id, application_name, env_name, principal, status, reason, event_time
		FROM inbox_events ORDER BY event_time DESC, rowid DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.App, &e.EnvName, &e.Principal, &e.Status, &e.Reason, &e.Time); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Lock prevents two apply runs (the OS hook and a manual run) from
// processing the same files. A lock older than ten minutes is stale.
func Lock() (func(), error) {
	path := filepath.Join(filepath.Dir(config.GetInboxPrivateKeyPath()), ".morrow-inbox.lock")
	for i := 0; i < 2; i++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if fi, serr := os.Stat(path); serr == nil && time.Since(fi.ModTime()) > 10*time.Minute {
			_ = os.Remove(path)
			continue
		}
		return nil, fmt.Errorf("another 'morrow apply' is running (lock %s)", path)
	}
	return nil, fmt.Errorf("could not take the inbox lock %s", path)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
