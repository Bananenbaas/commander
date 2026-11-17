// SPDX-License-Identifier: GPL-2.0-or-later

// Package runlock keeps "kb7screen run" and the commands that upload pictures
// from using the keyboard's interface 1 at the same time for one configuration
// folder. The lock is held by the operating system, so it is freed when the
// process dies.
package runlock

import "errors"

// ErrHeld is returned when another process holds the lock.
var ErrHeld = errors.New("the run lock is held by another process")

// FileName is the name of the lock file inside the configuration folder.
const FileName = ".kb7screen-run.lock"

// Held reports whether another holder has the lock on dir. It takes the lock
// and gives it back at once.
func Held(dir string) (bool, error) {
	release, err := Acquire(dir)
	if errors.Is(err, ErrHeld) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return false, release()
}
