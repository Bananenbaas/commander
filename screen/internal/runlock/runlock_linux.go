// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux

package runlock

import (
	"errors"
	"fmt"
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

// userLockPath is the per-user lock file: in $XDG_RUNTIME_DIR when it is set,
// else in a private folder (mode 0700) named commander in the user's cache
// folder. Never in a folder that other users can write to.
func userLockPath() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(cache, "commander")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
	}
	return filepath.Join(dir, "commander-kb7.lock"), nil
}

// acquireUser takes an exclusive flock on the user lock file. The file stays;
// the kernel drops the lock when the process dies.
func acquireUser() (release func() error, err error) {
	path, err := userLockPath()
	if err != nil {
		return nil, err
	}
	// O_NOFOLLOW: a symbolic link planted at the path is not followed.
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open the user lock file %s: %w", path, err)
	}
	f := os.NewFile(uintptr(fd), path)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		f.Close()
		return nil, err
	}
	if int(st.Uid) != os.Getuid() {
		f.Close()
		return nil, fmt.Errorf("the user lock file %s belongs to another user", path)
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
