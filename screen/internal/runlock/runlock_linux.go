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
