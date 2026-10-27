package cmd

import (
	"strings"
	"testing"

	"morrow/internal/env"
)

func TestCheckKeyLoss(t *testing.T) {
	secured := []env.SecuredRef{{App: "api", Name: "API_TOKEN"}, {App: "web", Name: "DB_PASS"}}
	cases := []struct {
		name    string
		secured []env.SecuredRef
		force   bool
		wantErr bool
	}{
		{"fresh install", nil, false, false},
		{"secured values, no force", secured, false, true},
		{"secured values, force", secured, true, false},
	}
	for _, c := range cases {
		err := checkKeyLoss(c.secured, c.force, "/opt/morrow/.morrow.key")
		if (err != nil) != c.wantErr {
			t.Fatalf("%s: err = %v, wantErr %v", c.name, err, c.wantErr)
		}
		if err == nil {
			continue
		}
		for _, want := range []string{"/opt/morrow/.morrow.key", "2 secured", "api: API_TOKEN", "web: DB_PASS", "Restore the original key", "--force"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: message missing %q:\n%v", c.name, want, err)
			}
		}
	}
}

func TestInitForceFlag(t *testing.T) {
	f := InitCmd.Flags().Lookup("force")
	if f == nil || f.DefValue != "false" {
		t.Fatalf("init --force flag missing or not default false: %+v", f)
	}
}
