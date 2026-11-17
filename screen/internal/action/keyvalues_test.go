// SPDX-License-Identifier: GPL-2.0-or-later

package action

import "testing"

func TestKeyTableHasNoDuplicateKeys(t *testing.T) {
	seen := map[Key]bool{}
	for _, e := range keyTable {
		if seen[e.key] {
			t.Errorf("key %#x appears twice in keyTable", uint16(e.key))
		}
		seen[e.key] = true
	}
}

// Independent reference values: name -> Windows set-1 scan code (0xE000 bit =
// extended) and Linux keycode.
var wantCodes = map[string][2]uint16{
	"A": {0x1E, 30}, "B": {0x30, 48}, "C": {0x2E, 46}, "D": {0x20, 32},
	"E": {0x12, 18}, "F": {0x21, 33}, "G": {0x22, 34}, "H": {0x23, 35},
	"I": {0x17, 23}, "J": {0x24, 36}, "K": {0x25, 37}, "L": {0x26, 38},
	"M": {0x32, 50}, "N": {0x31, 49}, "O": {0x18, 24}, "P": {0x19, 25},
	"Q": {0x10, 16}, "R": {0x13, 19}, "S": {0x1F, 31}, "T": {0x14, 20},
	"U": {0x16, 22}, "V": {0x2F, 47}, "W": {0x11, 17}, "X": {0x2D, 45},
	"Y": {0x15, 21}, "Z": {0x2C, 44},
	"1": {0x02, 2}, "2": {0x03, 3}, "3": {0x04, 4}, "4": {0x05, 5},
	"5": {0x06, 6}, "6": {0x07, 7}, "7": {0x08, 8}, "8": {0x09, 9},
	"9": {0x0A, 10}, "0": {0x0B, 11},
	"F1": {0x3B, 59}, "F2": {0x3C, 60}, "F3": {0x3D, 61}, "F4": {0x3E, 62},
	"F5": {0x3F, 63}, "F6": {0x40, 64}, "F7": {0x41, 65}, "F8": {0x42, 66},
	"F9": {0x43, 67}, "F10": {0x44, 68}, "F11": {0x57, 87}, "F12": {0x58, 88},
	"F13": {0x64, 183}, "F14": {0x65, 184}, "F15": {0x66, 185}, "F16": {0x67, 186},
	"F17": {0x68, 187}, "F18": {0x69, 188}, "F19": {0x6A, 189}, "F20": {0x6B, 190},
	"F21": {0x6C, 191}, "F22": {0x6D, 192}, "F23": {0x6E, 193}, "F24": {0x76, 194},
	"Enter": {0x1C, 28}, "Escape": {0x01, 1}, "Backspace": {0x0E, 14},
	"Tab": {0x0F, 15}, "Space": {0x39, 57}, "Minus": {0x0C, 12},
	"Equals": {0x0D, 13}, "LeftBracket": {0x1A, 26}, "RightBracket": {0x1B, 27},
	"Backslash": {0x2B, 43}, "Semicolon": {0x27, 39}, "Quote": {0x28, 40},
	"Grave": {0x29, 41}, "Comma": {0x33, 51}, "Period": {0x34, 52},
	"Slash": {0x35, 53}, "CapsLock": {0x3A, 58},
	"PrintScreen": {0xE037, 99}, "ScrollLock": {0x46, 70}, "Pause": {0x45, 119},
	"Insert": {0xE052, 110}, "Home": {0xE047, 102}, "PageUp": {0xE049, 104},
	"Delete": {0xE053, 111}, "End": {0xE04F, 107}, "PageDown": {0xE051, 109},
	"Right": {0xE04D, 106}, "Left": {0xE04B, 105}, "Down": {0xE050, 108},
	"Up":      {0xE048, 103},
	"NumLock": {0xE045, 69}, "NumDivide": {0xE035, 98}, "NumMultiply": {0x37, 55},
	"NumMinus": {0x4A, 74}, "NumPlus": {0x4E, 78}, "NumEnter": {0xE01C, 96},
	"Num1": {0x4F, 79}, "Num2": {0x50, 80}, "Num3": {0x51, 81},
	"Num4": {0x4B, 75}, "Num5": {0x4C, 76}, "Num6": {0x4D, 77},
	"Num7": {0x47, 71}, "Num8": {0x48, 72}, "Num9": {0x49, 73},
	"Num0": {0x52, 82}, "NumDecimal": {0x53, 83}, "NonUSBackslash": {0x56, 86},
	"LCtrl": {0x1D, 29}, "LShift": {0x2A, 42}, "LAlt": {0x38, 56},
	"LWin": {0xE05B, 125}, "RCtrl": {0xE01D, 97}, "RShift": {0x36, 54},
	"RAlt": {0xE038, 100}, "RWin": {0xE05C, 126},
	"VolumeUp": {0xE030, 115}, "VolumeDown": {0xE02E, 114}, "Mute": {0xE020, 113},
	"PlayPause": {0xE022, 164}, "NextTrack": {0xE019, 163},
	"PrevTrack": {0xE010, 165}, "StopMedia": {0xE024, 166},
}

func TestEveryKeyValue(t *testing.T) {
	if len(wantCodes) != len(keyNames) {
		t.Errorf("reference lists %d keys, ParseKey knows %d", len(wantCodes), len(keyNames))
	}
	for name, want := range wantCodes {
		k, err := ParseKey(name)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		code, extended, ok := WindowsScan(k)
		got := uint16(code)
		if extended {
			got |= 0xE000
		}
		if !ok || got != want[0] {
			t.Errorf("%s: Windows scan %#x ok=%v, want %#x", name, got, ok, want[0])
		}
		if lin, ok := LinuxCode(k); !ok || lin != want[1] {
			t.Errorf("%s: Linux keycode %d ok=%v, want %d", name, lin, ok, want[1])
		}
	}
}
