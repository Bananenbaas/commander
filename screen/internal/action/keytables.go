// SPDX-License-Identifier: GPL-2.0-or-later

package action

// keyCodes holds, for every key ParseKey accepts, the Windows set-1 scan code
// (0xE0xx means the extended flag is set) and the Linux input keycode. The
// tables carry no build tags so that their tests run on every OS.
type keyCodes struct {
	key Key
	win uint16
	lin uint16
}

const ext = 0xE000

var keyTable = []keyCodes{
	{A, 0x1E, 30}, {B, 0x30, 48}, {C, 0x2E, 46}, {D, 0x20, 32}, {E, 0x12, 18},
	{F, 0x21, 33}, {G, 0x22, 34}, {H, 0x23, 35}, {I, 0x17, 23}, {J, 0x24, 36},
	{K, 0x25, 37}, {L, 0x26, 38}, {M, 0x32, 50}, {N, 0x31, 49}, {O, 0x18, 24},
	{P, 0x19, 25}, {Q, 0x10, 16}, {R, 0x13, 19}, {S, 0x1F, 31}, {T, 0x14, 20},
	{U, 0x16, 22}, {V, 0x2F, 47}, {W, 0x11, 17}, {X, 0x2D, 45}, {Y, 0x15, 21},
	{Z, 0x2C, 44},

	{D1, 0x02, 2}, {D2, 0x03, 3}, {D3, 0x04, 4}, {D4, 0x05, 5}, {D5, 0x06, 6},
	{D6, 0x07, 7}, {D7, 0x08, 8}, {D8, 0x09, 9}, {D9, 0x0A, 10}, {D0, 0x0B, 11},

	{Enter, 0x1C, 28}, {Escape, 0x01, 1}, {Backspace, 0x0E, 14}, {Tab, 0x0F, 15},
	{Space, 0x39, 57}, {Minus, 0x0C, 12}, {Equals, 0x0D, 13},
	{LeftBracket, 0x1A, 26}, {RightBracket, 0x1B, 27}, {Backslash, 0x2B, 43},
	{Semicolon, 0x27, 39}, {Quote, 0x28, 40}, {Grave, 0x29, 41},
	{Comma, 0x33, 51}, {Period, 0x34, 52}, {Slash, 0x35, 53}, {CapsLock, 0x3A, 58},

	{F1, 0x3B, 59}, {F2, 0x3C, 60}, {F3, 0x3D, 61}, {F4, 0x3E, 62},
	{F5, 0x3F, 63}, {F6, 0x40, 64}, {F7, 0x41, 65}, {F8, 0x42, 66},
	{F9, 0x43, 67}, {F10, 0x44, 68}, {F11, 0x57, 87}, {F12, 0x58, 88},
	{F13, 0x64, 183}, {F14, 0x65, 184}, {F15, 0x66, 185}, {F16, 0x67, 186},
	{F17, 0x68, 187}, {F18, 0x69, 188}, {F19, 0x6A, 189}, {F20, 0x6B, 190},
	{F21, 0x6C, 191}, {F22, 0x6D, 192}, {F23, 0x6E, 193}, {F24, 0x76, 194},

	// Linux has no separate print-screen code besides KEY_SYSRQ (99), which
	// desktops treat as Print Screen.
	{PrintScreen, ext | 0x37, 99},
	{ScrollLock, 0x46, 70},
	// Pause is really the sequence E1 1D 45 and cannot be sent as one scan
	// code; plain 0x45 is what Windows maps to VK_PAUSE. It is rarely used.
	{Pause, 0x45, 119},
	{Insert, ext | 0x52, 110}, {Home, ext | 0x47, 102}, {PageUp, ext | 0x49, 104},
	{Delete, ext | 0x53, 111}, {End, ext | 0x4F, 107}, {PageDown, ext | 0x51, 109},
	{Right, ext | 0x4D, 106}, {Left, ext | 0x4B, 105}, {Down, ext | 0x50, 108},
	{Up, ext | 0x48, 103},

	// Windows reports NumLock as the extended form of 0x45.
	{NumLock, ext | 0x45, 69}, {NumDivide, ext | 0x35, 98}, {NumMultiply, 0x37, 55},
	{NumMinus, 0x4A, 74}, {NumPlus, 0x4E, 78}, {NumEnter, ext | 0x1C, 96},
	{Num1, 0x4F, 79}, {Num2, 0x50, 80}, {Num3, 0x51, 81}, {Num4, 0x4B, 75},
	{Num5, 0x4C, 76}, {Num6, 0x4D, 77}, {Num7, 0x47, 71}, {Num8, 0x48, 72},
	{Num9, 0x49, 73}, {Num0, 0x52, 82}, {NumDecimal, 0x53, 83},
	{NonUSBackslash, 0x56, 86},

	{LCtrl, 0x1D, 29}, {LShift, 0x2A, 42}, {LAlt, 0x38, 56}, {LWin, ext | 0x5B, 125},
	{RCtrl, ext | 0x1D, 97}, {RShift, 0x36, 54}, {RAlt, ext | 0x38, 100},
	{RWin, ext | 0x5C, 126},

	{VolumeUp, ext | 0x30, 115}, {VolumeDown, ext | 0x2E, 114}, {Mute, ext | 0x20, 113},
	{PlayPause, ext | 0x22, 164}, {NextTrack, ext | 0x19, 163},
	{PrevTrack, ext | 0x10, 165}, {StopMedia, ext | 0x24, 166},
}

var byKey = func() map[Key]keyCodes {
	m := make(map[Key]keyCodes, len(keyTable))
	for _, e := range keyTable {
		m[e.key] = e
	}
	return m
}()

// WindowsScan returns the set-1 scan code of k and whether it needs the
// extended-key flag.
func WindowsScan(k Key) (code uint8, extended bool, ok bool) {
	e, ok := byKey[k]
	return uint8(e.win), e.win&ext == ext, ok
}

// LinuxCode returns the Linux input keycode of k.
func LinuxCode(k Key) (uint16, bool) {
	e, ok := byKey[k]
	return e.lin, ok
}
