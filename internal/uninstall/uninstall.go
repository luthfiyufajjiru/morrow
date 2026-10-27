// Package uninstall removes what `morrow init` set up. By default it stops
// the apps and removes the inbox (hook, folder, keys, group) but keeps the
// database, master key and logs, so reinstalling resumes where it left off.
// With Purge it also deletes those.
//
// It only deletes files and folders Morrow owns by name. Folders that may
// hold other things (MORROW_HOME, or the binary's folder for a legacy
// install) are removed only if they end up empty.
package uninstall

import (
	"errors"
	"fmt"
	"io"
	"morrow/internal/app"
	"morrow/internal/config"
	"morrow/internal/db"
	"morrow/internal/env"
	"morrow/internal/inbox"
	"morrow/internal/privilege"
	"morrow/internal/startup"
	"os"
	"path/filepath"
)

// RunningApp is an app that uninstall will stop.
type RunningApp struct {
	Name string
	PID  int
}

// Plan is everything an uninstall would do, computed without changing anything.
type Plan struct {
	System  bool   // system install (false: per-user MORROW_HOME)
	DataDir string // folder holding the database
	Purge   bool

	Apps      []RunningApp
	Hook      string   // description of the installed auto-apply hook, if any
	Startup   string   // description of the installed boot hook, if any
	Inbox     []string // inbox folder and key files that exist
	Group     string   // deploy group to delete, if it exists
	Delete    []string // with Purge: database, key and logs that exist
	Keep      []string // without Purge: what stays for a reinstall
	EmptyDirs []string // removed afterwards only if empty
	Secured   []env.SecuredRef
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func existing(paths ...string) []string {
	var out []string
	for _, p := range paths {
		if p != "" && exists(p) {
			out = append(out, p)
		}
	}
	return out
}

// Build computes the plan. db.DB may be nil when there is no database.
func Build(purge bool) (Plan, error) {
	l := config.Current()
	p := Plan{System: !config.IsPerUser(), DataDir: filepath.Dir(l.DBPath), Purge: purge}

	if db.DB != nil {
		apps, err := app.ListApps()
		if err != nil {
			return p, err
		}
		for _, a := range apps {
			if a.Status == "running" {
				p.Apps = append(p.Apps, RunningApp{a.Name, a.PID})
			}
		}
	}
	if p.System {
		if desc, ok := inbox.HookInstalled(); ok {
			p.Hook = desc
		}
		if desc, ok := startup.Installed(); ok {
			p.Startup = desc
		}
		if privilege.DeployGroupExists() {
			p.Group = privilege.DeployGroup
		}
	}
	p.Inbox = existing(config.GetInboxDir(), config.GetInboxPublicKeyPath(), config.GetInboxPrivateKeyPath())

	data := existing(l.DBPath, l.DBPath+"-wal", l.DBPath+"-shm", l.DBPath+"-journal", l.KeyPath,
		filepath.Join(filepath.Dir(config.GetInboxPrivateKeyPath()), lockFile), l.LogsDir)
	if purge {
		p.Delete = data
		if db.DB != nil {
			secured, err := env.ListSecured()
			if err != nil {
				return p, err
			}
			p.Secured = secured
		}
	} else {
		p.Keep = existing(l.DBPath, l.KeyPath, l.LogsDir)
	}
	for _, d := range []string{filepath.Dir(l.DBPath), filepath.Dir(l.KeyPath), filepath.Dir(l.LogsDir)} {
		if !contains(p.EmptyDirs, d) {
			p.EmptyDirs = append(p.EmptyDirs, d)
		}
	}
	return p, nil
}

const lockFile = ".morrow-inbox.lock"

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Lines describes the plan for --dry-run and the confirmation prompt.
func (p Plan) Lines() []string {
	var out []string
	mode := "system install"
	if !p.System {
		mode = "per-user install"
	}
	out = append(out, fmt.Sprintf("Morrow %s in %s", mode, p.DataDir))
	if len(p.Apps) == 0 {
		out = append(out, "  stop apps:      none running")
	}
	for _, a := range p.Apps {
		out = append(out, fmt.Sprintf("  stop app:       %s (PID %d) and its log relay", a.Name, a.PID))
	}
	if p.Hook != "" {
		out = append(out, "  remove hook:    "+p.Hook)
	}
	if p.Startup != "" {
		out = append(out, "  remove startup: "+p.Startup)
	}
	for _, f := range p.Inbox {
		out = append(out, "  remove inbox:   "+f)
	}
	if p.Group != "" {
		out = append(out, "  remove group:   "+p.Group)
	}
	for _, f := range p.Delete {
		out = append(out, "  delete:         "+f)
	}
	for _, f := range p.Keep {
		out = append(out, "  keep:           "+f+"  (use --purge to delete)")
	}
	if len(p.Secured) > 0 {
		out = append(out, fmt.Sprintf("  %d secured variable(s) become unrecoverable unless you keep a copy of the key:", len(p.Secured)))
		for _, s := range p.Secured {
			out = append(out, fmt.Sprintf("    %s: %s", s.App, s.Name))
		}
	}
	return out
}

// Execute carries out the plan. If backupKey is set and the plan purges the
// master key, the key is copied there first. Apps that fail to stop abort the
// run before anything is deleted.
func Execute(p Plan, backupKey string) error {
	if exists(filepath.Dir(config.GetInboxPrivateKeyPath())) {
		unlock, err := inbox.Lock()
		if err != nil {
			return err
		}
		defer unlock()
	}

	var stopErrs []error
	for _, a := range p.Apps {
		if err := app.StopForShutdown(a.Name, app.DefaultStopTimeout); err != nil && !errors.Is(err, app.ErrNotRunning) {
			stopErrs = append(stopErrs, fmt.Errorf("stop %s: %w", a.Name, err))
		}
	}
	if err := errors.Join(stopErrs...); err != nil {
		return fmt.Errorf("nothing was removed because some apps could not be stopped:\n%w", err)
	}

	if p.Startup != "" {
		if err := startup.Remove(); err != nil {
			return err
		}
	}
	if p.Hook != "" {
		if err := inbox.RemoveHook(); err != nil {
			return err
		}
	}
	for _, f := range p.Inbox {
		if err := os.RemoveAll(f); err != nil {
			return err
		}
	}
	if p.Group != "" {
		if err := privilege.RemoveDeployGroup(); err != nil {
			return err
		}
	}
	if !p.Purge {
		return nil
	}

	key := config.Current().KeyPath
	if backupKey != "" && contains(p.Delete, key) {
		if err := copyFile(key, backupKey); err != nil {
			return fmt.Errorf("key backup failed, nothing purged: %w", err)
		}
	}
	// The database file is open (and locked on Windows) until closed.
	if db.DB != nil {
		db.DB.Close()
		db.DB = nil
	}
	for _, f := range p.Delete {
		if f == filepath.Join(filepath.Dir(config.GetInboxPrivateKeyPath()), lockFile) {
			continue // released by the deferred unlock
		}
		if err := os.RemoveAll(f); err != nil {
			return err
		}
	}
	return nil
}

// RemoveEmptyDirs deletes the plan's folders that are now empty. It must run
// after Execute has released the inbox lock.
func RemoveEmptyDirs(p Plan) {
	for _, d := range p.EmptyDirs {
		_ = os.Remove(d) // fails, as intended, if anything else is in there
	}
}

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
