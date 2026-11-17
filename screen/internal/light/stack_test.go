// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"image/color"
	"reflect"
	"strings"
	"testing"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
)

var (
	red  = color.NRGBA{255, 0, 0, 255}
	blue = color.NRGBA{0, 0, 255, 255}
)

func solid(c string, alpha float64, keys ...string) LayerConfig {
	return LayerConfig{Effect: "test-solid", Keys: keys,
		Settings: map[string]any{"color": c, "alpha": alpha}}
}

func build(t *testing.T, c Config) *Stack {
	t.Helper()
	s, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func buildErr(t *testing.T, c Config, want ...string) {
	t.Helper()
	_, err := Build(c)
	if err == nil {
		t.Fatalf("Build(%+v): no error", c)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not mention %q", err, w)
		}
	}
}

func TestBuildFPS(t *testing.T) {
	layers := []LayerConfig{solid("#FF0000", 255)}
	if got := build(t, Config{Layers: layers}).FPS(); got != 30 {
		t.Errorf("FPS 0 gives %d, want 30", got)
	}
	if got := build(t, Config{FPS: 10, Layers: layers}).FPS(); got != 10 {
		t.Errorf("FPS 10 gives %d", got)
	}
	buildErr(t, Config{FPS: 9, Layers: layers}, "fps", "9")
	buildErr(t, Config{FPS: 31, Layers: layers}, "fps", "31")
}

func TestBuildRefuses(t *testing.T) {
	buildErr(t, Config{}, "layer")
	buildErr(t, Config{Layers: []LayerConfig{{Effect: "sparkle"}}}, "layer 1", "sparkle")
	buildErr(t, Config{Layers: []LayerConfig{
		solid("#FF0000", 255),
		{Effect: "test-solid", Settings: map[string]any{"color": "#FF0000", "spede": 1.0}},
	}}, "layer 2", "test-solid", "spede")
	buildErr(t, Config{Layers: []LayerConfig{solid("#FF0000", 255, "W", "Nope")}}, "layer 1", "Nope")
	buildErr(t, Config{Layers: []LayerConfig{{Effect: "test-keys"}}}, "layer 1", "test-keys", "keys")
}

func TestNeedsKeysWithKeys(t *testing.T) {
	build(t, Config{Layers: []LayerConfig{{Effect: "test-keys", Keys: []string{"W"}}}})
}

func TestComposeOpaqueOver(t *testing.T) {
	s := build(t, Config{Layers: []LayerConfig{solid("#0000FF", 255), solid("#FF0000", 255)}})
	f := s.Render(0, &Input{})
	for i, c := range f {
		if c != red {
			t.Fatalf("LED %d = %v, want %v", i, c, red)
		}
	}
}

func TestComposeHalfAlpha(t *testing.T) {
	s := build(t, Config{Layers: []LayerConfig{solid("#0000FF", 255), solid("#FF0000", 128)}})
	f := s.Render(0, &Input{})
	want := color.NRGBA{128, 0, 127, 255}
	for i, c := range f {
		if !near(c, want, 1) {
			t.Fatalf("LED %d = %v, want %v ±1", i, c, want)
		}
	}
}

func TestComposeOverBlack(t *testing.T) {
	s := build(t, Config{Layers: []LayerConfig{solid("#FF0000", 0)}})
	for i, c := range s.Render(0, &Input{}) {
		if c != (color.NRGBA{0, 0, 0, 255}) {
			t.Fatalf("LED %d = %v, want opaque black", i, c)
		}
	}
}

func TestComposeKeysMask(t *testing.T) {
	s := build(t, Config{Layers: []LayerConfig{solid("#0000FF", 255), solid("#FF0000", 255, "W")}})
	f := s.Render(0, &Input{})
	for i, c := range f {
		want := blue
		if i == 30 {
			want = red
		}
		if c != want {
			t.Errorf("LED %d = %v, want %v", i, c, want)
		}
	}
}

func TestBuildCollectsStates(t *testing.T) {
	s := build(t, Config{Layers: []LayerConfig{
		{Effect: "test-state", Settings: map[string]any{"name": "gear", "toggle": "N", "start": "on"}},
		solid("#FF0000", 255),
		{Effect: "test-state", Settings: map[string]any{"name": "quantum"}},
	}})
	n := action.Chord{Keys: []action.Key{action.N}}
	want := []StateDef{{Name: "gear", Toggle: &n, Start: true}, {Name: "quantum"}}
	if got := s.States(); !reflect.DeepEqual(got, want) {
		t.Fatalf("States = %+v, want %+v", got, want)
	}
}

func TestBuildRefusesDuplicateStates(t *testing.T) {
	buildErr(t, Config{Layers: []LayerConfig{
		{Effect: "test-state", Settings: map[string]any{"name": "gear"}},
		{Effect: "test-state", Settings: map[string]any{"name": "gear"}},
	}}, "layer 2", "gear")
}

func near(a, b color.NRGBA, tol int) bool {
	d := func(x, y uint8) bool { v := int(x) - int(y); return v <= tol && v >= -tol }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B) && a.A == b.A
}
