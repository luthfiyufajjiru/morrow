package app

import (
	"strings"
	"testing"

	"morrow/internal/crypto"
	"morrow/internal/db"
)

func TestSetRunAsErrors(t *testing.T) {
	setupDB(t)
	mustCreate(t, "api", nil, "")
	if err := SetRunAs("api", "no-such-account-morrow-test", "pw"); err == nil || !strings.Contains(err.Error(), "no-such-account") {
		t.Errorf("unknown account accepted: %v", err)
	}
	if err := SetRunAs("nope", "", ""); err == nil {
		t.Error("unknown app accepted")
	}
	if err := SetRunAs("api", "", ""); err != nil {
		t.Errorf("clearing: %v", err)
	}
	if d, _ := GetAppDetail("api"); d.RunAs != "" {
		t.Errorf("RunAs = %q after clearing", d.RunAs)
	}
}

func storedSecret(t *testing.T) string {
	t.Helper()
	var s string
	if err := db.DB.QueryRow("SELECT application_run_as_secret FROM applications WHERE application_name = 'api'").Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

var _ = crypto.Encrypt // keep the import for OS-specific tests
