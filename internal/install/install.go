// Package install prepares the system install's directories and moves data
// from the legacy next-to-binary location into them.
package install

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"morrow/internal/config"
	"morrow/internal/privilege"
	"os"
	"path/filepath"
)

// PrepareSystem creates the directories of a system layout. The key's
// directory is private unless it also holds the database or logs (Windows),
// in which case the key file itself is restricted by privilege.RestrictKeyFile.
func PrepareSystem(l config.Layout) error {
	keyDir := filepath.Dir(l.KeyPath)
	for _, dir := range l.Dirs() {
		private := dir == keyDir && dir != filepath.Dir(l.DBPath) && dir != l.LogsDir
		if err := privilege.PrepareSystemDir(dir, private); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}
	return nil
}

// PrepareUser creates the database and logs directories of a per-user
// install, readable by its owner only.
func PrepareUser(l config.Layout) error {
	for _, dir := range []string{filepath.Dir(l.DBPath), l.LogsDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}
	return nil
}

// Result describes what MigrateLegacy did.
type Result struct {
	Migrated bool
	// Warnings are cleanup steps that failed after the data was copied.
	Warnings []string
}

// sqliteSidecars are the files SQLite may keep next to the database; a hot
// journal must travel with it or the copy would be inconsistent.
var sqliteSidecars = []string{"-journal", "-wal", "-shm"}

// MigrateLegacy moves the database and (if withKey) the master key from the
// legacy layout to the new one, and copies the logs. It does nothing unless
// the legacy database exists and the new one does not.
//
// The database is copied last and renamed into place, so its presence marks a
// completed migration: until then config.Current keeps using the legacy
// location and an interrupted migration simply runs again. The legacy
// database and key are removed afterwards so no second copy of the secrets
// stays readable next to the binary. Legacy logs are left in place, because
// apps that are running keep writing to them until restarted.
func MigrateLegacy(from, to config.Layout, withKey bool) (Result, error) {
	var res Result
	if from.DBPath == to.DBPath || exists(to.DBPath) || !exists(from.DBPath) {
		return res, nil
	}

	moveKey := withKey && exists(from.KeyPath)
	if moveKey {
		if err := copyKey(from.KeyPath, to.KeyPath); err != nil {
			return res, err
		}
	}
	if err := copyTree(from.LogsDir, to.LogsDir); err != nil {
		return res, fmt.Errorf("failed to copy logs: %w", err)
	}
	for _, suffix := range sqliteSidecars {
		if exists(from.DBPath + suffix) {
			if err := copyFile(from.DBPath+suffix, to.DBPath+suffix); err != nil {
				return res, err
			}
		}
	}
	tmp := to.DBPath + ".migrating"
	if err := copyFile(from.DBPath, tmp); err != nil {
		return res, err
	}
	if err := os.Rename(tmp, to.DBPath); err != nil {
		_ = os.Remove(tmp)
		return res, fmt.Errorf("failed to move database into %s: %w", to.DBPath, err)
	}
	res.Migrated = true

	remove := []string{from.DBPath}
	for _, suffix := range sqliteSidecars {
		remove = append(remove, from.DBPath+suffix)
	}
	if moveKey {
		remove = append(remove, from.KeyPath)
	}
	for _, path := range remove {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("could not remove %s: %v; delete it manually", path, err))
		}
	}
	return res, nil
}

// copyKey copies the master key and restricts the copy. An existing,
// different key at the destination is never overwritten: values encrypted
// with either key would become undecryptable.
func copyKey(from, to string) error {
	src, err := os.ReadFile(from)
	if err != nil {
		return fmt.Errorf("failed to read master key %s: %w", from, err)
	}
	if dst, err := os.ReadFile(to); err == nil {
		if !bytes.Equal(src, dst) {
			return fmt.Errorf("a different master key already exists at %s; move one of %s and %s aside before running init", to, from, to)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to read master key %s: %w", to, err)
	} else if err := os.WriteFile(to, src, 0600); err != nil {
		return fmt.Errorf("failed to copy master key to %s: %w", to, err)
	}
	return privilege.RestrictKeyFile(to, false)
}

// copyTree copies the regular files under from into to; a missing from is
// not an error.
func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == from && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(path, target)
	})
}

// copyFile copies a file, keeping its permission bits.
func copyFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	dst, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return fmt.Errorf("failed to copy %s to %s: %w", from, to, err)
	}
	return dst.Close()
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
