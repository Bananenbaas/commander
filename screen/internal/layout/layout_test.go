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

	"github.com/Bananenbaas/commander/screen/internal/action"
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

// TestLoadPages accepts buttons on pages 2 and 3, and the same position on
// different pages.
func TestLoadPages(t *testing.T) {
	dir := folder(t, map[string]string{"a.toml": "profile = 0\n" +
		"[[button]]\npage = 1\nposition = 4\naction = [\"A\"]\n" +
		"[[button]]\npage = 2\nposition = 4\naction = [\"B\"]\n" +
		"[[button]]\npage = 3\nposition = 10\naction = [\"C\"]\n"})
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	bs := c.Profiles[0].Buttons
	if len(bs) != 3 || bs[1].Page != 2 || bs[1].Position != 4 || bs[2].Page != 3 || bs[2].Position != 10 {
		t.Fatalf("buttons: %+v", bs)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"duplicate position", map[string]string{"a.toml": btn("") + "[[button]]\npage = 1\nposition = 2\naction = [\"B\"]\n"},
			[]string{"a.toml", "page 1 position 2"}},
		{"page 4", map[string]string{"a.toml": "profile = 0\n[[button]]\npage = 4\nposition = 4\naction = [\"A\"]\n"},
			[]string{"a.toml", "page 4 position 4", "page"}},
		{"page 3 position 11", map[string]string{"a.toml": "profile = 0\n[[button]]\npage = 3\nposition = 11\naction = [\"A\"]\n"},
			[]string{"a.toml", "page 3 position 11"}},
		{"duplicate on page 2", map[string]string{"a.toml": "profile = 0\n[[button]]\npage = 2\nposition = 4\naction = [\"A\"]\n[[button]]\npage = 2\nposition = 4\naction = [\"B\"]\n"},
			[]string{"a.toml", "page 2 position 4", "duplicate"}},
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

const lightTOML = `profile = 1

[light]
fps = 30

[[light.layer]]
effect = "wave"
colors = ["#0040FF", "#00C0FF", "#8000FF"]
speed = 0.5
direction = "right"

[[light.layer]]
effect = "ripple"
color = "#FFFFFF"
speed = 12
width = 1.5

[[light.layer]]
effect = "state"
name = "gear"
toggle = "N"
start = "on"
keys = ["N"]
color = "#30E070"

[[light.layer]]
effect = "state"
name = "quantum"
toggle = "B"
keys = ["B"]
color = "#00AAFF"
blink = 0
`

func stateButton(extra string) string {
	return "[[button]]\npage = 1\nposition = 1\n" + extra
}

func TestLoadLight(t *testing.T) {
	cfg, err := Load(folder(t, map[string]string{"a.toml": lightTOML + stateButton("state = \"gear\"\n")}))
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Profiles[0]
	if p.Light == nil {
		t.Fatal("Light is nil")
	}
	if p.Light.FPS() != 30 {
		t.Errorf("fps %d", p.Light.FPS())
	}
	st := p.Light.States()
	if len(st) != 2 || st[0].Name != "gear" || st[1].Name != "quantum" {
		t.Errorf("states %+v", st)
	}
	b := p.Buttons[0]
	if b.State != "gear" || len(b.Action) != 0 || b.Mode != Tap {
		t.Errorf("button %+v", b)
	}
}

func TestLoadWithoutLight(t *testing.T) {
	cfg, err := Load(folder(t, map[string]string{"a.toml": btn("")}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles[0].Light != nil {
		t.Error("Light set without [light]")
	}
}

func TestStateButtonWithAction(t *testing.T) {
	cfg, err := Load(folder(t, map[string]string{"a.toml": lightTOML + stateButton("state = \"quantum\"\naction = [\"B\"]\n")}))
	if err != nil {
		t.Fatal(err)
	}
	b := cfg.Profiles[0].Buttons[0]
	if b.State != "quantum" || len(b.Action) != 1 {
		t.Errorf("button %+v", b)
	}
}

func TestLightErrors(t *testing.T) {
	cases := []struct {
		name string
		toml string
		want []string
	}{
		{"unknown effect", "profile = 0\n[light]\n[[light.layer]]\neffect = \"sparkle\"\n",
			[]string{"a.toml", "light.layer 1", "sparkle"}},
		{"unknown setting", "profile = 0\n[light]\n[[light.layer]]\neffect = \"static\"\ncolor = \"#FFFFFF\"\nspeeed = 1\n",
			[]string{"a.toml", "light.layer 1", "speeed"}},
		{"second layer", "profile = 0\n[light]\n[[light.layer]]\neffect = \"static\"\ncolor = \"#FFFFFF\"\n[[light.layer]]\neffect = \"nope\"\n",
			[]string{"a.toml", "light.layer 2", "nope"}},
		{"no effect", "profile = 0\n[light]\n[[light.layer]]\ncolor = \"#FFFFFF\"\n",
			[]string{"a.toml", "light.layer 1", "effect"}},
		{"effect not a string", "profile = 0\n[light]\n[[light.layer]]\neffect = 3\n",
			[]string{"a.toml", "light.layer 1", "effect"}},
		{"keys not a list", "profile = 0\n[light]\n[[light.layer]]\neffect = \"static\"\ncolor = \"#FFFFFF\"\nkeys = \"W\"\n",
			[]string{"a.toml", "light.layer 1", "keys"}},
		{"keys not strings", "profile = 0\n[light]\n[[light.layer]]\neffect = \"static\"\ncolor = \"#FFFFFF\"\nkeys = [1]\n",
			[]string{"a.toml", "light.layer 1", "keys"}},
		{"no layers", "profile = 0\n[light]\nfps = 30\n",
			[]string{"a.toml", "light"}},
		{"bad fps", "profile = 0\n[light]\nfps = 99\n[[light.layer]]\neffect = \"static\"\ncolor = \"#FFFFFF\"\n",
			[]string{"a.toml", "light", "fps"}},
		{"state not defined", lightTOML + stateButton("state = \"shields\"\n"),
			[]string{"a.toml", "page 1 position 1", "shields"}},
		{"state without light", "profile = 0\n" + stateButton("state = \"gear\"\n"),
			[]string{"a.toml", "page 1 position 1", "gear"}},
		{"hold state without action", lightTOML + stateButton("state = \"gear\"\nmode = \"hold\"\n"),
			[]string{"a.toml", "page 1 position 1", "hold"}},
		{"no action no state", "profile = 0\n" + stateButton(""),
			[]string{"a.toml", "page 1 position 1", "action must hold at least one step"}},
	}
	for _, c := range cases {
		_, err := Load(folder(t, map[string]string{"a.toml": c.toml}))
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

func TestConfirmHold(t *testing.T) {
	cfg, err := Load(folder(t, map[string]string{"a.toml": "profile = 0\n" + stateButton("action = [\"B\"]\nconfirm = \"hold 1s\"\n")}))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Profiles[0].Buttons[0].Confirm; got != time.Second {
		t.Errorf("Confirm = %v, want 1s", got)
	}
	for _, edge := range []string{"hold 200ms", "hold 5s"} {
		if _, err := Load(folder(t, map[string]string{"a.toml": "profile = 0\n" + stateButton("action = [\"B\"]\nconfirm = \""+edge+"\"\n")})); err != nil {
			t.Errorf("%s: %v", edge, err)
		}
	}
	cfg, err = Load(folder(t, map[string]string{"a.toml": "profile = 0\n" + stateButton("action = [\"B\"]\n")}))
	if err != nil || cfg.Profiles[0].Buttons[0].Confirm != 0 {
		t.Errorf("default: %v %+v", err, cfg)
	}
}

func TestConfirmErrors(t *testing.T) {
	const form = `confirm must be "hold <duration>", e.g. "hold 1s"`
	cases := []struct {
		name, extra, want string
	}{
		{"no word", "action = [\"B\"]\nconfirm = \"1s\"\n", form},
		{"two spaces", "action = [\"B\"]\nconfirm = \"hold  1s\"\n", form},
		{"bad duration", "action = [\"B\"]\nconfirm = \"hold soon\"\n", form},
		{"empty", "action = [\"B\"]\nconfirm = \"hold\"\n", form},
		{"too short", "action = [\"B\"]\nconfirm = \"hold 100ms\"\n", "200ms"},
		{"too long", "action = [\"B\"]\nconfirm = \"hold 6s\"\n", "5s"},
		{"hold mode", "action = [\"B\"]\nmode = \"hold\"\nconfirm = \"hold 1s\"\n", "confirm"},
	}
	for _, c := range cases {
		_, err := Load(folder(t, map[string]string{"a.toml": "profile = 0\n" + stateButton(c.extra)}))
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.Contains(err.Error(), "page 1 position 1") {
			t.Errorf("%s: error %v, want it to contain %q", c.name, err, c.want)
		}
	}
	_, err := Load(folder(t, map[string]string{"a.toml": lightTOML + stateButton("state = \"gear\"\nconfirm = \"hold 1s\"\n")}))
	if err == nil || !strings.Contains(err.Error(), "confirm") {
		t.Errorf("state-only: %v", err)
	}
}

func TestLoadStoredLight(t *testing.T) {
	cfg, err := Load(folder(t, map[string]string{"a.toml": btn("") + "[stored_light]\ncolor = \"#FFFFFF\"\nbrightness = 80\n"}))
	if err != nil {
		t.Fatal(err)
	}
	sl := cfg.Profiles[0].StoredLight
	if sl == nil {
		t.Fatal("StoredLight is nil")
	}
	if sl.Color != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) || sl.Brightness != 80 {
		t.Errorf("stored light %+v", *sl)
	}
}

func TestLoadStoredLightOnly(t *testing.T) {
	cfg, err := Load(folder(t, map[string]string{"a.toml": "profile = 3\n[stored_light]\ncolor = \"#1C36FF\"\n"}))
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Profiles[0]
	if p.Number != 3 || len(p.Buttons) != 0 || p.Light != nil || p.StoredLight == nil {
		t.Fatalf("profile %+v", p)
	}
	if p.StoredLight.Color != (color.NRGBA{R: 0x1c, G: 0x36, B: 0xff, A: 255}) || p.StoredLight.Brightness != -1 {
		t.Errorf("stored light %+v", *p.StoredLight)
	}
}

func TestLoadStoredLightBrightnessOnly(t *testing.T) {
	cfg, err := Load(folder(t, map[string]string{"a.toml": "profile = 3\n[stored_light]\nbrightness = 0\n"}))
	if err != nil {
		t.Fatal(err)
	}
	sl := cfg.Profiles[0].StoredLight
	if sl == nil || sl.Color.A != 0 || sl.Brightness != 0 {
		t.Errorf("stored light %+v", sl)
	}
}

func TestLoadWithoutStoredLight(t *testing.T) {
	cfg, err := Load(folder(t, map[string]string{"a.toml": btn("")}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles[0].StoredLight != nil {
		t.Error("StoredLight set without [stored_light]")
	}
}

func TestLoadName(t *testing.T) {
	cfg, err := Load(folder(t, map[string]string{"a.toml": "profile = 1\nname = \"SC\"\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Profiles[0].Name; got != "SC" {
		t.Errorf("Name = %q", got)
	}
	cfg, err = Load(folder(t, map[string]string{"a.toml": btn("")}))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Profiles[0].Name; got != "" {
		t.Errorf("Name = %q without name", got)
	}
}

func TestNameErrors(t *testing.T) {
	for name, toml := range map[string]string{
		"20 characters": "profile = 1\nname = \"" + strings.Repeat("a", 20) + "\"\n",
		"empty":         "profile = 1\nname = \"\"\n",
		"non-ASCII":     "profile = 1\nname = \"café\"\n",
	} {
		_, err := Load(folder(t, map[string]string{"a.toml": toml}))
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		if !strings.Contains(err.Error(), "a.toml") || !strings.Contains(err.Error(), "name") {
			t.Errorf("%s: error %q does not name the file and key", name, err)
		}
	}
}

func TestStoredLightErrors(t *testing.T) {
	cases := []struct {
		name, toml string
		want       []string
	}{
		{"brightness 101", "profile = 0\n[stored_light]\ncolor = \"#FFFFFF\"\nbrightness = 101\n", []string{"a.toml", "stored_light", "brightness"}},
		{"brightness -1", "profile = 0\n[stored_light]\ncolor = \"#FFFFFF\"\nbrightness = -1\n", []string{"a.toml", "stored_light", "brightness"}},
		{"bad colour", "profile = 0\n[stored_light]\ncolor = \"white\"\n", []string{"a.toml", "stored_light", "color"}},
		{"transparent colour", "profile = 0\n[stored_light]\ncolor = \"#FFFFFF80\"\n", []string{"a.toml", "stored_light", "color"}},
		{"empty", "profile = 0\n[stored_light]\n", []string{"a.toml", "stored_light"}},
		{"unknown key", "profile = 0\n[stored_light]\ncolour = \"#FFFFFF\"\n", []string{"a.toml", "colour"}},
	}
	for _, c := range cases {
		_, err := Load(folder(t, map[string]string{"a.toml": c.toml}))
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
