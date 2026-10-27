// Package complete provides shell-completion candidates (app and variable
// names). It reads the database read-only and stays silent on any error, so
// pressing Tab never prints errors and works for regular users too.
package complete

import (
	"morrow/internal/config"
	"morrow/internal/db"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// open makes sure the database is open. Completion runs without the root
// command's pre-run (which would print notes or exit when uninitialized).
func open() bool {
	if db.DB != nil {
		return true
	}
	if _, err := os.Stat(config.GetDBPath()); err != nil {
		return false
	}
	return db.InitDB(config.GetDBPath()) == nil
}

func query(q string, args ...any) []string {
	if !open() {
		return nil
	}
	rows, err := db.DB.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			out = append(out, s)
		}
	}
	return out
}

// AppNames returns all application names.
func AppNames() []string {
	return query("SELECT application_name FROM applications ORDER BY application_name")
}

// EnvNames returns the variable names of an application (never values).
func EnvNames(app string) []string {
	return query(`
		SELECT env_name FROM application_environment_variables
		JOIN applications ON application_environment_variables.application_id = applications.application_id
		WHERE applications.application_name = ? ORDER BY env_name
	`, app)
}

func filter(list []string, prefix string) []string {
	var out []string
	for _, s := range list {
		if strings.HasPrefix(s, prefix) {
			out = append(out, s)
		}
	}
	return out
}

// App completes the first argument with an app name and nothing after it.
func App(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return filter(AppNames(), toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// AppThenEnv completes an app name, then one of its variable names.
func AppThenEnv(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return filter(AppNames(), toComplete), cobra.ShellCompDirectiveNoFileComp
	case 1:
		return filter(EnvNames(args[0]), toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// AppThenAssignments completes an app name, then "NAME=" for its existing
// variables (any number of them), leaving the cursor after "=".
func AppThenAssignments(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return filter(AppNames(), toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	if strings.Contains(toComplete, "=") {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, n := range filter(EnvNames(args[0]), toComplete) {
		out = append(out, n+"=")
	}
	return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
}

// AppThenFiles completes an app name, then file paths (the executable and
// its arguments).
func AppThenFiles(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return filter(AppNames(), toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveDefault
}

// NewAppThenFiles completes nothing for the new app's name, then file paths.
func NewAppThenFiles(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveDefault
}

// AppFlag completes an --app flag value.
func AppFlag(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return filter(AppNames(), toComplete), cobra.ShellCompDirectiveNoFileComp
}

// None disables file completion for commands that take no arguments.
func None(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveNoFileComp
}
