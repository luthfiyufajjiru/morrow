package grant

import (
	"database/sql"
	"fmt"
	"morrow/internal/db"
	"os/user"
	"runtime"
	"strings"
)

// Grant allows Principal to submit EnvName of App through the inbox.
type Grant struct {
	Principal string
	App       string
	EnvName   string
	Format    string
}

// NormalizePrincipal makes account names comparable: Windows account names
// are case-insensitive, Unix ones are not.
func NormalizePrincipal(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(name)
	}
	return name
}

// ResolvePrincipal checks that a local account exists and returns its
// canonical, normalized name (DOMAIN\name on Windows).
func ResolvePrincipal(name string) (string, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return "", fmt.Errorf("unknown user %q: %w", name, err)
	}
	return NormalizePrincipal(u.Username), nil
}

func appID(app string) (string, error) {
	var id string
	err := db.DB.QueryRow("SELECT application_id FROM applications WHERE application_name = ?", app).Scan(&id)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("application %s not found", app)
	}
	return id, err
}

// Add grants principal the right to submit vars of app, each checked against
// the name rules, with values required to match format.
func Add(principal, app string, vars []string, format string) error {
	if len(vars) == 0 {
		return fmt.Errorf("no variables given")
	}
	if err := CheckFormat(format); err != nil {
		return err
	}
	for _, v := range vars {
		if err := CheckName(v); err != nil {
			return err
		}
	}
	id, err := appID(app)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	for _, v := range vars {
		if _, err := tx.Exec(`
			INSERT INTO grants (principal, application_id, env_name, format) VALUES (?, ?, ?, ?)
			ON CONFLICT(principal, application_id, env_name) DO UPDATE SET format = excluded.format
		`, principal, id, v, format); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// Remove revokes the given vars of app from principal, or all of them when
// vars is empty. It returns how many grants were removed.
func Remove(principal, app string, vars []string) (int64, error) {
	id, err := appID(app)
	if err != nil {
		return 0, err
	}
	if len(vars) == 0 {
		res, err := db.DB.Exec("DELETE FROM grants WHERE principal = ? AND application_id = ?", principal, id)
		if err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	var n int64
	for _, v := range vars {
		res, err := db.DB.Exec("DELETE FROM grants WHERE principal = ? AND application_id = ? AND env_name = ?", principal, id, v)
		if err != nil {
			return n, err
		}
		c, _ := res.RowsAffected()
		n += c
	}
	return n, nil
}

// List returns every grant, ordered by principal, app and variable.
func List() ([]Grant, error) {
	rows, err := db.DB.Query(`
		SELECT grants.principal, applications.application_name, grants.env_name, grants.format
		FROM grants JOIN applications ON grants.application_id = applications.application_id
		ORDER BY grants.principal, applications.application_name, grants.env_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.Principal, &g.App, &g.EnvName, &g.Format); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Lookup returns the format principal must use for envName of app, or ok=false
// when there is no such grant.
func Lookup(principal, app, envName string) (format string, ok bool, err error) {
	err = db.DB.QueryRow(`
		SELECT grants.format FROM grants
		JOIN applications ON grants.application_id = applications.application_id
		WHERE grants.principal = ? AND applications.application_name = ? AND grants.env_name = ?
	`, NormalizePrincipal(principal), app, envName).Scan(&format)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return format, err == nil, err
}
