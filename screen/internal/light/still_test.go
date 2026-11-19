// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"testing"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
)

func TestStillStates(t *testing.T) {
	s := build(t, Config{Layers: []LayerConfig{
		staticLayer("#000000"),
		{Effect: "ripple", Settings: map[string]any{"color": "#FF0000"}},
		stateLayer("gear", "N", "on", "N", "#00FF00"),
		stateLayer("quantum", "B", "off", "B", "#0000FF"),
	}})
	p := Still(s)
	if p == nil {
		t.Fatal("Still: nil")
	}
	if got := rgbOf(p, ledOf(t, action.N)); got != [3]byte{0, 255, 0} {
		t.Errorf("N is % x, want green", got)
	}
	if got := rgbOf(p, ledOf(t, action.B)); got != [3]byte{0, 0, 0} {
		t.Errorf("B is % x, want off: its state starts off", got)
	}
	for led := range NumLEDs {
		if c := rgbOf(p, led); c[0] != 0 {
			t.Fatalf("LED %d is % x: ripple colour in the still picture", led, c)
		}
	}
	for i := NumLEDs / 16 * 48; i < len(p); i++ {
		if p[i] != 0 {
			t.Fatalf("payload byte %d is %x, want 0", i, p[i])
		}
	}
}

func TestStillIsNotScaled(t *testing.T) {
	p := Still(build(t, Config{Layers: []LayerConfig{staticLayer("#FFFFFF")}}))
	if got := rgbOf(p, 0); got != [3]byte{255, 255, 255} {
		t.Errorf("LED 0 is % x, want white", got)
	}
	if Still(nil) != nil {
		t.Error("Still(nil) is not nil")
	}
}
