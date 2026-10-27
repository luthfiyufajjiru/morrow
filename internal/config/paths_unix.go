//go:build !windows

package config

import "path/filepath"

func defaultSystemRoot() string { return "/" }

// systemLayout follows the FHS: the key is configuration (/etc), the database
// is state (/var/lib) and logs go where admins and logrotate expect them
// (/var/log), so package upgrades replacing the binary never touch them.
func systemLayout(root string) Layout {
	return Layout{
		DBPath:  filepath.Join(root, "var", "lib", "morrow", dbFile),
		KeyPath: filepath.Join(root, "etc", "morrow", keyFile),
		LogsDir: filepath.Join(root, "var", "log", "morrow"),
	}
}
