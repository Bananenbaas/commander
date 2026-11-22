// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package pipe

import (
	"errors"
	"io"
	"time"
)

// Listener accepts connections on a named pipe. Named pipes exist on
// Windows only.
type Listener struct{}

func userSID() (string, error) { return "", errors.ErrUnsupported }

// Listen returns errors.ErrUnsupported on this system.
func Listen(name string) (*Listener, error) { return nil, errors.ErrUnsupported }

// Accept returns errors.ErrUnsupported on this system.
func (l *Listener) Accept() (io.ReadWriteCloser, error) { return nil, errors.ErrUnsupported }

// Close returns errors.ErrUnsupported on this system.
func (l *Listener) Close() error { return errors.ErrUnsupported }

// Dial returns errors.ErrUnsupported on this system.
func Dial(name string, timeout time.Duration) (io.ReadWriteCloser, error) {
	return nil, errors.ErrUnsupported
}
