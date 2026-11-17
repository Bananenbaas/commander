// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows && !linux

package runlock

// Acquire does nothing here: kb7screen targets Windows and Linux only.
func Acquire(dir string) (release func() error, err error) {
	return func() error { return nil }, nil
}
