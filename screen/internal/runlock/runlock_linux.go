// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux

package runlock

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// Acquire takes an exclusive flock on the lock file in dir. The kernel drops
// the lock when the process dies.
func Acquire(dir string) (release func() error, err error) {
	path := filepath.Join(dir, FileName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrHeld
		}
		return nil, err
	}
	return func() error {
		// Remove while the lock is still held, so no other holder's file goes.
		rmErr := os.Remove(path)
		unlockErr := unix.Flock(int(f.Fd()), unix.LOCK_UN)
		closeErr := f.Close()
		if rmErr != nil && errors.Is(rmErr, os.ErrNotExist) {
			rmErr = nil
		}
		return errors.Join(rmErr, unlockErr, closeErr)
	}, nil
}

// userLockPath is the per-user lock file: in the runtime directory when there
// is one, else in the temporary directory.
func userLockPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "commander-kb7.lock")
}

// acquireUser takes an exclusive flock on the user lock file. The file stays;
// the kernel drops the lock when the process dies.
func acquireUser() (release func() error, err error) {
	f, err := os.OpenFile(userLockPath(), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrUserHeld
		}
		return nil, err
	}
	return func() error {
		unlockErr := unix.Flock(int(f.Fd()), unix.LOCK_UN)
		return errors.Join(unlockErr, f.Close())
	}, nil
}
