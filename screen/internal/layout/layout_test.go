// SPDX-License-Identifier: GPL-2.0-or-later

package layout

import (
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
)

const desktopTOML = `
profile = 0

[[button]]
page = 1
position = 0
title = "Browser"
color = "#FFAA00"
action = ["open https://example.org"]
`

const scTOML = `
profile = 1
switch_when_running = "Game.exe"

[[button]]
page = 1
position = 3
title = "Flight Ready"
action = ["U", "wait 600ms", "I"]

[[button]]
page = 1
position = 5
label = "eject"
action = ["AltGr+Y"]
mode = "hold"
`

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func folder(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for n, c := range files {
		write(t, dir, n, c)
	}
	return dir
}

func TestLoad(t *testing.T) {
	dir := folder(t, map[string]string{"desktop.toml": desktopTOML, "sc.toml": scTOML})
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Profiles) != 2 || c.Profiles[0].Number != 0 || c.Profiles[1].Number != 1 {
		t.Fatalf("profiles: %+v", c.Profiles)
	}
	if c.DesktopProfile() != 0 {
		t.Fatal("desktop profile")
	}
	b := c.Profiles[0].Buttons[0]
	if b.Color != (color.NRGBA{0xFF, 0xAA, 0x00, 0xFF}) || b.Mode != Tap || b.Title != "Browser" {
		t.Fatalf("button: %+v", b)
	}
	sc := c.Profiles[1]
	if sc.SwitchWhenRunning != "Game.exe" || filepath.Base(sc.File) != "sc.toml" {
		t.Fatalf("profile: %+v", sc)
	}
	if sc.Buttons[0].Color != (color.NRGBA{0x00, 0xAA, 0xFF, 0xFF}) {
		t.Fatalf("default colour: %v", sc.Buttons[0].Color)
	}
	if got := sc.Buttons[0].Action[1]; got != (action.Wait{D: 600 * time.Millisecond}) {
		t.Fatalf("step: %#v", got)
	}
	if sc.Buttons[1].Mode != Hold || sc.Buttons[1].Label != "eject" {
		t.Fatalf("hold button: %+v", sc.Buttons[1])
	}
}

func TestDesktopProfileFallback(t *testing.T) {
	c := &Config{Profiles: []Profile{
		{Number: 2, SwitchWhenRunning: "a"}, {Number: 1, SwitchWhenRunning: "b"},
	}}
	if c.DesktopProfile() != 1 {
		t.Fatal("lowest profile expected")
	}
	c.Profiles = append(c.Profiles, Profile{Number: 4})
	if c.DesktopProfile() != 4 {
		t.Fatal("profile without switch expected")
	}
}

func TestSymbolPathResolved(t *testing.T) {
	dir := folder(t, map[string]string{"a.toml": `
profile = 0
[[button]]
page = 1
position = 1
symbol = "symbols/x.png"
action = ["A"]
`})
	os.Mkdir(filepath.Join(dir, "symbols"), 0o755)
	write(t, dir, "symbols/x.png", "x")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := c.Profiles[0].Buttons[0].Symbol
	if !filepath.IsAbs(s) || filepath.Base(s) != "x.png" {
		t.Fatalf("symbol: %q", s)
	}
}

func btn(extra string) string {
	return "profile = 0\n[[button]]\npage = 1\nposition = 2\naction = [\"A\"]\n" + extra
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"duplicate position", map[string]string{"a.toml": btn("") + "[[button]]\npage = 1\nposition = 2\naction = [\"B\"]\n"},
			[]string{"a.toml", "page 1 position 2"}},
		{"page 2", map[string]string{"a.toml": "profile = 0\n[[button]]\npage = 2\nposition = 4\naction = [\"A\"]\n"},
			[]string{"a.toml", "page 2 position 4", "not measured"}},
		{"position 12", map[string]string{"a.toml": "profile = 0\n[[button]]\npage = 1\nposition = 12\naction = [\"A\"]\n"},
			[]string{"a.toml", "page 1 position 12"}},
		{"symbol and picture", map[string]string{"a.toml": btn("symbol = \"s.png\"\npicture = \"p.png\"\n"), "s.png": "x", "p.png": "x"},
			[]string{"a.toml", "page 1 position 2", "symbol", "picture"}},
		{"missing picture", map[string]string{"a.toml": btn("picture = \"nope.png\"\n")},
			[]string{"a.toml", "page 1 position 2", "nope.png"}},
		{"long label", map[string]string{"a.toml": btn("label = \"123456789012\"\n")},
			[]string{"a.toml", "page 1 position 2", "label"}},
		{"hold two chords", map[string]string{"a.toml": "profile = 0\n[[button]]\npage = 1\nposition = 2\nmode = \"hold\"\naction = [\"A\", \"B\"]\n"},
			[]string{"a.toml", "page 1 position 2", "hold"}},
		{"hold wait", map[string]string{"a.toml": "profile = 0\n[[button]]\npage = 1\nposition = 2\nmode = \"hold\"\naction = [\"wait 5ms\"]\n"},
			[]string{"a.toml", "hold"}},
		{"same profile", map[string]string{"a.toml": btn(""), "b.toml": btn("")},
			[]string{"a.toml", "b.toml", "profile 0"}},
		{"font key", map[string]string{"a.toml": "font = \"x.ttf\"\n" + btn("")},
			[]string{"a.toml", "font"}},
		{"unknown button key", map[string]string{"a.toml": btn("colour = \"#fff\"\n")},
			[]string{"a.toml", "colour"}},
		{"bad step", map[string]string{"a.toml": "profile = 0\n[[button]]\npage = 1\nposition = 2\naction = [\"Foo\"]\n"},
			[]string{"a.toml", "page 1 position 2", "Foo"}},
		{"bad colour", map[string]string{"a.toml": btn("color = \"red\"\n")},
			[]string{"a.toml", "color"}},
		{"missing profile", map[string]string{"a.toml": "[[button]]\npage = 1\nposition = 2\naction = [\"A\"]\n"},
			[]string{"a.toml", "profile"}},
		{"profile 5", map[string]string{"a.toml": "profile = 5\n"},
			[]string{"a.toml", "profile"}},
		{"empty action", map[string]string{"a.toml": "profile = 0\n[[button]]\npage = 1\nposition = 2\naction = []\n"},
			[]string{"a.toml", "action"}},
		{"no files", map[string]string{}, []string{"toml"}},
	}
	for _, c := range cases {
		_, err := Load(folder(t, c.files))
		if err == nil {
			t.Errorf("%s: no error", c.name)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: error %q lacks %q", c.name, err, w)
			}
		}
	}
}
