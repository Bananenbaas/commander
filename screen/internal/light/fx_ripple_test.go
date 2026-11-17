// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"math"
	"strings"
	"testing"
	"time"
)

const spaceLED = 75

func renderIn(e Effect, at time.Duration, in *Input) Layer {
	var l Layer
	e.Render(&l, at, in)
	return l
}

func ripplePress(led int, n int) *Input {
	return &Input{Presses: []Press{{LED: led, At: 0, N: n}}}
}

func TestRippleAgeZeroAndEnd(t *testing.T) {
	e := makeFx(t, "ripple", map[string]any{})
	l := renderIn(e, 0, ripplePress(spaceLED, 0))
	if l[spaceLED].A != 255 || l[spaceLED].R != 255 {
		t.Errorf("space at age 0: %v", l[spaceLED])
	}
	if l[0].A != 0 {
		t.Errorf("esc lit at age 0: %v", l[0])
	}
	l = renderIn(e, 1200*time.Millisecond, ripplePress(spaceLED, 0))
	for i, c := range l {
		if c.A != 0 {
			t.Fatalf("LED %d lit at age life: %v", i, c)
		}
	}
}

func TestRippleRingRadius(t *testing.T) {
	e := makeFx(t, "ripple", map[string]any{"speed": 12.0})
	l := renderIn(e, 500*time.Millisecond, ripplePress(spaceLED, 0))
	if l[spaceLED].A != 0 {
		t.Errorf("space lit at 0.5 s: %v", l[spaceLED])
	}
	sp, _ := Pos(spaceLED)
	found := false
	for i, c := range l {
		p, ok := Pos(i)
		if !ok || c.A == 0 {
			continue
		}
		d := math.Hypot(p.X-sp.X, p.Y-sp.Y)
		if math.Abs(d-6) > 1.5 {
			t.Errorf("LED %d lit at distance %.2f", i, d)
		}
		found = true
	}
	if !found {
		t.Error("nothing lit at 0.5 s")
	}
}

func TestRippleColors(t *testing.T) {
	e := makeFx(t, "ripple", map[string]any{"colors": []any{"#FF0000", "#0000FF"}})
	in := &Input{Presses: []Press{{LED: spaceLED, At: 0, N: 0}, {LED: spaceLED, At: 0, N: 1}}}
	if c := renderIn(e, 0, &Input{Presses: in.Presses[:1]})[spaceLED]; c.R != 255 || c.B != 0 {
		t.Errorf("press 0: %v", c)
	}
	if c := renderIn(e, 0, &Input{Presses: in.Presses[1:]})[spaceLED]; c.R != 0 || c.B != 255 {
		t.Errorf("press 1: %v", c)
	}
}

func TestRippleIgnoresFuturePressAndBothColours(t *testing.T) {
	e := makeFx(t, "ripple", map[string]any{})
	l := renderIn(e, 0, &Input{Presses: []Press{{LED: spaceLED, At: time.Second}}})
	if l[spaceLED].A != 0 {
		t.Error("future press drawn")
	}
	s := registry["ripple"]
	v, err := parseValues(s, map[string]any{"color": "#FF0000", "colors": []any{"#00FF00"}})
	if err == nil {
		_, err = s.New(v)
	}
	if err == nil {
		t.Error("color and colors both accepted")
	}
}

func stateStack(t *testing.T, layers ...LayerConfig) *Stack {
	t.Helper()
	s, err := Build(Config{Layers: layers})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestState(t *testing.T) {
	raw := map[string]any{"name": "caps", "color": "#00FF00"}
	e := makeFx(t, "state", raw)
	off := renderIn(e, 0, &Input{States: map[string]bool{"caps": false}})
	on := renderIn(e, 0, &Input{States: map[string]bool{"caps": true}})
	for i := range off {
		if off[i].A != 0 {
			t.Fatalf("off: LED %d drawn", i)
		}
	}
	if on[spaceLED].G != 255 || on[spaceLED].A != 255 {
		t.Errorf("on: %v", on[spaceLED])
	}
	if !registry["state"].NeedsKeys {
		t.Error("NeedsKeys false")
	}
	d := e.(interface{ State() StateDef }).State()
	if d.Name != "caps" || d.Toggle != nil || d.Start {
		t.Errorf("def %+v", d)
	}
}

func TestStateBlink(t *testing.T) {
	e := makeFx(t, "state", map[string]any{"name": "x", "color": "#00FF00", "blink": 2.0})
	in := &Input{States: map[string]bool{"x": true}}
	if renderIn(e, 300*time.Millisecond, in)[spaceLED].A != 0 {
		t.Error("lit at 0.3 s")
	}
	if renderIn(e, 100*time.Millisecond, in)[spaceLED].A == 0 {
		t.Error("dark at 0.1 s")
	}
}

func TestStateToggleAndStart(t *testing.T) {
	e := makeFx(t, "state", map[string]any{"name": "x", "color": "#00FF00", "toggle": "LAlt+N", "start": "on"})
	d := e.(interface{ State() StateDef }).State()
	if d.Toggle == nil || len(d.Toggle.Keys) == 0 || !d.Start {
		t.Errorf("def %+v", d)
	}
	s := registry["state"]
	if _, err := parseValues(s, map[string]any{"name": "x", "color": "#00FF00", "toggle": "wait 5ms"}); err == nil {
		t.Error("wait accepted as toggle")
	}
}

func TestStateHold(t *testing.T) {
	e := makeFx(t, "state", map[string]any{"name": "x", "color": "#00FF00", "toggle": "B", "hold": "400ms"})
	if d := e.(interface{ State() StateDef }).State(); d.Hold != 400*time.Millisecond {
		t.Errorf("Hold = %v, want 400ms", d.Hold)
	}
	e = makeFx(t, "state", map[string]any{"name": "x", "color": "#00FF00", "toggle": "B"})
	if d := e.(interface{ State() StateDef }).State(); d.Hold != 0 {
		t.Errorf("Hold = %v without the setting, want 0", d.Hold)
	}
	s := registry["state"]
	for _, bad := range []string{"50ms", "4s", "soon"} {
		if _, err := parseValues(s, map[string]any{"name": "x", "color": "#00FF00", "toggle": "B", "hold": bad}); err == nil {
			t.Errorf("hold %q accepted", bad)
		}
	}
}

func TestDuplicateStateNames(t *testing.T) {
	lc := LayerConfig{Effect: "state", Keys: []string{"space"}, Settings: map[string]any{"name": "x", "color": "#00FF00"}}
	if _, err := Build(Config{Layers: []LayerConfig{lc, lc}}); err == nil {
		t.Error("duplicate state accepted")
	}
	s := stateStack(t, lc)
	if len(s.States()) != 1 {
		t.Errorf("states %v", s.States())
	}
}

func TestStateToggleModifiers(t *testing.T) {
	s := registry["state"]
	for _, ok := range []string{"N", "LAlt+N", "AltGr+N", "LCtrl+LShift+N", "RWin+F1"} {
		v, err := parseValues(s, map[string]any{"name": "x", "color": "#00FF00", "toggle": ok})
		if err == nil {
			_, err = s.New(v)
		}
		if err != nil {
			t.Errorf("toggle %q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"A+N", "N+LAlt+B", "Space+N"} {
		v, err := parseValues(s, map[string]any{"name": "x", "color": "#00FF00", "toggle": bad})
		if err == nil {
			_, err = s.New(v)
		}
		if err == nil || !strings.Contains(err.Error(), "must be modifiers") {
			t.Errorf("toggle %q: err %v", bad, err)
		}
	}
}
