package startup

import (
	"strings"
	"testing"
)

func TestTaskArgs(t *testing.T) {
	args := strings.Join(TaskArgs(`C:\Program Files\Morrow\morrow.exe`), " ")
	for _, want := range []string{"/SC ONSTART", "/RU SYSTEM", `/TN Morrow\Startup`, `/TR "C:\Program Files\Morrow\morrow.exe" _boot`, "/F"} {
		if !strings.Contains(args, want) {
			t.Errorf("schtasks args missing %q: %s", want, args)
		}
	}
}
