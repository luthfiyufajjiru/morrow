package inbox

import (
	"strings"
	"testing"
)

func TestSystemdUnits(t *testing.T) {
	path, svc := SystemdUnits("/usr/local/bin/morrow", "/var/lib/morrow/inbox")
	if !strings.Contains(path, "DirectoryNotEmpty=/var/lib/morrow/inbox") || !strings.Contains(path, "WantedBy=multi-user.target") {
		t.Errorf("path unit:\n%s", path)
	}
	if !strings.Contains(svc, "Type=oneshot") || !strings.Contains(svc, "ExecStart=/usr/local/bin/morrow apply") {
		t.Errorf("service unit:\n%s", svc)
	}
}
