// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"image/color"
	"testing"
	"time"
)

func makeFx(t *testing.T, name string, raw map[string]any) Effect {
	t.Helper()
	s, ok := registry[name]
	if !ok {
		t.Fatalf("effect %q not registered", name)
	}
	v, err := parseValues(s, raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	e, err := s.New(v)
	if err != nil {
		t.Fatalf("%s: New: %v", name, err)
	}
	return e
}

func render(e Effect, at time.Duration) Layer {
	var l Layer
	e.Render(&l, at, &Input{})
	return l
}

func near8(a, b uint8) bool {
	d := int(a) - int(b)
	return d >= -1 && d <= 1
}

func TestStaticAndFixedFillMappedLEDs(t *testing.T) {
	want := color.NRGBA{0x12, 0x34, 0x56, 255}
	for _, name := range []string{"static", "fixed"} {
		l := render(makeFx(t, name, map[string]any{"color": "#123456"}), 0)
		for i := 0; i < NumLEDs; i++ {
			_, mapped := Pos(i)
			if mapped && l[i] != want {
				t.Errorf("%s: LED %d = %v, want %v", name, i, l[i], want)
			}
			if !mapped && l[i].A != 0 {
				t.Errorf("%s: unmapped LED %d drawn", name, i)
			}
		}
	}
	if registry["static"].NeedsKeys || !registry["fixed"].NeedsKeys {
		t.Error("NeedsKeys: static must be false, fixed true")
	}
}

func TestBreathing(t *testing.T) {
	e := makeFx(t, "breathing", map[string]any{
		"colors": []any{"#FF0000", "#0000FF"}, "period": "4s", "low": 0.1,
	})
	l := render(e, 0)
	if c := l[0]; !near8(c.R, 25) || c.G != 0 || c.B != 0 || c.A != 255 {
		t.Errorf("t=0: %v, want about 10%% red", c)
	}
	l = render(e, 2*time.Second)
	if c := l[0]; !near8(c.R, 255) || c.B != 0 {
		t.Errorf("t=period/2: %v, want full red", c)
	}
	l = render(e, 6*time.Second)
	if c := l[0]; c.R != 0 || !near8(c.B, 255) {
		t.Errorf("second period peak: %v, want full blue", c)
	}
	l = render(e, 8*time.Second)
	if c := l[0]; !near8(c.R, 25) {
		t.Errorf("colour list must wrap: %v", c)
	}
}

func TestWaveColourPeriodicInSpace(t *testing.T) {
	cols := []color.NRGBA{{255, 0, 0, 255}, {0, 0, 255, 255}}
	a := waveColor(cols, 3.0, 14, 0.5, 0)
	b := waveColor(cols, 3.0+14, 14, 0.5, 0)
	if !near8(a.R, b.R) || !near8(a.B, b.B) {
		t.Errorf("one length apart differ: %v vs %v", a, b)
	}
	mid := waveColor(cols, 3.5, 14, 0.5, 0) // phase 0.25: halfway red to blue
	if !near8(mid.R, 128) && !near8(mid.R, 127) {
		t.Errorf("midpoint not interpolated: %v", mid)
	}
}

func TestWaveRender(t *testing.T) {
	raw := map[string]any{"colors": []any{"#FF0000", "#0000FF"}, "speed": 0.5, "length": 14.0}
	e := makeFx(t, "wave", raw)
	l0 := render(e, 0)
	l1 := render(e, 2*time.Second) // 1/speed
	for i := 0; i < NumLEDs; i++ {
		if _, ok := Pos(i); !ok {
			continue
		}
		if l0[i].A != 255 {
			t.Fatalf("LED %d not opaque", i)
		}
		if !near8(l0[i].R, l1[i].R) || !near8(l0[i].B, l1[i].B) {
			t.Fatalf("LED %d differs after 1/speed: %v vs %v", i, l0[i], l1[i])
		}
	}
	raw["direction"] = "left"
	left := makeFx(t, "wave", raw)
	at := 500 * time.Millisecond // 0.25/speed
	r, lf := render(e, at), render(left, at)
	diff := false
	for i := 0; i < NumLEDs; i++ {
		if r[i] != lf[i] {
			diff = true
		}
	}
	if !diff {
		t.Error("left and right look the same")
	}
}

func TestWaveNeedsTwoColours(t *testing.T) {
	s := registry["wave"]
	v, err := parseValues(s, map[string]any{"colors": []any{"#FF0000"}})
	if err == nil {
		_, err = s.New(v)
	}
	if err == nil {
		t.Error("one colour accepted")
	}
}
