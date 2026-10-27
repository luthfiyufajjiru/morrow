package privilege

import (
	"strings"
	"testing"
)

func TestInboxSDDL(t *testing.T) {
	sddl := InboxSDDL("S-1-5-21-1-2-3-1001")
	if !strings.HasPrefix(sddl, "D:P") {
		t.Errorf("inbox DACL must be protected (no inherited Users read): %s", sddl)
	}
	// The deploy group may only add files (0x2) + synchronize, never list (0x1) or read.
	if !strings.Contains(sddl, "(A;;0x100002;;;S-1-5-21-1-2-3-1001)") {
		t.Errorf("deploy group ACE: %s", sddl)
	}
	if strings.Contains(sddl, ";BU)") || strings.Contains(sddl, ";WD)") || strings.Contains(sddl, ";AU)") {
		t.Errorf("Users/Everyone must have no access: %s", sddl)
	}
}
