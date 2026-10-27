//go:build windows

package config

import (
	"os"
	"path/filepath"
)

func defaultSystemRoot() string {
	if dir := os.Getenv("ProgramData"); dir != "" {
		return dir
	}
	return `C:\ProgramData`
}

// systemLayout keeps everything under %ProgramData%\Morrow, which survives
// reinstalls and upgrades of the binary in Program Files.
func systemLayout(root string) Layout {
	return flatLayout(filepath.Join(root, "Morrow"))
}
