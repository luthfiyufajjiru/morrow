package inbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"morrow/internal/app"
	"morrow/internal/config"
	"morrow/internal/crypto"
	"morrow/internal/db"
	"morrow/internal/env"
	"morrow/internal/grant"
)

const secret = "sk_test_0123456789abcdef"

// setup creates an install with app "api", inbox keys and an inbox, and makes
// every submission file appear to be written by `owner`.
func setup(t *testing.T, owner submitter) string {
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
	if err := crypto.InitInboxKeys(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(config.GetInboxDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := app.CreateApp("api", "/bin/true", nil, ""); err != nil {
		t.Fatal(err)
	}
	orig := ownerOf
	ownerOf = func(string, os.FileInfo) (submitter, error) { return owner, nil }
	t.Cleanup(func() { ownerOf = orig })
	return dir
}

func me(t *testing.T) string {
	t.Helper()
	who, err := CurrentPrincipal()
	if err != nil {
		t.Fatal(err)
	}
	return who
}

// writeSealed drops a hand-made submission, bypassing Submit's checks, as an
// attacker with inbox write access could.
func writeSealed(t *testing.T, p payload) string {
	t.Helper()
	if p.Version == 0 {
		p.Version = 1
	}
	if p.ID == "" {
		id, _ := newID()
		p.ID = id
	}
	msg, _ := json.Marshal(p)
	sealed, err := crypto.Seal(msg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(config.GetInboxDir(), p.ID+fileSuffix)
	if err := os.WriteFile(path, sealed, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func lastEvent(t *testing.T) Event {
	t.Helper()
	ev, err := Events(1)
	if err != nil || len(ev) != 1 {
		t.Fatalf("Events = %v, %v", ev, err)
	}
	return ev[0]
}

func inboxEmpty(t *testing.T) bool {
	t.Helper()
	entries, err := os.ReadDir(config.GetInboxDir())
	if err != nil {
		t.Fatal(err)
	}
	return len(entries) == 0
}

func TestSubmitApplyRoundTrip(t *testing.T) {
	setup(t, submitter{})
	who := me(t)
	ownerOf = func(string, os.FileInfo) (submitter, error) { return submitter{name: who}, nil }

	if _, err := Submit("api", []Pair{{"API_TOKEN", secret}}, false); err == nil || !strings.Contains(err.Error(), "no grant") {
		t.Fatalf("Submit without grant = %v", err)
	}
	if err := grant.Add(who, "api", []string{"API_TOKEN"}, "token"); err != nil {
		t.Fatal(err)
	}
	if _, err := Submit("api", []Pair{{"API_TOKEN", "bad value; id"}}, false); err == nil {
		t.Fatal("Submit accepted a value outside the granted format")
	}
	ids, err := Submit("api", []Pair{{"API_TOKEN", secret}}, true)
	if err != nil || len(ids) != 1 {
		t.Fatalf("Submit = %v, %v", ids, err)
	}
	sealed, _ := os.ReadFile(filepath.Join(config.GetInboxDir(), ids[0]+fileSuffix))
	if strings.Contains(string(sealed), secret) {
		t.Fatal("inbox file contains the plaintext value")
	}

	res, err := Apply()
	if err != nil || res.Applied != 1 || res.Rejected != 0 {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
	if len(res.Restarted) != 0 {
		t.Errorf("restarted a stopped app: %v", res.Restarted)
	}
	if v, sec, err := env.GetEnv("api", "API_TOKEN"); err != nil || v != secret || !sec {
		t.Fatalf("stored value = %q secured=%v err=%v", v, sec, err)
	}
	if ev := lastEvent(t); ev.Status != "applied" || ev.Principal != who || ev.ID != ids[0] {
		t.Errorf("event = %+v", ev)
	}
	if !inboxEmpty(t) {
		t.Error("processed file left in the inbox")
	}
}

func TestApplyRejections(t *testing.T) {
	setup(t, submitter{name: "ci"})
	if err := grant.Add("ci", "api", []string{"API_TOKEN"}, "token"); err != nil {
		t.Fatal(err)
	}
	if err := env.SetEnv("api", "API_TOKEN", "original-value-123", true); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		file   func() string
		reason string
	}{
		{"not granted", func() string { return writeSealed(t, payload{App: "api", Name: "DB_PASS", Value: secret}) }, "no grant"},
		{"denied name", func() string { return writeSealed(t, payload{App: "api", Name: "LD_PRELOAD", Value: "/tmp/x.so"}) }, "cannot be granted"},
		{"wrong format", func() string { return writeSealed(t, payload{App: "api", Name: "API_TOKEN", Value: "$(id)"}) }, "token format"},
		{"control chars", func() string { return writeSealed(t, payload{App: "api", Name: "API_TOKEN", Value: "abcdefgh\nX=1"}) }, "control"},
		{"other app", func() string { return writeSealed(t, payload{App: "nope", Name: "API_TOKEN", Value: secret}) }, "no grant"},
		{"tampered", func() string {
			p := writeSealed(t, payload{App: "api", Name: "API_TOKEN", Value: secret})
			b, _ := os.ReadFile(p)
			b[len(b)-1] ^= 1
			_ = os.WriteFile(p, b, 0600)
			return p
		}, "could not be decrypted"},
		{"renamed file", func() string {
			p := writeSealed(t, payload{App: "api", Name: "API_TOKEN", Value: secret})
			id, _ := newID()
			np := filepath.Join(config.GetInboxDir(), id+fileSuffix)
			_ = os.Rename(p, np)
			return np
		}, "malformed"},
		{"unsealed junk", func() string {
			p := filepath.Join(config.GetInboxDir(), "0123456789abcdef0123456789abcdef"+fileSuffix)
			_ = os.WriteFile(p, []byte(`{"v":1,"app":"api","name":"API_TOKEN","value":"x"}`), 0600)
			return p
		}, "could not be decrypted"},
		{"wrong suffix", func() string {
			p := filepath.Join(config.GetInboxDir(), "note.txt")
			_ = os.WriteFile(p, []byte("x"), 0600)
			return p
		}, "unexpected file name"},
		{"oversized", func() string {
			p := filepath.Join(config.GetInboxDir(), "fedcba9876543210fedcba9876543210"+fileSuffix)
			_ = os.WriteFile(p, make([]byte, MaxFileSize+1), 0600)
			return p
		}, "larger than"},
	}
	for _, c := range cases {
		c.file()
		res, err := Apply()
		if err != nil || res.Applied != 0 || res.Rejected != 1 {
			t.Errorf("%s: Apply = %+v, %v", c.name, res, err)
			continue
		}
		ev := lastEvent(t)
		if ev.Status != "rejected" || !strings.Contains(ev.Reason, c.reason) {
			t.Errorf("%s: event = %+v, want reason containing %q", c.name, ev, c.reason)
		}
		if !inboxEmpty(t) {
			t.Errorf("%s: rejected file left in the inbox", c.name)
		}
	}
	if v, _, _ := env.GetEnv("api", "API_TOKEN"); v != "original-value-123" {
		t.Fatalf("a rejected submission changed the value to %q", v)
	}
	if _, _, err := env.GetEnv("api", "DB_PASS"); err == nil {
		t.Error("a rejected submission created DB_PASS")
	}
	evs, _ := Events(100)
	for _, e := range evs {
		if strings.Contains(e.Reason, secret) || strings.Contains(e.Reason, "/tmp/x.so") {
			t.Errorf("event leaks a submitted value: %+v", e)
		}
	}
}

func TestApplyReplay(t *testing.T) {
	setup(t, submitter{name: "ci"})
	if err := grant.Add("ci", "api", []string{"API_TOKEN"}, "token"); err != nil {
		t.Fatal(err)
	}
	path := writeSealed(t, payload{App: "api", Name: "API_TOKEN", Value: "first-value-123", Time: time.Now().Unix()})
	copyOf, _ := os.ReadFile(path)
	if res, err := Apply(); err != nil || res.Applied != 1 {
		t.Fatalf("first Apply = %+v, %v", res, err)
	}
	if err := env.SetEnv("api", "API_TOKEN", "rotated-value-456", true); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, copyOf, 0600); err != nil {
		t.Fatal(err)
	}
	res, err := Apply()
	if err != nil || res.Rejected != 1 {
		t.Fatalf("replayed Apply = %+v, %v", res, err)
	}
	if ev := lastEvent(t); !strings.Contains(ev.Reason, "replay") {
		t.Errorf("event = %+v", ev)
	}
	if v, _, _ := env.GetEnv("api", "API_TOKEN"); v != "rotated-value-456" {
		t.Fatalf("replay rolled the value back to %q", v)
	}
}

func TestApplyPrivilegedOwner(t *testing.T) {
	setup(t, submitter{name: "root", privileged: true})
	// Administrators can set any name, as with set-env, but universal value rules still apply.
	writeSealed(t, payload{App: "api", Name: "SOME_SETTING", Value: "free text, any format"})
	if res, err := Apply(); err != nil || res.Applied != 1 {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
	writeSealed(t, payload{App: "api", Name: "SOME_SETTING", Value: "two\nlines"})
	if res, err := Apply(); err != nil || res.Rejected != 1 {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
}

func TestApplySymlinkRejected(t *testing.T) {
	setup(t, submitter{name: "ci"})
	target := filepath.Join(t.TempDir(), "target")
	_ = os.WriteFile(target, []byte("x"), 0600)
	link := filepath.Join(config.GetInboxDir(), "00000000000000000000000000000001"+fileSuffix)
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	res, err := Apply()
	if err != nil || res.Rejected != 1 {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
	if ev := lastEvent(t); !strings.Contains(ev.Reason, "regular file") {
		t.Errorf("event = %+v", ev)
	}
	if _, err := os.Stat(target); err != nil {
		t.Error("apply removed the symlink target instead of the link")
	}
}

func TestApplyWithoutInbox(t *testing.T) {
	setup(t, submitter{})
	if err := os.RemoveAll(config.GetInboxDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(); err != crypto.ErrInboxDisabled {
		t.Fatalf("Apply = %v, want ErrInboxDisabled", err)
	}
}

func TestApplyLock(t *testing.T) {
	setup(t, submitter{})
	unlock, err := Lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(); err == nil || !strings.Contains(err.Error(), "another 'morrow apply'") {
		t.Fatalf("concurrent Apply = %v", err)
	}
	unlock()
	if _, err := Apply(); err != nil {
		t.Fatalf("Apply after unlock = %v", err)
	}
}
