package grant

import (
	"path/filepath"
	"strings"
	"testing"

	"morrow/internal/db"
)

func setupDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MORROW_HOME", dir)
	if err := db.InitDB(filepath.Join(dir, "morrow.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.DB.Close() })
	if err := db.EnsureSchemaEmbedded(); err != nil {
		t.Fatalf("EnsureSchemaEmbedded: %v", err)
	}
	for _, a := range []string{"api", "web"} {
		if _, err := db.DB.Exec(`INSERT INTO applications (application_id, application_name, application_executable_path, application_arguments)
			VALUES (?, ?, 'x', '[]')`, "id-"+a, a); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAddLookupListRemove(t *testing.T) {
	setupDB(t)
	if err := Add("ci", "api", []string{"API_TOKEN", "DB_PASS"}, "token"); err != nil {
		t.Fatal(err)
	}
	if err := Add("ci", "api", []string{"LOG_LEVEL"}, "enum:info|warn"); err != nil {
		t.Fatal(err)
	}
	// Re-granting updates the format instead of duplicating.
	if err := Add("ci", "api", []string{"DB_PASS"}, "any"); err != nil {
		t.Fatal(err)
	}

	if f, ok, err := Lookup("ci", "api", "DB_PASS"); err != nil || !ok || f != "any" {
		t.Errorf("Lookup DB_PASS = %q, %v, %v", f, ok, err)
	}
	for _, c := range []struct{ who, app, name string }{
		{"ci", "web", "API_TOKEN"}, // other app
		{"other", "api", "API_TOKEN"},
		{"ci", "api", "NOT_GRANTED"},
	} {
		if _, ok, err := Lookup(c.who, c.app, c.name); err != nil || ok {
			t.Errorf("Lookup(%v) should not be granted (ok=%v, err=%v)", c, ok, err)
		}
	}

	gs, err := List()
	if err != nil || len(gs) != 3 {
		t.Fatalf("List = %v, %v", gs, err)
	}

	if n, err := Remove("ci", "api", []string{"LOG_LEVEL"}); err != nil || n != 1 {
		t.Errorf("Remove one = %d, %v", n, err)
	}
	if n, err := Remove("ci", "api", nil); err != nil || n != 2 {
		t.Errorf("Remove all = %d, %v", n, err)
	}
	if gs, _ := List(); len(gs) != 0 {
		t.Errorf("grants left: %v", gs)
	}
}

func TestAddRejects(t *testing.T) {
	setupDB(t)
	cases := []struct {
		app    string
		vars   []string
		format string
		want   string
	}{
		{"api", []string{"LD_PRELOAD"}, "token", "cannot be granted"},
		{"api", []string{"OK", "NODE_OPTIONS"}, "token", "cannot be granted"},
		{"api", []string{"OK"}, "bogus", "unknown format"},
		{"api", nil, "token", "no variables"},
		{"nope", []string{"OK"}, "token", "not found"},
	}
	for _, c := range cases {
		err := Add("ci", c.app, c.vars, c.format)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("Add(%v, %v, %q) = %v, want %q", c.app, c.vars, c.format, err, c.want)
		}
	}
	if gs, _ := List(); len(gs) != 0 {
		t.Errorf("a rejected Add left grants behind: %v", gs)
	}
}
