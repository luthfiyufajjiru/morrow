package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	dbFile  = "morrow.db"
	keyFile = ".morrow.key"
	logsDir = "logs"
)

// Layout is where one install keeps its database, master key and logs.
type Layout struct {
	DBPath  string
	KeyPath string
	LogsDir string
}

// Dirs returns the distinct directories of the layout, in creation order.
func (l Layout) Dirs() []string {
	var dirs []string
	for _, d := range []string{filepath.Dir(l.KeyPath), filepath.Dir(l.DBPath), l.LogsDir} {
		if !contains(dirs, d) {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// flatLayout keeps everything in one directory: per-user installs and the
// legacy system install next to the binary.
func flatLayout(dir string) Layout {
	return Layout{
		DBPath:  filepath.Join(dir, dbFile),
		KeyPath: filepath.Join(dir, keyFile),
		LogsDir: filepath.Join(dir, logsDir),
	}
}

// systemRoot is the base the system layout is built from (see systemLayout);
// tests point it at a temp dir instead of the real /etc or ProgramData.
var systemRoot = defaultSystemRoot()

// exeDir returns the directory of the running binary; tests override it.
var exeDir = func() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exePath), nil
}

// SystemLayout returns the layout of a system install (MORROW_HOME unset).
func SystemLayout() Layout {
	return systemLayout(systemRoot)
}

// LegacyLayout returns where older system installs kept their data:
// next to the binary. ok is false if the binary's location is unknown.
func LegacyLayout() (layout Layout, ok bool) {
	dir, err := exeDir()
	if err != nil {
		return Layout{}, false
	}
	return flatLayout(dir), true
}

// IsPerUser reports whether Morrow runs as a per-user install (MORROW_HOME
// set) rather than a system install.
func IsPerUser() bool {
	return os.Getenv("MORROW_HOME") != ""
}

// UsingLegacy reports whether a system install still runs from the legacy
// next-to-binary location because `morrow init` has not migrated it yet.
func UsingLegacy() bool {
	if IsPerUser() || exists(SystemLayout().DBPath) {
		return false
	}
	legacy, ok := LegacyLayout()
	return ok && exists(legacy.DBPath)
}

// Current returns the layout this process uses: MORROW_HOME for per-user
// installs, otherwise the system layout, falling back to the legacy location
// until it is migrated. MORROW_KEY_PATH overrides the key in every mode.
func Current() Layout {
	var l Layout
	switch {
	case IsPerUser():
		l = flatLayout(os.Getenv("MORROW_HOME"))
	case UsingLegacy():
		l, _ = LegacyLayout()
	default:
		l = SystemLayout()
	}
	if envPath := os.Getenv("MORROW_KEY_PATH"); envPath != "" {
		l.KeyPath = envPath
	}
	return l
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// GetDataDir returns the directory holding the database.
func GetDataDir() string {
	return filepath.Dir(GetDBPath())
}

// GetDBPath returns the absolute path to the database file.
func GetDBPath() string {
	return Current().DBPath
}

// GetKeyPath returns the absolute path to the encryption key file.
func GetKeyPath() string {
	return Current().KeyPath
}

// GetLogsDir returns the absolute path to the logs directory, creating it if needed.
func GetLogsDir() string {
	dir := Current().LogsDir
	_ = os.MkdirAll(dir, 0755)
	return dir
}

// GetLogFilePath returns the path to the log file for a specific application.
func GetLogFilePath(appName string) string {
	return filepath.Join(GetLogsDir(), fmt.Sprintf("%s.log", appName))
}

// GetRelayPIDFilePath returns the path to the PID file for the log relay sidecar.
func GetRelayPIDFilePath(appName string) string {
	return filepath.Join(GetLogsDir(), fmt.Sprintf("%s.relay.pid", appName))
}

// Inbox paths for write-only secret submission (system installs only). The
// public key and the inbox live beside the database, where regular users can
// reach them; the private key lives beside the master key.
const (
	inboxDir        = "inbox"
	inboxPublicKey  = "inbox.pub"
	inboxPrivateKey = ".morrow-inbox.key"
)

// GetInboxDir returns the directory deployers drop sealed submissions into.
func GetInboxDir() string {
	return filepath.Join(filepath.Dir(Current().DBPath), inboxDir)
}

// GetInboxPublicKeyPath returns the world-readable key used to seal submissions.
func GetInboxPublicKeyPath() string {
	return filepath.Join(filepath.Dir(Current().DBPath), inboxPublicKey)
}

// GetInboxPrivateKeyPath returns the root/Administrator-only key that opens submissions.
func GetInboxPrivateKeyPath() string {
	return filepath.Join(filepath.Dir(Current().KeyPath), inboxPrivateKey)
}
