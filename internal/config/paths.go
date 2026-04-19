package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// GetPath returns a path relative to the Morrow executable.
func GetPath(filename string) string {
	if envPath := os.Getenv("MORROW_HOME"); envPath != "" {
		return filepath.Join(envPath, filename)
	}
	exePath, err := os.Executable()
	if err != nil {
		return filename
	}
	exeDir := filepath.Dir(exePath)
	return filepath.Join(exeDir, filename)
}

// GetDBPath returns the absolute path to the database file.
func GetDBPath() string {
	return GetPath("morrow.db")
}

// GetKeyPath returns the absolute path to the encryption key file.
func GetKeyPath() string {
	if envPath := os.Getenv("MORROW_KEY_PATH"); envPath != "" {
		return envPath
	}
	return GetPath(".morrow.key")
}

// GetLogsDir returns the absolute path to the logs directory, creating it if needed.
func GetLogsDir() string {
	dir := GetPath("logs")
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
