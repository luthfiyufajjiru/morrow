package app

import (
	"os/user"
	"reflect"
	"strings"
	"testing"
)

func TestSplitAccount(t *testing.T) {
	for in, want := range map[string][2]string{
		`HOST\svc`: {"HOST", "svc"}, "svc": {".", "svc"}, "svc@corp.example": {"", "svc@corp.example"},
	} {
		if d, n := splitAccount(in); d != want[0] || n != want[1] {
			t.Errorf("splitAccount(%q) = %q, %q", in, d, n)
		}
	}
}

func TestMergeEnv(t *testing.T) {
	got := mergeEnv([]string{`Path=C:\Windows`, `=C:=C:\`, `USERPROFILE=C:\Users\svc`, `path=C:\override`, "A=1"})
	want := []string{"A=1", `path=C:\override`, `USERPROFILE=C:\Users\svc`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("mergeEnv = %v, want %v (last value wins, case-insensitive, sorted, no drive entries)", got, want)
	}
	b := envBlock([]string{"A=1", "B=2"})
	if len(b) != len("A=1")+1+len("B=2")+1+1 || b[len(b)-1] != 0 || b[len(b)-2] != 0 {
		t.Errorf("envBlock is not double-NUL terminated: %v", b)
	}
}

func TestRunAsWindows(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if err := SetRunAs("api", me.Username, ""); err == nil || !strings.Contains(err.Error(), "password") {
		t.Errorf("missing password accepted: %v", err)
	}
	if err := SetRunAs("api", me.Username, "P@ss-word"); err != nil {
		t.Fatal(err)
	}
	if s := storedSecret(t); s == "" || strings.Contains(s, "P@ss-word") {
		t.Errorf("password stored as %q, want encrypted", s)
	}
	if p, err := runAsSecret("api"); err != nil || p != "P@ss-word" {
		t.Errorf("runAsSecret = %q, %v", p, err)
	}
}
