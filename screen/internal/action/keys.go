// Package action defines key names and the steps a button can carry out.
package action

import (
	"fmt"
	"strings"
)

// Key identifies a key: the USB HID keyboard-page usage for keyboard keys, or
// MediaBase OR'd with the Consumer-page usage for media keys.
type Key uint16

// MediaBase marks a media key; the low bits hold the Consumer usage.
const MediaBase Key = 0x0C00

// Letters.
const (
	A Key = 0x04 + iota
	B
	C
	D
	E
	F
	G
	H
	I
	J
	K
	L
	M
	N
	O
	P
	Q
	R
	S
	T
	U
	V
	W
	X
	Y
	Z
)

// Digits on the main block.
const (
	D1 Key = 0x1e + iota
	D2
	D3
	D4
	D5
	D6
	D7
	D8
	D9
	D0
)

// Editing and punctuation keys.
const (
	Enter        Key = 0x28
	Escape       Key = 0x29
	Backspace    Key = 0x2a
	Tab          Key = 0x2b
	Space        Key = 0x2c
	Minus        Key = 0x2d
	Equals       Key = 0x2e
	LeftBracket  Key = 0x2f
	RightBracket Key = 0x30
	Backslash    Key = 0x31
	Semicolon    Key = 0x33
	Quote        Key = 0x34
	Grave        Key = 0x35
	Comma        Key = 0x36
	Period       Key = 0x37
	Slash        Key = 0x38
	CapsLock     Key = 0x39
)

// Function keys F1 to F12.
const (
	F1 Key = 0x3a + iota
	F2
	F3
	F4
	F5
	F6
	F7
	F8
	F9
	F10
	F11
	F12
)

// Navigation keys.
const (
	PrintScreen Key = 0x46
	ScrollLock  Key = 0x47
	Pause       Key = 0x48
	Insert      Key = 0x49
	Home        Key = 0x4a
	PageUp      Key = 0x4b
	Delete      Key = 0x4c
	End         Key = 0x4d
	PageDown    Key = 0x4e
	Right       Key = 0x4f
	Left        Key = 0x50
	Down        Key = 0x51
	Up          Key = 0x52
)

// Numeric keypad.
const (
	NumLock     Key = 0x53
	NumDivide   Key = 0x54
	NumMultiply Key = 0x55
	NumMinus    Key = 0x56
	NumPlus     Key = 0x57
	NumEnter    Key = 0x58
	Num1        Key = 0x59
	Num2        Key = 0x5a
	Num3        Key = 0x5b
	Num4        Key = 0x5c
	Num5        Key = 0x5d
	Num6        Key = 0x5e
	Num7        Key = 0x5f
	Num8        Key = 0x60
	Num9        Key = 0x61
	Num0        Key = 0x62
	NumDecimal  Key = 0x63

	NonUSBackslash Key = 0x64
)

// Function keys F13 to F24.
const (
	F13 Key = 0x68 + iota
	F14
	F15
	F16
	F17
	F18
	F19
	F20
	F21
	F22
	F23
	F24
)

// Modifiers.
const (
	LCtrl  Key = 0xe0
	LShift Key = 0xe1
	LAlt   Key = 0xe2
	LWin   Key = 0xe3
	RCtrl  Key = 0xe4
	RShift Key = 0xe5
	RAlt   Key = 0xe6
	RWin   Key = 0xe7
)

// Media keys.
const (
	VolumeUp   = MediaBase | 0xE9
	VolumeDown = MediaBase | 0xEA
	Mute       = MediaBase | 0xE2
	PlayPause  = MediaBase | 0xCD
	NextTrack  = MediaBase | 0xB5
	PrevTrack  = MediaBase | 0xB6
	StopMedia  = MediaBase | 0xB7
)

var keyNames = map[string]Key{}

func init() {
	add := func(name string, k Key) { keyNames[strings.ToLower(name)] = k }
	for i := 0; i < 26; i++ {
		add(string(rune('A'+i)), A+Key(i))
	}
	for i := 1; i <= 9; i++ {
		add(fmt.Sprint(i), D1+Key(i-1))
	}
	add("0", D0)
	for i := 0; i < 12; i++ {
		add(fmt.Sprintf("F%d", i+1), F1+Key(i))
		add(fmt.Sprintf("F%d", i+13), F13+Key(i))
	}
	for i, k := range []Key{Num0, Num1, Num2, Num3, Num4, Num5, Num6, Num7, Num8, Num9} {
		add(fmt.Sprintf("Num%d", i), k)
	}
	named := map[string]Key{
		"Enter": Enter, "Escape": Escape, "Backspace": Backspace, "Tab": Tab,
		"Space": Space, "Minus": Minus, "Equals": Equals,
		"LeftBracket": LeftBracket, "RightBracket": RightBracket,
		"Backslash": Backslash, "Semicolon": Semicolon, "Quote": Quote,
		"Grave": Grave, "Comma": Comma, "Period": Period, "Slash": Slash,
		"CapsLock": CapsLock, "PrintScreen": PrintScreen,
		"ScrollLock": ScrollLock, "Pause": Pause, "Insert": Insert,
		"Home": Home, "PageUp": PageUp, "Delete": Delete, "End": End,
		"PageDown": PageDown, "Right": Right, "Left": Left, "Down": Down,
		"Up": Up, "NumLock": NumLock, "NumDivide": NumDivide,
		"NumMultiply": NumMultiply, "NumMinus": NumMinus, "NumPlus": NumPlus,
		"NumEnter": NumEnter, "NumDecimal": NumDecimal,
		"NonUSBackslash": NonUSBackslash,
		"LCtrl":          LCtrl, "LShift": LShift, "LAlt": LAlt, "LWin": LWin,
		"RCtrl": RCtrl, "RShift": RShift, "RAlt": RAlt, "RWin": RWin,
		"VolumeUp": VolumeUp, "VolumeDown": VolumeDown, "Mute": Mute,
		"PlayPause": PlayPause, "NextTrack": NextTrack,
		"PrevTrack": PrevTrack, "StopMedia": StopMedia,
	}
	for n, k := range named {
		add(n, k)
	}
}

// ParseKey returns the key with the given name, matched case-insensitively.
// AltGr is not a key; it is expanded by ParseStep.
func ParseKey(name string) (Key, error) {
	k, ok := keyNames[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return 0, fmt.Errorf("unknown key name %q", name)
	}
	return k, nil
}
