// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package runlock

import (
	"errors"
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
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return nil, ErrHeld
		}
		return nil, err
	}
	return func() error { return windows.CloseHandle(h) }, nil
}
