// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"image/color"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
)

// testSolid fills the whole layer with one colour at one alpha.
type testSolid struct{ c color.NRGBA }

func (s testSolid) Render(l *Layer, _ time.Duration, _ *Input) {
	for i := range l {
		l[i] = s.c
	}
}

// testState is a test-only state effect: it declares a state and draws
// nothing.
type testState struct{ def StateDef }

func (testState) Render(*Layer, time.Duration, *Input) {}
func (s testState) State() StateDef                    { return s.def }

// lastAll keeps the values the "test-all" effect was last built with.
var lastAll Values

func init() {
	Register(Spec{
		Name: "test-solid",
		Help: "Test only: one colour everywhere.",
		Settings: []Setting{
			{Name: "color", Kind: KindColor, Required: true, Help: "The colour."},
			{Name: "alpha", Kind: KindNumber, Default: 255.0, Min: 0, Max: 255, Help: "Opacity."},
		},
		New: func(v Values) (Effect, error) {
			c := v["color"].(color.NRGBA)
			c.A = uint8(v["alpha"].(float64))
			return testSolid{c}, nil
		},
	})
	Register(Spec{
		Name:      "test-keys",
		Help:      "Test only: needs keys.",
		NeedsKeys: true,
		Settings:  []Setting{{Name: "color", Kind: KindColor, Default: "#FFFFFF"}},
		New: func(v Values) (Effect, error) {
			return testSolid{v["color"].(color.NRGBA)}, nil
		},
	})
	Register(Spec{
		Name: "test-state",
		Help: "Test only: declares a state.",
		Settings: []Setting{
			{Name: "name", Kind: KindName, Required: true},
			{Name: "toggle", Kind: KindChord},
			{Name: "start", Kind: KindChoice, Choices: []string{"on", "off"}, Default: "off"},
		},
		New: func(v Values) (Effect, error) {
			d := StateDef{Name: v["name"].(string), Start: v["start"] == "on"}
			if c, ok := v["toggle"].(action.Chord); ok {
				d.Toggle = &c
			}
			return testState{d}, nil
		},
	})
	Register(Spec{
		Name: "test-all",
		Help: "Test only: one setting of each kind.",
		Settings: []Setting{
			{Name: "color", Kind: KindColor, Default: "#102030"},
			{Name: "colors", Kind: KindColors, Default: []string{"#FF0000", "#00ff00"}},
			{Name: "number", Kind: KindNumber, Default: 0.5, Min: 0, Max: 1},
			{Name: "free", Kind: KindNumber, Default: 0.0},
			{Name: "choice", Kind: KindChoice, Default: "right", Choices: []string{"right", "left"}},
			{Name: "time", Kind: KindDuration, Default: "4s", Min: 0.5, Max: 60},
			{Name: "chord", Kind: KindChord, Default: "LAlt+N"},
			{Name: "name", Kind: KindName, Default: "gear"},
			{Name: "optional", Kind: KindNumber},
		},
		New: func(v Values) (Effect, error) {
			lastAll = v
			return testSolid{}, nil
		},
	})
}

func spec(t *testing.T, name string) Spec {
	t.Helper()
	s, ok := registry[name]
	if !ok {
		t.Fatalf("effect %q is not registered", name)
	}
	return s
}

func TestDefaultsFilled(t *testing.T) {
	v, err := parseValues(spec(t, "test-all"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Values{
		"color":  color.NRGBA{0x10, 0x20, 0x30, 255},
		"colors": []color.NRGBA{{255, 0, 0, 255}, {0, 255, 0, 255}},
		"number": 0.5,
		"free":   0.0,
		"choice": "right",
		"time":   4 * time.Second,
		"chord":  action.Chord{Keys: []action.Key{action.LAlt, action.N}},
		"name":   "gear",
	}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("defaults:\n got %#v\nwant %#v", v, want)
	}
	if _, ok := v["optional"]; ok {
		t.Fatal("a setting without default and not given must stay absent")
	}
}

func TestValuesParsed(t *testing.T) {
	raw := map[string]any{
		"color":  "#a0B0c0",
		"colors": []any{"#000001"},
		"number": int64(1),
		"free":   float64(-7.5),
		"choice": "LEFT",
		"time":   "600ms",
		"chord":  "N",
		"name":   "quantum",
	}
	v, err := parseValues(spec(t, "test-all"), raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := v["color"]; got != (color.NRGBA{0xa0, 0xb0, 0xc0, 255}) {
		t.Errorf("color = %v", got)
	}
	if got := v["colors"]; !reflect.DeepEqual(got, []color.NRGBA{{0, 0, 1, 255}}) {
		t.Errorf("colors = %v", got)
	}
	if got := v["number"]; got != 1.0 {
		t.Errorf("TOML int64 number = %#v, want float64 1", got)
	}
	if got := v["free"]; got != -7.5 {
		t.Errorf("free = %v", got)
	}
	if got := v["choice"]; got != "left" {
		t.Errorf("choice = %v, want the declared spelling", got)
	}
	if got := v["time"]; got != 600*time.Millisecond {
		t.Errorf("time = %v", got)
	}
	if got := v["chord"]; !reflect.DeepEqual(got, action.Chord{Keys: []action.Key{action.N}}) {
		t.Errorf("chord = %v", got)
	}
	if got := v["name"]; got != "quantum" {
		t.Errorf("name = %v", got)
	}
}

func TestValuesRefused(t *testing.T) {
	cases := []struct {
		effect string
		raw    map[string]any
		want   []string // all must appear in the error
	}{
		{"test-all", map[string]any{"spede": 1.0}, []string{"test-all", "spede"}},
		{"test-solid", map[string]any{"color": "#GG0000"}, []string{"test-solid", "color", "#GG0000"}},
		{"test-solid", map[string]any{"color": "#FF000"}, []string{"color", "#FF000"}},
		{"test-solid", map[string]any{"color": "FF0000"}, []string{"color", "FF0000"}},
		{"test-solid", map[string]any{"color": int64(5)}, []string{"color", "5"}},
		{"test-solid", map[string]any{}, []string{"test-solid", "color", "required"}},
		{"test-solid", map[string]any{"color": "#FF0000", "alpha": int64(-1)}, []string{"test-solid: alpha: -1 is below the minimum 0"}},
		{"test-solid", map[string]any{"color": "#FF0000", "alpha": 256.5}, []string{"test-solid: alpha: 256.5 is above the maximum 255"}},
		{"test-solid", map[string]any{"color": "#FF0000", "alpha": "full"}, []string{"alpha", "full"}},
		{"test-all", map[string]any{"colors": []any{}}, []string{"colors"}},
		{"test-all", map[string]any{"colors": []any{"#FF0000", "red"}}, []string{"colors", "red"}},
		{"test-all", map[string]any{"colors": "#FF0000"}, []string{"colors"}},
		{"test-all", map[string]any{"choice": "up"}, []string{"choice", "up", "right", "left"}},
		{"test-all", map[string]any{"time": "4"}, []string{"time", "4"}},
		{"test-all", map[string]any{"time": "100ms"}, []string{"time", "100ms", "below"}},
		{"test-all", map[string]any{"time": "2m"}, []string{"time", "2m", "above"}},
		{"test-all", map[string]any{"time": "-1s"}, []string{"time", "-1s"}},
		{"test-all", map[string]any{"chord": "wait 5ms"}, []string{"chord", "wait 5ms"}},
		{"test-all", map[string]any{"chord": "NoSuchKey"}, []string{"chord", "NoSuchKey"}},
		{"test-all", map[string]any{"name": "  "}, []string{"name"}},
		{"test-all", map[string]any{"name": int64(3)}, []string{"name", "3"}},
	}
	for _, c := range cases {
		_, err := parseValues(spec(t, c.effect), c.raw)
		if err == nil {
			t.Errorf("%s %v: no error", c.effect, c.raw)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s %v: error %q does not mention %q", c.effect, c.raw, err, w)
			}
		}
	}
}

func TestSpecsSorted(t *testing.T) {
	specs := Specs()
	var names []string
	for _, s := range specs {
		names = append(names, s.Name)
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Fatalf("Specs not sorted by name: %v", names)
		}
	}
	for _, want := range []string{"test-all", "test-keys", "test-solid", "test-state"} {
		found := false
		for _, n := range names {
			found = found || n == want
		}
		if !found {
			t.Errorf("Specs lacks %q: %v", want, names)
		}
	}
}

func mustPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", what)
		}
	}()
	f()
}

func TestRegisterRefusesMistakes(t *testing.T) {
	newNil := func(Values) (Effect, error) { return nil, nil }
	mustPanic(t, "duplicate name", func() {
		Register(Spec{Name: "test-solid", New: newNil})
	})
	mustPanic(t, "no New", func() {
		Register(Spec{Name: "test-no-new"})
	})
	mustPanic(t, "bad default", func() {
		Register(Spec{Name: "test-bad-default", New: newNil,
			Settings: []Setting{{Name: "c", Kind: KindColor, Default: "red"}}})
	})
	mustPanic(t, "no kind", func() {
		Register(Spec{Name: "test-no-kind", New: newNil, Settings: []Setting{{Name: "c"}}})
	})
	mustPanic(t, "reserved setting", func() {
		Register(Spec{Name: "test-reserved", New: newNil,
			Settings: []Setting{{Name: "keys", Kind: KindName}}})
	})
	for _, n := range []string{"test-no-new", "test-bad-default", "test-no-kind", "test-reserved"} {
		if _, ok := registry[n]; ok {
			t.Errorf("%q registered despite the panic", n)
		}
	}
}
