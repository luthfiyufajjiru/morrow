package complete

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"morrow/internal/db"
)

func setup(t *testing.T, withDB bool) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MORROW_HOME", dir)
	db.DB = nil
	t.Cleanup(func() {
		if db.DB != nil {
			db.DB.Close()
			db.DB = nil
		}
	})
	if !withDB {
		return
	}
	if err := db.InitDB(filepath.Join(dir, "morrow.db")); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureSchemaEmbedded(); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"web", "api"} {
		if _, err := db.DB.Exec(`INSERT INTO applications (application_id, application_name, application_executable_path, application_arguments)
			VALUES (?, ?, 'x', '[]')`, "id-"+a, a); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []string{"LOG_LEVEL", "API_TOKEN"} {
		if _, err := db.DB.Exec(`INSERT INTO application_environment_variables (application_id, env_name, env_value, env_is_secured)
			VALUES ('id-api', ?, 'secret-value', 1)`, n); err != nil {
			t.Fatal(err)
		}
	}
	// Completion opens the database itself; start from a closed handle.
	db.DB.Close()
	db.DB = nil
}

func TestCandidates(t *testing.T) {
	setup(t, true)
	cases := []struct {
		name string
		fn   cobra.CompletionFunc
		args []string
		tc   string
		want []string
		dir  cobra.ShellCompDirective
	}{
		{"app, sorted", App, nil, "", []string{"api", "web"}, cobra.ShellCompDirectiveNoFileComp},
		{"app prefix", App, nil, "w", []string{"web"}, cobra.ShellCompDirectiveNoFileComp},
		{"nothing after app", App, []string{"api"}, "", nil, cobra.ShellCompDirectiveNoFileComp},
		{"env names", AppThenEnv, []string{"api"}, "", []string{"API_TOKEN", "LOG_LEVEL"}, cobra.ShellCompDirectiveNoFileComp},
		{"env names, other app", AppThenEnv, []string{"web"}, "", nil, cobra.ShellCompDirectiveNoFileComp},
		{"assignments", AppThenAssignments, []string{"api"}, "L", []string{"LOG_LEVEL="}, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace},
		{"inside a value", AppThenAssignments, []string{"api"}, "LOG_LEVEL=", nil, cobra.ShellCompDirectiveNoFileComp},
		{"update: files after app", AppThenFiles, []string{"api"}, "", nil, cobra.ShellCompDirectiveDefault},
		{"create: no name suggestions", NewAppThenFiles, nil, "", nil, cobra.ShellCompDirectiveNoFileComp},
		{"--app flag", AppFlag, nil, "a", []string{"api"}, cobra.ShellCompDirectiveNoFileComp},
	}
	for _, c := range cases {
		got, dir := c.fn(nil, c.args, c.tc)
		if !reflect.DeepEqual(got, c.want) || dir != c.dir {
			t.Errorf("%s: got %v (%v), want %v (%v)", c.name, got, dir, c.want, c.dir)
		}
	}
}

func TestNeverReturnsValues(t *testing.T) {
	setup(t, true)
	for _, fn := range []cobra.CompletionFunc{AppThenEnv, AppThenAssignments} {
		got, _ := fn(nil, []string{"api"}, "")
		for _, s := range got {
			if strings.Contains(s, "secret-value") {
				t.Errorf("candidate %q leaks a value", s)
			}
		}
	}
}

func TestUninitializedIsSilent(t *testing.T) {
	setup(t, false)
	if got, dir := App(nil, nil, ""); got != nil || dir != cobra.ShellCompDirectiveNoFileComp {
		t.Errorf("got %v (%v), want no candidates", got, dir)
	}
	if db.DB != nil {
		t.Error("completion created a database before init")
	}
}
