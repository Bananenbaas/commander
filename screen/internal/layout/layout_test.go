// SPDX-License-Identifier: GPL-2.0-or-later

package layout

import (
	"fmt"
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
	for _, c := range []struct {
		name     string
		profiles []Profile
		want     byte
	}{
		{"a desktop layout", []Profile{{Number: 3, SwitchWhenRunning: "a"}, {Number: 2}, {Number: 4}}, 2},
		{"a desktop layout above a free profile", []Profile{{Number: 4, SwitchWhenRunning: "a"}, {Number: 1}}, 1},
		{"a game layout only", []Profile{{Number: 1, SwitchWhenRunning: "game.exe"}}, 0},
		{"game layouts only", []Profile{{Number: 1, SwitchWhenRunning: "b"}, {Number: 0, SwitchWhenRunning: "a"}, {Number: 3, SwitchWhenRunning: "c"}}, 2},
		{"every profile has a program", []Profile{
			{Number: 0, SwitchWhenRunning: "a"}, {Number: 1, SwitchWhenRunning: "b"}, {Number: 2, SwitchWhenRunning: "c"},
			{Number: 3, SwitchWhenRunning: "d"}, {Number: 4, SwitchWhenRunning: "e"},
		}, 0},
		{"no layouts", nil, 0},
	} {
		cfg := &Config{Profiles: c.profiles}
		if got := cfg.DesktopProfile(); got != c.want {
			t.Errorf("%s: DesktopProfile() = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestLoadRefusesNoDesktopProfile(t *testing.T) {
	files := map[string]string{}
	for n := 0; n <= 4; n++ {
		files[fmt.Sprintf("p%d.toml", n)] = fmt.Sprintf("profile = %d\nswitch_when_running = \"g%d.exe\"\n", n, n)
	}
	_, err := Load(folder(t, files))
	if err == nil || !strings.Contains(err.Error(), "desktop") {
		t.Fatalf("Load = %v, want an error about the desktop profile", err)
	}
	// One layout without a program is enough.
	files["p2.toml"] = "profile = 2\n"
	if _, err := Load(folder(t, files)); err != nil {
		t.Fatalf("Load with a desktop layout: %v", err)
	}
}

func TestDesktopProfileOfAGameOnlyFolder(t *testing.T) {
	dir := folder(t, map[string]string{"game.toml": `
profile = 1
switch_when_running = "game.exe"

[[button]]
page = 1
position = 0
title = "Power"
action = ["P"]
`})
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.DesktopProfile(); got != 0 {
		t.Fatalf("DesktopProfile() = %d, want 0: the game's profile is never left", got)
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
