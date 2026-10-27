// Package grant decides which variables a non-admin principal may submit
// through the inbox, and which values are acceptable.
//
// Apps on a system install run as root/Administrator, so any variable a
// deployer can set is a potential way to run code as root. Validation is
// layered: an explicit per-app name allowlist (the grant), a built-in name
// denylist that applies even to granted names, a per-grant value format, and
// universal value rules. None of this can make every value safe for every
// app; running apps as a dedicated user is the only complete fix.
package grant

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MaxValueLen is the largest value accepted through the inbox.
const MaxValueLen = 4096

var namePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// deniedNames are variables that make loaders, interpreters or shells run
// code or load libraries chosen by whoever sets them. Matched case-insensitively.
var deniedNames = map[string]bool{
	"PATH": true, "IFS": true, "ENV": true, "BASH_ENV": true, "SHELLOPTS": true, "PS4": true,
	"NODE_OPTIONS": true, "NODE_PATH": true,
	"JAVA_TOOL_OPTIONS": true, "_JAVA_OPTIONS": true, "JDK_JAVA_OPTIONS": true, "CLASSPATH": true,
	"GODEBUG": true, "GOFLAGS": true,
	"DOTNET_STARTUP_HOOKS": true, "COR_PROFILER": true, "CORECLR_PROFILER": true, "COR_ENABLE_PROFILING": true,
	"COMSPEC": true, "PATHEXT": true, "PSMODULEPATH": true,
	"HOME": true, "USERPROFILE": true, "TMPDIR": true, "TEMP": true, "TMP": true,
	"GIT_SSH": true, "GIT_SSH_COMMAND": true, "EDITOR": true, "VISUAL": true, "PAGER": true,
	"OPENSSL_CONF": true, "SSL_CERT_FILE": true, "SSL_CERT_DIR": true,
}

// deniedPrefixes cover families of loader and interpreter hooks.
var deniedPrefixes = []string{
	"LD_", "DYLD_", "PYTHON", "PERL", "RUBY", "GEM_", "BUNDLE_", "LUA_", "PHP", "R_",
	"MORROW_", "GCONV_", "MALLOC_", "GLIBC_",
}

// CheckName rejects names that are malformed or on the denylist.
func CheckName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%q is not a valid variable name", name)
	}
	upper := strings.ToUpper(name)
	if deniedNames[upper] {
		return fmt.Errorf("%s can make programs run other code, so it cannot be granted", name)
	}
	for _, p := range deniedPrefixes {
		if strings.HasPrefix(upper, p) {
			return fmt.Errorf("%s* variables can make programs run other code, so %s cannot be granted", p, name)
		}
	}
	return nil
}

var (
	tokenPattern = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]{8,512}$`)
	intPattern   = regexp.MustCompile(`^-?[0-9]{1,19}$`)
)

// CheckFormat validates a format specification: token, int, bool, url,
// enum:a|b|c, regex:<RE2 pattern> or any.
func CheckFormat(format string) error {
	switch {
	case format == "token" || format == "int" || format == "bool" || format == "url" || format == "any":
		return nil
	case strings.HasPrefix(format, "enum:"):
		for _, v := range strings.Split(strings.TrimPrefix(format, "enum:"), "|") {
			if v == "" {
				return fmt.Errorf("enum format has an empty choice: %q", format)
			}
		}
		return nil
	case strings.HasPrefix(format, "regex:"):
		if _, err := regexp.Compile(strings.TrimPrefix(format, "regex:")); err != nil {
			return fmt.Errorf("invalid regex format: %w", err)
		}
		return nil
	}
	return fmt.Errorf("unknown format %q (use token, int, bool, url, enum:a|b, regex:<pattern> or any)", format)
}

// CheckValue applies the universal value rules and then the grant's format.
func CheckValue(value, format string) error {
	if len(value) > MaxValueLen {
		return fmt.Errorf("value is longer than %d bytes", MaxValueLen)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("value is not valid UTF-8")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("value contains control characters (newline, NUL, ...)")
		}
	}
	switch {
	case format == "any":
		return nil
	case format == "token":
		if !tokenPattern.MatchString(value) {
			return fmt.Errorf("value does not match the token format: 8-512 characters of A-Z a-z 0-9 . _ ~ + / = -")
		}
	case format == "int":
		if !intPattern.MatchString(value) {
			return fmt.Errorf("value is not an integer")
		}
	case format == "bool":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("value is not a boolean (true/false/1/0)")
		}
	case format == "url":
		u, err := url.Parse(value)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("value is not an https URL")
		}
		if u.User != nil {
			return fmt.Errorf("URL must not embed credentials; grant them as a separate variable")
		}
	case strings.HasPrefix(format, "enum:"):
		for _, v := range strings.Split(strings.TrimPrefix(format, "enum:"), "|") {
			if value == v {
				return nil
			}
		}
		return fmt.Errorf("value is not one of %s", strings.TrimPrefix(format, "enum:"))
	case strings.HasPrefix(format, "regex:"):
		re, err := regexp.Compile("^(?:" + strings.TrimPrefix(format, "regex:") + ")$")
		if err != nil {
			return fmt.Errorf("invalid regex format: %w", err)
		}
		if !re.MatchString(value) {
			return fmt.Errorf("value does not match the granted pattern")
		}
	default:
		return CheckFormat(format)
	}
	return nil
}
