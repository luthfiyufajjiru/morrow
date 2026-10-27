package grant

import (
	"strings"
	"testing"
)

func TestCheckName(t *testing.T) {
	cases := []struct {
		name string
		ok   bool
	}{
		{"API_TOKEN", true}, {"db_pass", true}, {"_X1", true}, {"REDIS_URL", true},
		{"", false}, {"1ABC", false}, {"A-B", false}, {"A B", false}, {"A=B", false}, {strings.Repeat("A", 129), false},
		// loaders, interpreters and shells: denied even if an admin tries to grant them
		{"PATH", false}, {"path", false}, {"LD_PRELOAD", false}, {"ld_library_path", false}, {"DYLD_INSERT_LIBRARIES", false},
		{"NODE_OPTIONS", false}, {"PYTHONPATH", false}, {"PYTHONSTARTUP", false}, {"PERL5OPT", false}, {"RUBYOPT", false},
		{"JAVA_TOOL_OPTIONS", false}, {"_JAVA_OPTIONS", false}, {"BASH_ENV", false}, {"IFS", false}, {"GODEBUG", false},
		{"DOTNET_STARTUP_HOOKS", false}, {"COMSPEC", false}, {"MORROW_HOME", false}, {"GCONV_PATH", false},
	}
	for _, c := range cases {
		if err := CheckName(c.name); (err == nil) != c.ok {
			t.Errorf("CheckName(%q) = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestCheckFormat(t *testing.T) {
	for _, f := range []string{"token", "int", "bool", "url", "any", "enum:a|b", "regex:[a-z]+"} {
		if err := CheckFormat(f); err != nil {
			t.Errorf("CheckFormat(%q) = %v", f, err)
		}
	}
	for _, f := range []string{"", "string", "enum:", "enum:a||b", "regex:(", "TOKEN"} {
		if err := CheckFormat(f); err == nil {
			t.Errorf("CheckFormat(%q) accepted", f)
		}
	}
}

func TestCheckValue(t *testing.T) {
	cases := []struct {
		format, value string
		ok            bool
	}{
		{"token", "sk_live_abcdef123456", true},
		{"token", "eyJhbGciOi.eyJzdWIi.SflKxw-_~+/=", true},
		// injection-shaped values are not tokens
		{"token", "x; rm -rf /", false},
		{"token", "$(id)", false},
		{"token", "`id`", false},
		{"token", "a b c d e f g h", false},
		{"token", "'quoted-token'", false},
		{"token", "short", false},
		{"token", strings.Repeat("a", 513), false},
		{"int", "8080", true}, {"int", "-1", true}, {"int", "1e3", false}, {"int", "80; id", false},
		{"bool", "true", true}, {"bool", "0", true}, {"bool", "yes", false},
		{"url", "https://api.example.com/v1", true},
		{"url", "http://api.example.com", false},
		{"url", "https://user:pass@example.com", false},
		{"url", "file:///etc/passwd", false},
		{"url", "javascript:alert(1)", false},
		{"enum:info|warn|error", "warn", true}, {"enum:info|warn|error", "debug", false},
		{"regex:[0-9]{4}", "2026", true}, {"regex:[0-9]{4}", "20260", false}, // anchored
		{"any", "anything goes; even $(this)", true},
		// universal rules apply to every format, including any
		{"any", "line1\nline2", false},
		{"any", "nul\x00byte", false},
		{"any", "esc\x1b[31m", false},
		{"any", "\xff\xfe", false},
		{"any", strings.Repeat("a", MaxValueLen+1), false},
	}
	for _, c := range cases {
		if err := CheckValue(c.value, c.format); (err == nil) != c.ok {
			t.Errorf("CheckValue(%q, %q) = %v, want ok=%v", c.value, c.format, err, c.ok)
		}
	}
}
