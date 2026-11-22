// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/action"
)

func TestLEDOfKey(t *testing.T) {
	cases := []struct {
		k    action.Key
		want int
	}{
		{action.Escape, 0}, {action.F1, 1}, {action.F12, 12}, {action.Grave, 14},
		{action.D1, 15}, {action.D0, 24}, {action.Backspace, 27}, {action.Tab, 28},
		{action.Q, 29}, {action.W, 30}, {action.P, 38}, {action.CapsLock, 42}, {action.A, 43},
		{action.S, 44}, {action.D, 45}, {action.H, 48}, {action.J, 49},
		{action.Enter, 55}, {action.LShift, 56}, {action.Z, 58}, {action.M, 64},
		{action.Slash, 67}, {action.RShift, 69}, {action.Up, 70}, {action.LCtrl, 71},
		{action.LWin, 72}, {action.LAlt, 73}, {action.Space, 75}, {action.RAlt, 78},
		{action.RCtrl, 81}, {action.Left, 82}, {action.Down, 83}, {action.Right, 84},
	}
	for _, c := range cases {
		got, ok := LEDOfKey(c.k)
		if !ok || got != c.want {
			t.Errorf("LEDOfKey(%s) = %d, %v; want %d", action.KeyName(c.k), got, ok, c.want)
		}
	}
	if _, ok := LEDOfKey(action.PrintScreen); ok {
		t.Error("PrintScreen must have no LED")
	}
}

func TestResolveLEDs(t *testing.T) {
	got, err := ResolveLEDs([]string{"W", "A", "S", "D"})
	if err != nil || !reflect.DeepEqual(got, []int{30, 43, 44, 45}) {
		t.Fatalf("WASD = %v, %v", got, err)
	}
	got, err = ResolveLEDs([]string{"wrist"})
	if err != nil || len(got) != 10 || got[0] != 96 || got[9] != 105 {
		t.Fatalf("wrist = %v, %v", got, err)
	}
	got, err = ResolveLEDs([]string{"all"})
	if err != nil || len(got) != 94 {
		t.Fatalf("all = %d LEDs, %v", len(got), err)
	}
	got, err = ResolveLEDs([]string{"keys"})
	if err != nil || len(got) != 85-7 || got[0] != 0 || got[len(got)-1] != 84 {
		t.Fatalf("keys = %v, %v", got, err)
	}
	got, err = ResolveLEDs([]string{"W", "w", "wrist1", "Wrist10", "Fn", "menu", "RGBButton", "GameMode"})
	if err != nil || !reflect.DeepEqual(got, []int{30, 79, 80, 87, 90, 96, 105}) {
		t.Fatalf("mixed = %v, %v", got, err)
	}
	if _, err := ResolveLEDs([]string{"W", "Nope"}); err == nil || !strings.Contains(err.Error(), "Nope") {
		t.Fatalf("unknown name error = %v", err)
	}
}

func TestPos(t *testing.T) {
	for _, led := range []int{13, 54, 57, 68, 74, 76, 77, 91, 95, 106, 111} {
		if _, ok := Pos(led); ok {
			t.Errorf("LED %d must have no position", led)
		}
	}
	if _, ok := Pos(-1); ok {
		t.Error("negative LED has a position")
	}
	w, _ := Pos(30)
	e, _ := Pos(31)
	a, _ := Pos(43)
	if !(w.X < e.X) {
		t.Errorf("W %v not left of E %v", w, e)
	}
	if !(a.Y > w.Y) {
		t.Errorf("A %v not below W %v", a, w)
	}
	if d := math.Hypot(w.X-a.X, w.Y-a.Y); d < 0.8 || d > 1.3 {
		t.Errorf("distance W-A = %v", d)
	}
	all, _ := ResolveLEDs([]string{"all"})
	seen := map[Point]int{}
	for _, led := range all {
		p, ok := Pos(led)
		if !ok {
			t.Errorf("mapped LED %d has no position", led)
			continue
		}
		if other, dup := seen[p]; dup {
			t.Errorf("LEDs %d and %d share position %v", other, led, p)
		}
		seen[p] = led
	}
}
