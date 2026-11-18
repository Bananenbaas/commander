// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package tray

import (
	"context"
	"errors"
)

// Tray is a running tray icon; there is none on this system.
type Tray struct{}

var errOnlyWindows = errors.New("the tray icon is only available on Windows")

// Run is not available on this system.
func Run(ctx context.Context, o Options) (*Tray, error) { return nil, errOnlyWindows }

// SetTip does nothing on this system.
func (t *Tray) SetTip(s string) {}

// SetFailed does nothing on this system.
func (t *Tray) SetFailed(failed bool) {}

// Balloon does nothing on this system.
func (t *Tray) Balloon(title, text string) {}

// BalloonAction does nothing on this system.
func (t *Tray) BalloonAction(title, text string, onClick func()) {}

// Done is closed: there is no tray thread on this system.
func (t *Tray) Done() <-chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}
