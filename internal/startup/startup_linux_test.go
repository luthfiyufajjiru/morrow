package startup

import (
	"strings"
	"testing"
)

func TestUnit(t *testing.T) {
	u := Unit("/usr/local/bin/morrow")
	for _, want := range []string{
		"ExecStart=/usr/local/bin/morrow _boot",
		"ExecStop=/usr/local/bin/morrow _shutdown",
		"Type=oneshot", "RemainAfterExit=yes", // stays active so ExecStop runs at shutdown
		"KillMode=process", // systemd must not kill the apps itself
		"After=network-online.target", "WantedBy=multi-user.target",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit missing %q:\n%s", want, u)
		}
	}
}
