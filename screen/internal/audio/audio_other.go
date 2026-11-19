// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !(windows && amd64)

package audio

import (
	"errors"
	"runtime"
)

// openEngine has no audio to follow here: the mixer is built for 64-bit
// Windows only (its COM calls pass floats the way the x64 convention does).
func openEngine(func(), func(string, ...any)) (engine, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("the mixer needs 64-bit x86 Windows")
	}
	return nil, errors.New("the mixer needs Windows")
}
