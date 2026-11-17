// SPDX-License-Identifier: GPL-2.0-or-later

package action

import "testing"

func TestTablesCoverEveryName(t *testing.T) {
	for name, k := range keyNames {
		if _, _, ok := WindowsScan(k); !ok {
			t.Errorf("%s: no Windows scan code", name)
		}
		if c, ok := LinuxCode(k); !ok || c == 0 {
			t.Errorf("%s: no Linux keycode", name)
		}
	}
}

func TestLinuxCodesUnique(t *testing.T) {
	seen := map[uint16]Key{}
	for name, k := range keyNames {
		c, _ := LinuxCode(k)
		if o, dup := seen[c]; dup && o != k {
			t.Errorf("%s shares Linux keycode %d with key %#x", name, c, uint16(o))
		}
		seen[c] = k
	}
}

func TestWindowsScansUnique(t *testing.T) {
	type sc struct {
		code uint8
		ext  bool
	}
	seen := map[sc]Key{}
	for name, k := range keyNames {
		code, ext, _ := WindowsScan(k)
		if o, dup := seen[sc{code, ext}]; dup && o != k {
			t.Errorf("%s shares scan code %#x ext=%v with key %#x", name, code, ext, uint16(o))
		}
		seen[sc{code, ext}] = k
	}
}

func TestWindowsScanValues(t *testing.T) {
	cases := []struct {
		k    Key
		code uint8
		ext  bool
	}{
		{A, 0x1E, false}, {N, 0x31, false}, {Y, 0x15, false}, {L, 0x26, false},
		{U, 0x16, false}, {I, 0x17, false}, {Period, 0x34, false},
		{LCtrl, 0x1D, false}, {LAlt, 0x38, false}, {RAlt, 0x38, true},
		{RCtrl, 0x1D, true}, {LShift, 0x2A, false}, {RShift, 0x36, false},
		{LWin, 0x5B, true}, {RWin, 0x5C, true}, {Up, 0x48, true},
		{Delete, 0x53, true}, {NumEnter, 0x1C, true}, {NumDivide, 0x35, true},
		{PrintScreen, 0x37, true}, {Pause, 0x45, false},
		{VolumeUp, 0x30, true}, {VolumeDown, 0x2E, true}, {Mute, 0x20, true},
		{PlayPause, 0x22, true}, {NextTrack, 0x19, true}, {PrevTrack, 0x10, true},
		{StopMedia, 0x24, true},
	}
	for _, c := range cases {
		code, ext, ok := WindowsScan(c.k)
		if !ok || code != c.code || ext != c.ext {
			t.Errorf("key %#x: got %#x ext=%v ok=%v, want %#x ext=%v", uint16(c.k), code, ext, ok, c.code, c.ext)
		}
	}
}

func TestLinuxCodeValues(t *testing.T) {
	cases := map[Key]uint16{
		A: 30, N: 49, LAlt: 56, RAlt: 100, Enter: 28, Escape: 1, F12: 88,
		VolumeUp: 115, VolumeDown: 114, Mute: 113, PlayPause: 164,
		NextTrack: 163, PrevTrack: 165, StopMedia: 166,
	}
	for k, want := range cases {
		if got, ok := LinuxCode(k); !ok || got != want {
			t.Errorf("key %#x: got %d ok=%v, want %d", uint16(k), got, ok, want)
		}
	}
}
