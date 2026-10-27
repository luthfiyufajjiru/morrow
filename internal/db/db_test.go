package db

import (
	"context"
	"database/sql"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func setupDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MORROW_HOME", dir)
	if err := InitDB(filepath.Join(dir, "morrow.db")); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { DB.Close() })
}

func columns(t *testing.T, table string) map[string]bool {
	t.Helper()
	rows, err := DB.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	return cols
}

func TestEnsureSchemaEmbedded(t *testing.T) {
	setupDB(t)
	if err := EnsureSchemaEmbedded(); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := EnsureSchemaEmbedded(); err != nil {
		t.Fatalf("second apply (idempotence): %v", err)
	}

	if !columns(t, "applications")["application_working_directory"] {
		t.Error("applications.application_working_directory missing")
	}
	if !columns(t, "application_environment_variables")["env_is_secured"] {
		t.Error("application_environment_variables.env_is_secured missing")
	}

	var version int
	var dirty bool
	if err := DB.QueryRow("SELECT version, dirty FROM schema_migrations").Scan(&version, &dirty); err != nil {
		t.Fatal(err)
	}
	// The latest version is the highest NNNNNN_*.up.sql embedded in the binary.
	latest := 0
	ups, _ := fs.Glob(migrationsFS, "migrations/*.up.sql")
	for _, p := range ups {
		if n, err := strconv.Atoi(strings.SplitN(path.Base(p), "_", 2)[0]); err == nil && n > latest {
			latest = n
		}
	}
	if latest < 4 || version != latest || dirty {
		t.Errorf("schema_migrations = (%d, dirty=%v), want (%d, false)", version, dirty, latest)
	}
	for _, table := range []string{"grants", "inbox_events"} {
		if len(columns(t, table)) == 0 {
			t.Errorf("table %s missing", table)
		}
	}
}

func TestWorkingDirectoryDefault(t *testing.T) {
	setupDB(t)
	if err := EnsureSchemaEmbedded(); err != nil {
		t.Fatal(err)
	}
	_, err := DB.Exec(`INSERT INTO applications (application_id, application_name, application_executable_path, application_arguments)
		VALUES ('1','a','/bin/x','[]')`)
	if err != nil {
		t.Fatal(err)
	}
	var wd string
	if err := DB.QueryRow("SELECT application_working_directory FROM applications").Scan(&wd); err != nil {
		t.Fatal(err)
	}
	if wd != "" {
		t.Errorf("default working dir = %q, want empty", wd)
	}
}

// A writer must wait for another connection's write lock instead of failing
// immediately with SQLITE_BUSY (relay and `morrow stop` updating together).
func TestBusyTimeout(t *testing.T) {
	setupDB(t)
	if err := EnsureSchemaEmbedded(); err != nil {
		t.Fatal(err)
	}
	var timeout int
	if err := DB.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil || timeout != busyTimeoutMs {
		t.Fatalf("busy_timeout = %d, %v; want %d", timeout, err, busyTimeoutMs)
	}

	path := filepath.Join(os.Getenv("MORROW_HOME"), "morrow.db")
	other, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	conn, err := other.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = conn.ExecContext(context.Background(), "COMMIT")
	}()
	if _, err := DB.Exec(`INSERT INTO applications (application_id, application_name, application_executable_path, application_arguments)
		VALUES ('id-busy', 'busy', 'x', '[]')`); err != nil {
		t.Fatalf("write while another connection held the lock: %v", err)
	}
}
