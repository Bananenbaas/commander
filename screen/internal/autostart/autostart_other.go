// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package autostart

import "errors"

var errOnlyWindows = errors.New("starting with Windows is only available on Windows")

// Enabled is not available on this system.
func Enabled(exe string) (bool, error) { return false, errOnlyWindows }

// Enable is not available on this system.
func Enable(exe, dir string) error { return errOnlyWindows }

// Disable is not available on this system.
func Disable() error { return errOnlyWindows }
