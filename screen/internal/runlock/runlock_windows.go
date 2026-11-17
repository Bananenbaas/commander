// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package runlock

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Acquire takes the lock on dir. The file is opened without sharing and is
// deleted when the handle closes, which also happens when the process dies.
func Acquire(dir string) (release func() error, err error) {
	name, err := windows.UTF16PtrFromString(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0, nil, windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_DELETE_ON_CLOSE, 0)
	if err != nil {
		return nil, mapCreateError(filepath.Join(dir, FileName), err)
	}
	return func() error { return windows.CloseHandle(h) }, nil
}

// mapCreateError turns the error of creating the lock file into ErrHeld when
// another process has it open. Any other failure, such as an access denied
// error for an unwritable folder or a lock file still pending deletion, is
// returned with its cause.
func mapCreateError(path string, err error) error {
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return ErrHeld
	}
	return fmt.Errorf("cannot create the lock file %s: %w (is the folder writable?)", path, err)
}

// userMutexName is the per-user mutex of the run loop.
const userMutexName = `Local\Commander-KB7`

// acquireUser creates the named mutex and keeps its handle open until
// release; the system closes it when the process dies.
func acquireUser() (release func() error, err error) {
	name, err := windows.UTF16PtrFromString(userMutexName)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return nil, ErrUserHeld
	}
	if err != nil {
		return nil, fmt.Errorf("cannot create the user lock: %w", err)
	}
	return func() error { return windows.CloseHandle(h) }, nil
}
