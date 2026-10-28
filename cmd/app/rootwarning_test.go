package app

import (
	"strings"
	"testing"
)

func TestRootWarning(t *testing.T) {
	if w := rootWarning("api", "", true, "linux"); !strings.Contains(w, "api runs as root") || !strings.Contains(w, "sudo morrow update api --user <account>") {
		t.Errorf("linux warning = %q", w)
	}
	if w := rootWarning("api", "", true, "windows"); !strings.Contains(w, "Administrator") || !strings.Contains(w, "--password-env") {
		t.Errorf("windows warning = %q", w)
	}
	if w := rootWarning("api", "svc-api", true, "linux"); w != "" {
		t.Errorf("warned for an app with --user: %q", w)
	}
	if w := rootWarning("api", "", false, "linux"); w != "" {
		t.Errorf("warned when not elevated (per-user install): %q", w)
	}
}
