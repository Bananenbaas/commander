// SPDX-License-Identifier: GPL-2.0-or-later

// Package light holds the KB7's LED table and the effects drawn onto it.
package light

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Bananenbaas/commander/screen/internal/action"
)

// NumLEDs is the number of slots in the LED table; 94 of them drive something.
const NumLEDs = 112

// Point is a position in key units: X grows to the right, Y downwards. The
// origin is the centre of the Escape key.
type Point struct{ X, Y float64 }

type led struct {
	key  action.Key // zero when the LED has no action.Key
	name string     // extra name for LEDs that are not keys, else ""
	pos  Point
}

var (
	table    [NumLEDs]led
	mapped   [NumLEDs]bool
	byKey    = map[action.Key]int{}
	byName   = map[string]int{}
	groupAll []int
	groupKey []int
	groupWr  []int
)

// setKey registers a key LED.
func setKey(i int, k action.Key, x, y float64) {
	table[i] = led{key: k, pos: Point{x, y}}
	mapped[i] = true
	byKey[k] = i
}

// setNamed registers an LED that has no action.Key.
func setNamed(i int, name string, x, y float64) {
	table[i] = led{name: name, pos: Point{x, y}}
	mapped[i] = true
	byName[strings.ToLower(name)] = i
}

// row places keys of the given widths side by side starting at the left edge
// -0.5 (so that a one-unit key starts centred on an integer column).
func row(first int, y float64, keys []action.Key, widths []float64) {
	left := -0.5
	for n, k := range keys {
		setKey(first+n, k, left+widths[n]/2, y)
		left += widths[n]
	}
}

func ones(n int) []float64 {
	w := make([]float64, n)
	for i := range w {
		w[i] = 1
	}
	return w
}

func init() {
	// Function row: Escape, then F1-F12 in blocks of four.
	setKey(0, action.Escape, 0, 0)
	fx := []float64{2, 3, 4, 5, 6.5, 7.5, 8.5, 9.5, 11, 12, 13, 14}
	for n := 0; n < 12; n++ {
		setKey(1+n, action.F1+action.Key(n), fx[n], 0)
	}

	// Number row: ` 1-0 - = Backspace (two units wide).
	num := []action.Key{action.Grave, action.D1, action.D2, action.D3, action.D4, action.D5,
		action.D6, action.D7, action.D8, action.D9, action.D0, action.Minus, action.Equals, action.Backspace}
	w := ones(14)
	w[13] = 2
	row(14, 1.5, num, w)

	// Tab row: Tab (1.5), Q-P, [ ], \ (1.5).
	top := []action.Key{action.Tab, action.Q, action.W, action.E, action.R, action.T, action.Y,
		action.U, action.I, action.O, action.P, action.LeftBracket, action.RightBracket, action.Backslash}
	w = ones(14)
	w[0], w[13] = 1.5, 1.5
	row(28, 2.5, top, w)

	// Caps row: Caps Lock (1.75), A-L, ; ', Enter (2.25, slot 55).
	home := []action.Key{action.CapsLock, action.A, action.S, action.D, action.F, action.G, action.H,
		action.J, action.K, action.L, action.Semicolon, action.Quote, action.Enter}
	w = ones(13)
	w[0], w[12] = 1.75, 2.25
	row(42, 3.5, home[:12], w[:12])
	setKey(55, action.Enter, 13.375, 3.5) // slot 54 is the unused ISO # key

	// Shift row: Left Shift (2.25, slot 57 is the unused ISO key), Z-M , . /,
	// Right Shift (2.75, slot 68 is unused).
	bot := []action.Key{action.LShift, action.Z, action.X, action.C, action.V, action.B, action.N,
		action.M, action.Comma, action.Period, action.Slash, action.RShift}
	w = ones(12)
	w[0], w[11] = 2.25, 2.75
	row(56, 4.5, bot[:1], w[:1])
	left := -0.5 + 2.25
	for n := 1; n < 11; n++ {
		setKey(57+n, bot[n], left+0.5, 4.5)
		left++
	}
	setKey(69, action.RShift, left+2.75/2, 4.5)

	// Bottom row: Ctrl, Win, Alt (1.25 each), Space (6.25), Alt, Fn, Menu, Ctrl.
	setKey(71, action.LCtrl, 0.125, 5.5)
	setKey(72, action.LWin, 1.375, 5.5)
	setKey(73, action.LAlt, 2.625, 5.5)
	setKey(75, action.Space, 6.375, 5.5)
	setKey(78, action.RAlt, 10.125, 5.5)
	setNamed(79, "Fn", 11.375, 5.5)
	setNamed(80, "Menu", 12.625, 5.5)
	setKey(81, action.RCtrl, 13.875, 5.5)

	// Arrow cluster to the right of the main block.
	setKey(70, action.Up, 17, 4.5)
	setKey(82, action.Left, 16, 5.5)
	setKey(83, action.Down, 17, 5.5)
	setKey(84, action.Right, 18, 5.5)

	// Media keys and the four triangular buttons above the function row.
	setNamed(85, "PrevTrack", 16, -1.5)
	setNamed(86, "NextTrack", 17, -1.5)
	setNamed(87, "GameMode", 5, -1.5)
	setNamed(88, "ProfileCycle", 6, -1.5)
	setNamed(89, "MicOff", 7, -1.5)
	setNamed(90, "RGBButton", 8, -1.5)

	// Wrist rest strip, ten segments below the main block.
	for n := 0; n < 10; n++ {
		setNamed(96+n, fmt.Sprintf("Wrist%d", n+1), 1+float64(n)*1.4, 7.5)
	}

	for i := 0; i < NumLEDs; i++ {
		if !mapped[i] {
			continue
		}
		groupAll = append(groupAll, i)
		switch {
		case i <= 84:
			groupKey = append(groupKey, i)
		case i >= 96 && i <= 105:
			groupWr = append(groupWr, i)
		}
	}
}

// LEDOfKey returns the LED under key k.
func LEDOfKey(k action.Key) (int, bool) {
	i, ok := byKey[k]
	return i, ok
}

// Pos returns the position of a mapped LED in key units.
func Pos(led int) (Point, bool) {
	if led < 0 || led >= NumLEDs || !mapped[led] {
		return Point{}, false
	}
	return table[led].pos, true
}

// ResolveLEDs turns names into a sorted list of distinct LED indices. A name is
// a key name as action.ParseKey takes it, one of Fn, Menu, PrevTrack,
// NextTrack, GameMode, ProfileCycle, MicOff, RGBButton, Wrist1 to Wrist10, or a
// group: all, keys (main block and arrows) or wrist. Names are not case
// sensitive.
func ResolveLEDs(names []string) ([]int, error) {
	set := map[int]bool{}
	for _, name := range names {
		key := strings.ToLower(strings.TrimSpace(name))
		var list []int
		switch key {
		case "all":
			list = groupAll
		case "keys":
			list = groupKey
		case "wrist":
			list = groupWr
		default:
			if i, ok := byName[key]; ok {
				list = []int{i}
			} else if k, err := action.ParseKey(name); err == nil {
				if i, ok := byKey[k]; ok {
					list = []int{i}
				} else {
					return nil, fmt.Errorf("key %q has no LED", name)
				}
			} else {
				return nil, fmt.Errorf("unknown LED or key name %q", name)
			}
		}
		for _, i := range list {
			set[i] = true
		}
	}
	out := make([]int, 0, len(set))
	for i := range set {
		out = append(out, i)
	}
	sort.Ints(out)
	return out, nil
}
