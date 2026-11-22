// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"errors"
	"testing"
)

func TestBuildErrorsAreLayerErrors(t *testing.T) {
	cases := []struct {
		name    string
		layer   LayerConfig
		setting string
		text    string
	}{
		{"one colour", LayerConfig{Effect: "wave", Settings: map[string]any{"colors": []any{"#FF0000"}}},
			"colors", "layer 2: wave: wave: colors: needs at least two colours"},
		{"unknown effect", LayerConfig{Effect: "nope"}, "effect", `layer 2: unknown effect "nope"`},
		{"fixed without keys", LayerConfig{Effect: "fixed", Settings: map[string]any{"color": "#FF0000"}},
			"keys", "layer 2: fixed: keys: required but not given"},
		{"unknown setting", LayerConfig{Effect: "static", Settings: map[string]any{"color": "#FF0000", "zzz": 1}},
			"zzz", `layer 2: static: unknown setting "zzz"`},
		{"bad value", LayerConfig{Effect: "static", Settings: map[string]any{"color": "red"}},
			"color", `layer 2: static: color: "red" is not a colour such as "#FF8000"`},
		{"missing", LayerConfig{Effect: "wave"}, "colors", "layer 2: wave: colors: required but not given"},
	}
	base := LayerConfig{Effect: "static", Settings: map[string]any{"color": "#000000"}}
	for _, c := range cases {
		_, err := Build(Config{Layers: []LayerConfig{base, c.layer}})
		var le *LayerError
		if !errors.As(err, &le) {
			t.Fatalf("%s: %v is not a LayerError", c.name, err)
		}
		if le.Layer != 2 || le.Setting != c.setting {
			t.Errorf("%s: layer %d setting %q, want 2 %q", c.name, le.Layer, le.Setting, c.setting)
		}
		if err.Error() != c.text {
			t.Errorf("%s: text %q, want %q", c.name, err.Error(), c.text)
		}
	}
}

func TestLEDsNamesResolve(t *testing.T) {
	leds := LEDs()
	if len(leds) != 94 {
		t.Fatalf("%d LEDs, want 94", len(leds))
	}
	for _, l := range leds {
		got, err := ResolveLEDs([]string{l.Name})
		if err != nil || len(got) != 1 || got[0] != l.Index {
			t.Errorf("LED %d name %q resolves to %v, %v", l.Index, l.Name, got, err)
		}
		if p, ok := Pos(l.Index); !ok || p.X != l.X || p.Y != l.Y {
			t.Errorf("LED %d position differs from Pos", l.Index)
		}
	}
}

func TestFramesMatchesPreview(t *testing.T) {
	s, err := Build(Config{Layers: []LayerConfig{
		{Effect: "static", Settings: map[string]any{"color": "#000000"}},
		{Effect: "ripple", Settings: map[string]any{"color": "#FF0000"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	f := Frames(s)
	if len(f) != FrameCount || FrameCount != 80 {
		t.Fatalf("%d frames, want 80", len(f))
	}
	if *f[12] == *f[8] { // 0.6 s against 0.4 s
		t.Error("no ring started at the W press")
	}
}

func TestRegisterRefusesEnabled(t *testing.T) {
	mustPanic(t, "enabled setting", func() {
		Register(Spec{Name: "test-enabled", New: func(Values) (Effect, error) { return nil, nil },
			Settings: []Setting{{Name: "enabled", Kind: KindName}}})
	})
}
