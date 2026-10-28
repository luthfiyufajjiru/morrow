//go:build !windows

package app

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// lockFile takes an exclusive lock on path without waiting. The lock lasts
// until the file is closed or the process dies (also by SIGKILL or the OOM
// killer). Go opens files close-on-exec, so the app never inherits it.
func lockFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, errLocked
		}
		return nil, err
	}
	return f, nil
}

// probeLock reports whether another process holds the lock on path. known is
// false when that cannot be told: no lock file, or no access to it.
func probeLock(path string) (held, known bool) {
	f, err := os.Open(path)
	if err != nil {
		return false, false
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
		return errors.Is(err, unix.EWOULDBLOCK), errors.Is(err, unix.EWOULDBLOCK)
	}
	_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
	return false, true
}
