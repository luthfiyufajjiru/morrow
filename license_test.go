package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// rtfControl matches RTF control words (\par, \fs22, ...) and group braces.
var rtfControl = regexp.MustCompile(`\\[a-z]+-?[0-9]* ?|[{}]`)

func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n") // Windows checkouts use CRLF
}

// The repository and the MSI installer must carry the same license, and the
// readme must point to it.
func TestLicense(t *testing.T) {
	text := readText(t, "LICENSE")
	if !strings.HasPrefix(text, "MIT License\n") || !strings.Contains(text, "Copyright (c) 2026 Luthfi Yufajjiru") {
		t.Errorf("LICENSE is not the expected MIT license:\n%s", text)
	}

	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	plain := norm(rtfControl.ReplaceAllString(readText(t, "installer/license.rtf"), " "))
	if !strings.Contains(plain, norm(text)) {
		t.Errorf("installer/license.rtf does not contain the LICENSE text; it reads:\n%s", plain)
	}

	if !strings.Contains(readText(t, "readme.md"), "[MIT License](LICENSE)") {
		t.Error("readme does not reference LICENSE")
	}
}
