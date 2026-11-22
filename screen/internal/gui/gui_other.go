// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package gui

import "errors"

// Run returns errors.ErrUnsupported: the window needs WebView2, which exists
// on Windows only.
func Run(pipeName string, opts Options) error { return errors.ErrUnsupported }
