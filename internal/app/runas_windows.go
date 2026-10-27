//go:build windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	advapi32                    = windows.NewLazySystemDLL("advapi32.dll")
	procLogonUserW              = advapi32.NewProc("LogonUserW")
	procCreateProcessWithTokenW = advapi32.NewProc("CreateProcessWithTokenW")
)

const (
	logon32LogonInteractive = 2
	logon32LogonBatch       = 4
	logon32ProviderDefault  = 0
	logonWithProfile        = 1
	createNoWindow          = 0x08000000
)

// checkRunAsAccount validates a run-as account. Windows cannot start a
// process as another account without its password.
func checkRunAsAccount(account, password string) error {
	if password == "" {
		return fmt.Errorf("Windows needs the password of %s to start the app as that account: pass it with --password-env <VAR>", account)
	}
	if _, _, _, err := windows.LookupSID("", account); err != nil {
		return fmt.Errorf("unknown account %q: %w", account, err)
	}
	return nil
}

// splitAccount turns DOMAIN\name or name into (domain, name); "." is the
// local machine. A UPN (name@domain) is passed whole with no domain.
func splitAccount(account string) (domain, name string) {
	if d, n, ok := strings.Cut(account, `\`); ok {
		return d, n
	}
	if strings.Contains(account, "@") {
		return "", account
	}
	return ".", account
}

// logon gets a primary token for the account, as a batch logon (service-like)
// or, if the account lacks that right, an interactive one.
func logon(account, password string) (windows.Token, error) {
	domain, name := splitAccount(account)
	u, _ := windows.UTF16PtrFromString(name)
	p, _ := windows.UTF16PtrFromString(password)
	var d *uint16
	if domain != "" {
		d, _ = windows.UTF16PtrFromString(domain)
	}
	var lastErr error
	for _, kind := range []uintptr{logon32LogonBatch, logon32LogonInteractive} {
		var tok windows.Token
		r, _, err := procLogonUserW.Call(uintptr(unsafe.Pointer(u)), uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(p)),
			kind, logon32ProviderDefault, uintptr(unsafe.Pointer(&tok)))
		if r != 0 {
			return tok, nil
		}
		lastErr = err
	}
	return 0, fmt.Errorf("cannot log on as %s: %w", account, lastErr)
}

// tokenChild is a process created for another account.
type tokenChild struct {
	pid int
	h   windows.Handle
}

func (c tokenChild) Pid() int { return c.pid }

func (c tokenChild) Wait() ExitInfo {
	defer windows.CloseHandle(c.h)
	if _, err := windows.WaitForSingleObject(c.h, windows.INFINITE); err != nil {
		return ExitInfo{Code: -1}
	}
	var code uint32
	if err := windows.GetExitCodeProcess(c.h, &code); err != nil {
		return ExitInfo{Code: -1}
	}
	return ExitInfo{Code: int(code)}
}

// mergeEnv keeps the last value of each variable (names are case-insensitive)
// and sorts the result, as Windows environment blocks expect.
func mergeEnv(env []string) []string {
	byKey := map[string]string{}
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k == "" {
			continue // per-drive entries like "=C:=C:\"
		}
		byKey[strings.ToUpper(k)] = kv
	}
	out := make([]string, 0, len(byKey))
	for _, kv := range byKey {
		out = append(out, kv)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToUpper(out[i]) < strings.ToUpper(out[j]) })
	return out
}

func envBlock(env []string) []uint16 {
	var b []uint16
	for _, kv := range env {
		s, err := windows.UTF16FromString(kv)
		if err != nil {
			continue
		}
		b = append(b, s...) // includes the terminating NUL
	}
	return append(b, 0)
}

// startChild starts the app without a console window, as account if one is
// set. For another account it logs the account on with its password and uses
// CreateProcessWithTokenW, which works both from an elevated Administrator
// (it needs SeImpersonatePrivilege) and from SYSTEM (the boot task).
func startChild(cmd *exec.Cmd, extra []string, account, password string) (child, error) {
	if account == "" {
		detachProcess(cmd)
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return execChild{cmd}, nil
	}
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	tok, err := logon(account, password)
	if err != nil {
		return nil, err
	}
	defer tok.Close()

	userEnv, err := tok.Environ(false)
	if err != nil {
		return nil, fmt.Errorf("cannot load the environment of %s: %w", account, err)
	}
	block := envBlock(mergeEnv(append(userEnv, extra...)))
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{cmd.Path}, cmd.Args[1:]...)))
	if err != nil {
		return nil, err
	}
	var cwd *uint16
	if cmd.Dir != "" {
		if cwd, err = windows.UTF16PtrFromString(cmd.Dir); err != nil {
			return nil, err
		}
	}

	out, ok := cmd.Stdout.(*os.File)
	if !ok {
		return nil, fmt.Errorf("internal error: output is not a file")
	}
	nul, err := os.Open(os.DevNull)
	if err != nil {
		return nil, err
	}
	defer nul.Close()
	outH, nulH := windows.Handle(out.Fd()), windows.Handle(nul.Fd())
	for _, h := range []windows.Handle{outH, nulH} {
		if err := windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			return nil, err
		}
	}
	defer windows.SetHandleInformation(outH, windows.HANDLE_FLAG_INHERIT, 0)

	si := windows.StartupInfo{
		Flags:      windows.STARTF_USESTDHANDLES | windows.STARTF_USESHOWWINDOW,
		ShowWindow: windows.SW_HIDE,
		StdInput:   nulH,
		StdOutput:  outH,
		StdErr:     outH,
	}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	r, _, callErr := procCreateProcessWithTokenW.Call(uintptr(tok), logonWithProfile, 0, uintptr(unsafe.Pointer(cmdline)),
		createNoWindow|windows.CREATE_UNICODE_ENVIRONMENT|windows.CREATE_NEW_PROCESS_GROUP,
		uintptr(unsafe.Pointer(&block[0])), uintptr(unsafe.Pointer(cwd)), uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	if r == 0 {
		return nil, fmt.Errorf("cannot start the app as %s: %w", account, callErr)
	}
	windows.CloseHandle(pi.Thread)
	return tokenChild{pid: int(pi.ProcessId), h: pi.Process}, nil
}
