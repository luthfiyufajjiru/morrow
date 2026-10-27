package hooks

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"morrow/internal/crypto"
	"morrow/internal/db"
)

func setup(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MORROW_HOME", dir)
	t.Setenv("MORROW_KEY_PATH", "")
	if err := db.InitDB(filepath.Join(dir, "morrow.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB.Close() })
	if err := db.EnsureSchemaEmbedded(); err != nil {
		t.Fatal(err)
	}
	if err := crypto.InitMasterKey(); err != nil {
		t.Fatal(err)
	}
	for _, a := range []string{"api", "web"} {
		if _, err := db.DB.Exec(`INSERT INTO applications (application_id, application_name, application_executable_path, application_arguments)
			VALUES (?, ?, 'x', '[]')`, "id-"+a, a); err != nil {
			t.Fatal(err)
		}
	}
}

func TestValidation(t *testing.T) {
	if evs, err := ParseEvents([]string{"Crash", " giveup ", ""}); err != nil || strings.Join(evs, ",") != "crash,giveup" {
		t.Errorf("ParseEvents = %v, %v", evs, err)
	}
	for _, bad := range [][]string{{"boom"}, {}, {""}} {
		if _, err := ParseEvents(bad); err == nil {
			t.Errorf("ParseEvents(%v) accepted", bad)
		}
	}
	for _, c := range []struct {
		kind, target string
		ok           bool
	}{
		{KindExec, "echo hi", true}, {KindExec, "  ", false},
		{KindWebhook, "https://example.com/hook", true}, {KindWebhook, "http://10.0.0.1:8080/x", true},
		{KindWebhook, "ftp://example.com", false}, {KindWebhook, "example.com/hook", false},
		{"email", "x", false},
	} {
		if err := CheckTarget(c.kind, c.target); (err == nil) != c.ok {
			t.Errorf("CheckTarget(%s, %q) = %v", c.kind, c.target, err)
		}
	}
}

func TestAddListRemove(t *testing.T) {
	setup(t)
	secretURL := "https://hooks.example.com/services/T0/B0/SECRET-TOKEN"
	id, err := Add("api", []string{"crash"}, KindWebhook, secretURL)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	_ = db.DB.QueryRow("SELECT hook_target FROM hooks WHERE hook_id = ?", id).Scan(&stored)
	if strings.Contains(stored, "SECRET-TOKEN") {
		t.Fatal("hook target stored in plain text")
	}
	if _, err := Add("web", []string{"giveup"}, KindExec, "true"); err != nil {
		t.Fatal(err)
	}
	if _, err := Add("nope", []string{"crash"}, KindExec, "true"); err == nil {
		t.Error("hook added to an unknown app")
	}

	hs, _ := List("api")
	if len(hs) != 1 || hs[0].Target != secretURL || hs[0].Kind != KindWebhook || hs[0].Events[0] != "crash" {
		t.Fatalf("List(api) = %+v", hs)
	}
	if all, _ := List(""); len(all) != 2 {
		t.Errorf("List() = %d hooks, want 2", len(all))
	}

	// Without the key (a regular user) targets are masked.
	t.Setenv("MORROW_KEY_PATH", filepath.Join(t.TempDir(), "missing.key"))
	if hs, _ := List("api"); hs[0].Target != "****" {
		t.Errorf("target visible without the key: %q", hs[0].Target)
	}

	if err := Remove("api", id); err != nil {
		t.Fatal(err)
	}
	if err := Remove("api", id); err == nil {
		t.Error("removing a missing hook succeeded")
	}
}

func TestExecHookGetsEventVariables(t *testing.T) {
	setup(t)
	out := filepath.Join(t.TempDir(), "event.txt")
	command := `echo "$MORROW_EVENT $MORROW_APP_NAME $MORROW_EXIT_CODE $MORROW_EXIT_SIGNAL $MORROW_RESTART_COUNT" > '` + out + `'`
	if runtime.GOOS == "windows" {
		command = `echo %MORROW_EVENT% %MORROW_APP_NAME% %MORROW_EXIT_CODE% %MORROW_RESTART_COUNT% > "` + out + `"`
	}
	if _, err := Add("api", []string{"crash"}, KindExec, command); err != nil {
		t.Fatal(err)
	}
	var lines []string
	Fire(Event{Name: EventCrash, App: "api", AppID: "id-api", ExitCode: 3, Signal: "", RestartCount: 2, Time: time.Now()},
		func(s string) { lines = append(lines, s) })
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("hook did not run: %v (log %v)", err, lines)
	}
	if !strings.Contains(string(got), "crash api 3") || !strings.Contains(string(got), "2") {
		t.Errorf("hook saw %q", got)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "ran") {
		t.Errorf("log = %v", lines)
	}
}

func TestWebhook(t *testing.T) {
	setup(t)
	var got Event
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.Contains(r.URL.Path, "fail") {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	if _, err := Add("api", []string{"crash", "giveup"}, KindWebhook, srv.URL+"/ok/TOKEN123"); err != nil {
		t.Fatal(err)
	}
	if _, err := Add("api", []string{"exit"}, KindWebhook, srv.URL+"/never"); err != nil {
		t.Fatal(err)
	}
	var lines []string
	Fire(Event{Name: EventGiveUp, App: "api", AppID: "id-api", ExitCode: 1, RestartCount: 5, Time: time.Now()},
		func(s string) { lines = append(lines, s) })
	if calls != 1 {
		t.Fatalf("webhook calls = %d, want 1 (only the hook subscribed to giveup)", calls)
	}
	if got.Name != EventGiveUp || got.App != "api" || got.RestartCount != 5 || got.Host == "" {
		t.Errorf("payload = %+v", got)
	}

	failing := Hook{ID: "x", Kind: KindWebhook, Target: srv.URL + "/fail/TOKEN456"}
	err := Run(failing, Event{Name: EventCrash, App: "api"})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("failing webhook: %v", err)
	}
	if strings.Contains(err.Error(), "TOKEN456") {
		t.Errorf("error leaks the webhook URL: %v", err)
	}
	unreachable := Hook{ID: "y", Kind: KindWebhook, Target: "http://127.0.0.1:1/TOKEN789"}
	if err := Run(unreachable, Event{Name: EventCrash}); err == nil || strings.Contains(err.Error(), "TOKEN789") {
		t.Errorf("unreachable webhook error = %v (must fail without leaking the URL)", err)
	}
}

func TestFireSkipsUndecryptableHooks(t *testing.T) {
	setup(t)
	if _, err := Add("api", []string{"crash"}, KindExec, "true"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MORROW_KEY_PATH", filepath.Join(t.TempDir(), "missing.key"))
	var lines []string
	Fire(Event{Name: EventCrash, App: "api"}, func(s string) { lines = append(lines, s) })
	if len(lines) != 1 || !strings.Contains(lines[0], "cannot decrypt") {
		t.Errorf("log = %v", lines)
	}
}
