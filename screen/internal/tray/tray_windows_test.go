// SPDX-License-Identifier: GPL-2.0-or-later

//go:build amd64

package tray

import (
	"testing"
	"unsafe"
)

// The structures handed to Windows must have the sizes of their C layout.
func TestStructSizesAMD64(t *testing.T) {
	cases := []struct {
		name      string
		got, want uintptr
	}{
		{"NOTIFYICONDATAW", unsafe.Sizeof(notifyIconData{}), 976},
		{"WNDCLASSEXW", unsafe.Sizeof(wndClassEx{}), 80},
		{"MSG", unsafe.Sizeof(winMsg{}), 48},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s: %d bytes, want %d", c.name, c.got, c.want)
		}
	}
}
