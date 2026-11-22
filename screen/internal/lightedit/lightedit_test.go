// SPDX-License-Identifier: GPL-2.0-or-later

package lightedit

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/layout"
)

// decode reads a [light] (and anything else) written as TOML, as the window
// gets it from a file.
func decode(t *testing.T, text string) *layout.File {
	t.Helper()
	f, err := layout.DecodeFile([]byte("profile = 0\n" + text))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return f
}

func intp(n int) *int { return &n }

func TestRoundTrip(t *testing.T) {
	none := []string{}
	noStates := []State{}
	for name, s := range map[string]Simple{
		"none":           {Colors: none, States: noStates},
		"off":            {Base: "off", Colors: none, States: noStates},
		"static":         {Base: "static", Colors: []string{"#FF8000"}, States: noStates},
		"wave default":   {Base: "wave", Colors: []string{"#FF0000", "#0000FF"}, Speed: 0.5, Direction: "right", States: noStates},
		"wave set":       {Base: "wave", Colors: []string{"#FF0000", "#00FF00", "#0000FF"}, Speed: 2.25, Direction: "up", States: noStates},
		"breathing":      {Base: "breathing", Colors: []string{"#FFFFFF"}, Period: 4, States: noStates},
		"breathing set":  {Base: "breathing", Colors: []string{"#FF0000", "#00FF00"}, Period: 2.5, States: noStates},
		"ripple one":     {Base: "static", Colors: []string{"#101010"}, Ripple: &Ripple{Colors: []string{"#FFFFFF"}}, States: noStates},
		"ripple several": {Colors: none, Ripple: &Ripple{Colors: []string{"#FF0000", "#00FF00"}}, States: noStates},
		"highlight":      {Base: "off", Colors: none, Highlight: &Highlight{Keys: []string{"W", "A", "S", "D"}, Color: "#FF0000"}, States: noStates},
		"everything": {
			Base: "wave", Colors: []string{"#FF0000", "#0000FF"}, Speed: 1, Direction: "left",
			Ripple:    &Ripple{Colors: []string{"#FFFFFF"}},
			Highlight: &Highlight{Keys: []string{"wrist"}, Color: "#00FF00"},
			States: []State{
				{Name: "caps", Toggle: "CapsLock", Keys: []string{"CapsLock"}, Color: "#FF0000"},
				{Name: "mute", Keys: []string{"F1", "F2"}, Color: "#0000FF"},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok, reason := FromFile(ToFile(s))
			if !ok {
				t.Fatalf("not ok: %s", reason)
			}
			if !reflect.DeepEqual(got, s) {
				t.Fatalf("round trip\n got %#v\nwant %#v", got, s)
			}
		})
	}
}

func TestFromFileNil(t *testing.T) {
	s, ok, reason := FromFile(nil)
	if !ok || reason != "" || s.Base != "" || s.Ripple != nil || s.Highlight != nil || len(s.States) != 0 {
		t.Fatalf("FromFile(nil) = %#v, %v, %q", s, ok, reason)
	}
}

func TestFromFileFits(t *testing.T) {
	f := decode(t, `
[light]
fps = 30

[[light.layer]]
effect = "wave"
colors = ["#ff0000", "#0000ff"]
speed = 3
direction = "Left"
length = 14

[[light.layer]]
effect = "fixed"
keys = ["W", "A", "S", "D"]
color = "#00ff00"

[[light.layer]]
effect = "state"
name = "caps"
toggle = "CapsLock"
keys = ["CapsLock"]
color = "#FF0000"
start = "off"

[[light.layer]]
effect = "state"
name = "mute"
keys = ["F1"]
color = "#0000FF"
enabled = true
`)
	s, ok, reason := FromFile(f.Light)
	if !ok {
		t.Fatalf("not ok: %s", reason)
	}
	want := Simple{
		Base: "wave", Colors: []string{"#FF0000", "#0000FF"}, Speed: 3, Direction: "left",
		Highlight: &Highlight{Keys: []string{"W", "A", "S", "D"}, Color: "#00FF00"},
		States: []State{
			{Name: "caps", Toggle: "CapsLock", Keys: []string{"CapsLock"}, Color: "#FF0000"},
			{Name: "mute", Keys: []string{"F1"}, Color: "#0000FF"},
		},
	}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("got %#v\nwant %#v", s, want)
	}
}

func TestFromFileDefaultsInOtherForms(t *testing.T) {
	f := decode(t, `
[[light.layer]]
effect = "breathing"
period = "4000ms"
low = 0.1

[[light.layer]]
effect = "ripple"
mode = "PRESS"
spread = 8
life = "1200ms"
`)
	s, ok, reason := FromFile(f.Light)
	if !ok {
		t.Fatalf("not ok: %s", reason)
	}
	want := Simple{Base: "breathing", Colors: []string{"#FFFFFF"}, Period: 4,
		Ripple: &Ripple{Colors: []string{"#FFFFFF"}}, States: []State{}}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("got %#v\nwant %#v", s, want)
	}
}

func TestFromFileStaticBlackIsOff(t *testing.T) {
	f := decode(t, "[[light.layer]]\neffect = \"static\"\ncolor = \"#000000\"\n")
	s, ok, _ := FromFile(f.Light)
	if !ok || s.Base != "off" || len(s.Colors) != 0 {
		t.Fatalf("got %#v, %v", s, ok)
	}
}

func TestFromFileDoesNotFit(t *testing.T) {
	for name, tc := range map[string]struct{ toml, reason string }{
		"two bases": {`
[[light.layer]]
effect = "static"
color = "#FF0000"
[[light.layer]]
effect = "wave"
colors = ["#FF0000", "#0000FF"]
`, "This lighting has two base effects, so it opens in Advanced."},
		"ripple below base": {`
[[light.layer]]
effect = "ripple"
[[light.layer]]
effect = "static"
color = "#FF0000"
`, "The ripple sits below the base effect, so it opens in Advanced."},
		"state below highlight": {`
[[light.layer]]
effect = "state"
name = "caps"
keys = ["CapsLock"]
color = "#FF0000"
[[light.layer]]
effect = "fixed"
keys = ["W"]
color = "#FF0000"
`, "A key state sits below the highlighted keys, so it opens in Advanced."},
		"two ripples": {`
[[light.layer]]
effect = "ripple"
[[light.layer]]
effect = "ripple"
`, "This lighting has two ripples, so it opens in Advanced."},
		"length": {`
[[light.layer]]
effect = "wave"
colors = ["#FF0000", "#0000FF"]
length = 20
`, "The wave's length is changed, which the simple view has no control for."},
		"fps": {`
[light]
fps = 20
[[light.layer]]
effect = "static"
color = "#FF0000"
`, "The frame rate is changed, which the simple view has no control for."},
		"layer off": {`
[[light.layer]]
effect = "static"
color = "#FF0000"
[[light.layer]]
effect = "ripple"
enabled = false
`, "One of the layers is switched off, so it opens in Advanced."},
		"blink": {`
[[light.layer]]
effect = "state"
name = "caps"
keys = ["CapsLock"]
color = "#FF0000"
blink = 2
`, "The key state's blinking is changed, which the simple view has no control for."},
		"hold": {`
[[light.layer]]
effect = "state"
name = "caps"
toggle = "CapsLock"
hold = "500ms"
keys = ["CapsLock"]
color = "#FF0000"
`, "The key state's hold time is set, which the simple view has no control for."},
		"start on": {`
[[light.layer]]
effect = "state"
name = "caps"
keys = ["CapsLock"]
color = "#FF0000"
start = "on"
`, "The key state's starting state is changed, which the simple view has no control for."},
		"ripple wave mode": {`
[[light.layer]]
effect = "ripple"
colors = ["#FF0000", "#0000FF"]
mode = "wave"
`, "The ripple's colour mode is changed, which the simple view has no control for."},
		"low": {`
[[light.layer]]
effect = "breathing"
low = 0.3
`, "The breathing's lowest brightness is changed, which the simple view has no control for."},
		"base on some keys": {`
[[light.layer]]
effect = "static"
keys = ["W"]
color = "#FF0000"
`, "The one colour covers only some keys, which the simple view has no control for."},
		"unknown effect": {`
[[light.layer]]
effect = "sparkle"
`, "This lighting uses the sparkle effect, which the simple view has no control for."},
		"slow breathing": {`
[[light.layer]]
effect = "breathing"
period = "30s"
`, "The breathing's speed is outside what the simple view's slider covers, so it opens in Advanced."},
	} {
		t.Run(name, func(t *testing.T) {
			f := decode(t, tc.toml)
			_, ok, reason := FromFile(f.Light)
			if ok {
				t.Fatalf("fits the simple view; want %q", tc.reason)
			}
			if reason != tc.reason {
				t.Fatalf("reason %q, want %q", reason, tc.reason)
			}
		})
	}
}

func TestReasonsAreSentences(t *testing.T) {
	f := decode(t, "[[light.layer]]\neffect = \"wave\"\ncolors = \"#FF0000\"\n")
	_, ok, reason := FromFile(f.Light)
	if ok || reason != "The wave's colours are not valid, so it opens in Advanced." || strings.ContainsRune(reason, 0x2014) {
		t.Fatalf("got ok %v, reason %q", ok, reason)
	}
}

func TestToFileOff(t *testing.T) {
	l := ToFile(Simple{Base: "off"})
	if l == nil || len(l.Layers) != 1 {
		t.Fatalf("got %#v", l)
	}
	want := map[string]any{"effect": "static", "color": "#000000"}
	if !reflect.DeepEqual(l.Layers[0], want) {
		t.Fatalf("got %#v, want %#v", l.Layers[0], want)
	}
}

func TestToFileEmpty(t *testing.T) {
	if l := ToFile(Simple{}); l != nil {
		t.Fatalf("got %#v, want nil", l)
	}
	if l := ToFile(Simple{Colors: []string{}, States: []State{}}); l != nil {
		t.Fatalf("got %#v, want nil", l)
	}
}

func TestToFileLeavesDefaultsOut(t *testing.T) {
	l := ToFile(Simple{
		Base: "wave", Colors: []string{"#ff0000", "#0000ff"}, Speed: 0.5, Direction: "right",
		Ripple: &Ripple{Colors: []string{"#ffffff"}},
	})
	want := []map[string]any{
		{"effect": "wave", "colors": []string{"#FF0000", "#0000FF"}},
		{"effect": "ripple", "color": "#FFFFFF"},
	}
	if l == nil || l.FPS != 0 || !reflect.DeepEqual(l.Layers, want) {
		t.Fatalf("got %#v, want %#v", l, want)
	}
	b := ToFile(Simple{Base: "breathing", Colors: []string{"#FFFFFF"}, Period: 4})
	if want := []map[string]any{{"effect": "breathing"}}; !reflect.DeepEqual(b.Layers, want) {
		t.Fatalf("got %#v, want %#v", b.Layers, want)
	}
}

func TestToFileWritesSettings(t *testing.T) {
	l := ToFile(Simple{
		Base: "breathing", Colors: []string{"#FF0000"}, Period: 2.5,
		Ripple: &Ripple{Colors: []string{"#FF0000", "#00FF00"}},
		States: []State{{Name: "caps", Toggle: "CapsLock", Keys: []string{"CapsLock"}, Color: "#ff0000"}},
	})
	want := []map[string]any{
		{"effect": "breathing", "colors": []string{"#FF0000"}, "period": "2.5s"},
		{"effect": "ripple", "colors": []string{"#FF0000", "#00FF00"}},
		{"effect": "state", "name": "caps", "toggle": "CapsLock", "keys": []string{"CapsLock"}, "color": "#FF0000"},
	}
	if !reflect.DeepEqual(l.Layers, want) {
		t.Fatalf("got %#v\nwant %#v", l.Layers, want)
	}
	w := ToFile(Simple{Base: "wave", Colors: []string{"#FF0000", "#0000FF"}, Speed: 2, Direction: "down"})
	if got := w.Layers[0]; got["speed"] != 2.0 || got["direction"] != "down" {
		t.Fatalf("got %#v", got)
	}
}

// A file Commander writes from the simple view must load.
func TestToFileLoads(t *testing.T) {
	l := ToFile(Simple{
		Base: "wave", Colors: []string{"#FF0000", "#0000FF"}, Speed: 1.5, Direction: "left",
		Ripple:    &Ripple{Colors: []string{"#FFFFFF"}},
		Highlight: &Highlight{Keys: []string{"W", "A", "S", "D"}, Color: "#00FF00"},
		States:    []State{{Name: "caps", Toggle: "CapsLock", Keys: []string{"CapsLock"}, Color: "#FF0000"}},
	})
	f := &layout.File{Profile: 0, Light: l, StoredLight: StoredToFile(Stored{Mode: "color", Color: "#102030", Brightness: intp(40)})}
	data, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := layout.LoadWith(t.TempDir(), map[string][]byte{"a.toml": data}); err != nil {
		t.Fatalf("load: %v\n%s", err, data)
	}
	back, err := layout.DecodeFile(data)
	if err != nil {
		t.Fatalf("decode: %v\n%s", err, data)
	}
	s, ok, reason := FromFile(back.Light)
	if !ok || s.Speed != 1.5 || len(s.States) != 1 {
		t.Fatalf("got %#v, %v, %q", s, ok, reason)
	}
}

func TestStored(t *testing.T) {
	for name, s := range map[string]Stored{
		"still":            {Mode: "still"},
		"still brightness": {Mode: "still", Brightness: intp(40)},
		"color brightness": {Mode: "color", Color: "#FF8000", Brightness: intp(75)},
		"color":            {Mode: "color", Color: "#FF8000"},
	} {
		t.Run(name, func(t *testing.T) {
			got := StoredFromFile(StoredToFile(s))
			if !reflect.DeepEqual(got, s) {
				t.Fatalf("got %#v, want %#v", got, s)
			}
		})
	}
	if st := StoredToFile(Stored{Mode: "still"}); st != nil {
		t.Fatalf("still without brightness: got %#v, want nil", st)
	}
	st := StoredToFile(Stored{Mode: "still", Color: "#FF0000", Brightness: intp(0)})
	if st == nil || st.Color != "" || st.Brightness == nil || *st.Brightness != 0 {
		t.Fatalf("still with brightness 0: got %#v", st)
	}
	if got := StoredFromFile(nil); got.Mode != "still" || got.Brightness != nil {
		t.Fatalf("StoredFromFile(nil) = %#v", got)
	}
	if got := StoredFromFile(&layout.FileStored{Color: "#ff8000"}); got.Color != "#FF8000" || got.Mode != "color" {
		t.Fatalf("lower-case colour: got %#v", got)
	}
}
