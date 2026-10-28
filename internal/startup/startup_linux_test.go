package startup

import (
	"strings"
	"testing"
)

func TestUnit(t *testing.T) {
	u := Unit("/usr/local/bin/morrow", nil)
	for _, want := range []string{
		"ExecStart=/usr/local/bin/morrow _boot",
		"ExecStop=/usr/local/bin/morrow _shutdown",
		"Type=oneshot", "RemainAfterExit=yes", // stays active so ExecStop runs at shutdown
		"KillMode=process", // systemd must not kill the apps itself
		"Wants=network-online.target", "After=network-online.target", "WantedBy=multi-user.target",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit missing %q:\n%s", want, u)
		}
	}
}

func TestUnitAfter(t *testing.T) {
	after, err := Units([]string{"postgresql.service", "redis-server", "local-fs.target"})
	if err != nil {
		t.Fatal(err)
	}
	u := Unit("/usr/local/bin/morrow", after)
	for _, want := range []string{
		"Wants=postgresql.service\nAfter=postgresql.service\n",
		"Wants=redis-server.service\nAfter=redis-server.service\n", // a bare name means a service
		"Wants=local-fs.target\nAfter=local-fs.target\n",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("unit missing %q:\n%s", want, u)
		}
	}
	// The ordering belongs to [Unit], not [Service].
	if i, j := strings.Index(u, "After=postgresql.service"), strings.Index(u, "[Service]"); i > j {
		t.Errorf("After= is not in [Unit]:\n%s", u)
	}
}

func TestUnitsRejectsInvalid(t *testing.T) {
	for _, bad := range []string{
		"",
		"postgresql.service\nExecStartPre=/bin/sh -c id", // would inject a directive
		"postgresql.service ExecStart=x",
		"../etc/passwd.service",
		"postgresql.bogus",
	} {
		if _, err := Units([]string{bad}); err == nil {
			t.Errorf("Units(%q) accepted an invalid unit name", bad)
		}
	}
	if got, err := Units([]string{"postgresql@15-main.service"}); err != nil || got[0] != "postgresql@15-main.service" {
		t.Errorf("template instance rejected: %v %v", got, err)
	}
}
