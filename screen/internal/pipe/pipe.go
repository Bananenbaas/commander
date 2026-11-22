// SPDX-License-Identifier: GPL-2.0-or-later

// Package pipe is the named pipe between Commander and its window. Commander
// listens on it; the window process dials it. Only the signed-in user can
// open the pipe, and remote clients are rejected.
package pipe

import "errors"

// ErrClosed is returned by Accept after the listener is closed.
var ErrClosed = errors.New("pipe: listener closed")

// Name is the pipe of the signed-in user: \\.\pipe\Commander-<SID>.
func Name() (string, error) {
	sid, err := userSID()
	if err != nil {
		return "", err
	}
	return `\\.\pipe\Commander-` + sid, nil
}
