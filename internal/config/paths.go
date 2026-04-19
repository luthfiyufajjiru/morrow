package config

import (
	"os"
	"path/filepath"
)

// GetPath returns a path relative to the Morrow executable.
func GetPath(filename string) string {
	// Support environment variable override
	if envPath := os.Getenv("MORROW_HOME"); envPath != "" {
		return filepath.Join(envPath, filename)
	}

	exePath, err := os.Executable()
	if err != nil {
		return filename // Fallback to CWD
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
