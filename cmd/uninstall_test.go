package cmd

import (
	"strings"
	"testing"
)

func TestConfirm(t *testing.T) {
	cases := []struct {
		input string
		purge bool
		want  bool
	}{
		{"y\n", false, true},
		{"YES\n", false, true},
		{"n\n", false, false},
		{"\n", false, false},
		{"", false, false}, // no input (non-interactive) means no
		{"purge\n", true, true},
		{"y\n", true, false}, // purging needs the word typed out
		{"PURGE\n", true, false},
		{"", true, false},
	}
	for _, c := range cases {
		if got := confirm(strings.NewReader(c.input), c.purge); got != c.want {
			t.Errorf("confirm(%q, purge=%v) = %v, want %v", c.input, c.purge, got, c.want)
		}
	}
}
