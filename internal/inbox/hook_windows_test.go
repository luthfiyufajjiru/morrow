package inbox

import (
	"strings"
	"testing"
)

func TestTaskArgs(t *testing.T) {
	args := strings.Join(TaskArgs(`C:\Program Files\Morrow\morrow.exe`), " ")
	for _, want := range []string{"/RU SYSTEM", "/SC MINUTE /MO 1", `/TR "C:\Program Files\Morrow\morrow.exe" apply`, `/TN Morrow\Inbox`, "/F"} {
		if !strings.Contains(args, want) {
			t.Errorf("schtasks args missing %q: %s", want, args)
		}
	}
}
