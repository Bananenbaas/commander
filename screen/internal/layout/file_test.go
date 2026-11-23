// SPDX-License-Identifier: GPL-2.0-or-later

package layout

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// richLayout uses every key of the format, with comments and odd key order.
const richLayout = `# my layout
switch_when_running = "Game.exe" # the program
name = "Rich"
profile = 1

[stored_light]
brightness = 60
color = "#102030"

[light]
fps = 20

[[light.layer]]
effect = "wave"
speed = 1.5
colors = ["#FF0000", "#0000FF"]
keys = ["W", "A", "S", "D"]
direction = "up"

[[light.layer]]
width = 3
effect = "ripple"
speed = 12.0
mode = "wave"
colors = ["#00FF00", "#FFFF00"]

[[light.layer]]
effect = "state"
name = "boost"
toggle = "LAlt+N"
keys = ["N"]
start = "on"
color = "#FFFFFF"

[[button]]
position = 0
page = 1
label = "Go"
symbol = "symbols/s.png"
action = ["LCtrl+A", "wait 100ms"]
state = "boost"

[[button]]
page = 1
position = 1
title = "Pic"
color = "#112233"
picture = "pictures/p.png"
action = ["F5"]

[[button]]
page = 2
position = 3
mode = "hold"
action = ["LShift"]

[[button]]
page = 3
position = 10
confirm = "hold 1s"
action = ["F12"]
`

func richFolder(t *testing.T) string {
	t.Helper()
	dir := folder(t, map[string]string{"a.toml": richLayout})
	for _, p := range []string{"symbols/s.png", "pictures/p.png"} {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestEncodeRoundTrip(t *testing.T) {
	dir := richFolder(t)
	want, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	f, err := DecodeFile([]byte(richLayout))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(enc), "speed = 12.0") {
		t.Errorf("a float must stay a float:\n%s", enc)
	}
	got, err := LoadWith(dir, map[string][]byte{"a.toml": enc})
	if err != nil {
		t.Fatalf("encoded file does not load: %v\n%s", err, enc)
	}
	// File holds the path of the file on disk; the contents differ only by that.
	if !reflect.DeepEqual(got.Profiles, want.Profiles) {
		t.Errorf("profiles differ after a round trip\n got: %+v\nwant: %+v\n%s", got.Profiles, want.Profiles, enc)
	}
}

func TestEncodeIsStable(t *testing.T) {
	f, err := DecodeFile([]byte(richLayout))
	if err != nil {
		t.Fatal(err)
	}
	first, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(first), Header+"\n") {
		t.Errorf("output must start with the header and a blank line:\n%s", first)
	}
	f2, err := DecodeFile(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := f2.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("encoding is not stable\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestLoadWithAddsAndReplaces(t *testing.T) {
	dir := folder(t, map[string]string{"a.toml": btn("")})
	repl := []byte(prof(1))
	game := []byte("name = \"X\"\nprogram = \"x.exe\"\n[devices]\nkb7 = 2\n")
	cfg, err := LoadWith(dir, map[string][]byte{"a.toml": repl, "games/x.toml": game})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) != 1 || cfg.Profiles[0].Number != 1 {
		t.Errorf("profiles = %+v, want the replaced profile 1", cfg.Profiles)
	}
	if len(cfg.Games) != 1 || cfg.Games[0].Name != "X" {
		t.Errorf("games = %+v, want the added game X", cfg.Games)
	}
	disk, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if disk.Profiles[0].Number != 0 || len(disk.Games) != 0 {
		t.Errorf("disk changed: %+v %+v", disk.Profiles, disk.Games)
	}
	if _, err := os.Stat(filepath.Join(dir, "games")); !os.IsNotExist(err) {
		t.Errorf("games/ must not appear on disk, stat err = %v", err)
	}
}

func TestLoadWithAddsALayout(t *testing.T) {
	dir := folder(t, map[string]string{"a.toml": btn("")})
	cfg, err := LoadWith(dir, map[string][]byte{"b.toml": []byte(prof(3))})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) != 2 || cfg.Profiles[1].Number != 3 {
		t.Errorf("profiles = %+v", cfg.Profiles)
	}
}

func TestDecodeFileRefusesUnknownKeys(t *testing.T) {
	_, err := DecodeFile([]byte("profile = 0\nbogus = 1\n"))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("err = %v, want one naming bogus", err)
	}
	_, err = DecodeGameFile([]byte("name = \"x\"\nbogus = 1\n"))
	if err == nil || !strings.Contains(err.Error(), "bogus") {
		t.Errorf("game err = %v, want one naming bogus", err)
	}
}

func TestGameFileEncode(t *testing.T) {
	dir := folder(t, map[string]string{"a.toml": btn("")})
	g := &GameFile{Name: "SC", Program: "StarCitizen.exe", Devices: map[string]any{"kb7": int64(2)}}
	enc, err := g.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(enc), Header+"\n") {
		t.Errorf("no header:\n%s", enc)
	}
	cfg, err := LoadWith(dir, map[string][]byte{"games/sc.toml": enc})
	if err != nil {
		t.Fatalf("%v\n%s", err, enc)
	}
	if len(cfg.Games) != 1 || cfg.Games[0].KB7 == nil || *cfg.Games[0].KB7 != 2 {
		t.Errorf("games = %+v, want KB7 2", cfg.Games)
	}
	back, err := DecodeGameFile(enc)
	if err != nil || !reflect.DeepEqual(back, g) {
		t.Errorf("DecodeGameFile = %+v, %v", back, err)
	}
}

// prof is a one-button layout for the given profile.
func prof(n int) string {
	return strings.Replace(btn(""), "profile = 0", "profile = "+string(rune('0'+n)), 1)
}

func TestCheckButtonFields(t *testing.T) {
	dir := t.TempDir()
	ok := FileButton{Page: 1, Position: 0, Action: []string{"LCtrl+A"}}
	if err := CheckButton(dir, ok, nil); err != nil {
		t.Fatalf("good button: %v", err)
	}
	cases := []struct {
		name   string
		mod    func(b *FileButton)
		states []string
		field  string
		step   int
	}{
		{"page", func(b *FileButton) { b.Page = 4 }, nil, "position", -1},
		{"label", func(b *FileButton) { b.Label = "twelve chars" }, nil, "label", -1},
		{"color", func(b *FileButton) { b.Color = "red" }, nil, "color", -1},
		{"symbol", func(b *FileButton) { b.Symbol = "symbols/none.png" }, nil, "symbol", -1},
		{"both", func(b *FileButton) { b.Symbol = "a.png"; b.Picture = "b.png" }, nil, "picture", -1},
		{"no action", func(b *FileButton) { b.Action = nil }, nil, "action", -1},
		{"bad step", func(b *FileButton) { b.Action = []string{"LCtrl+A", "bogus"} }, nil, "action", 1},
		{"hold two", func(b *FileButton) { b.Mode = "hold"; b.Action = []string{"A", "B"} }, nil, "mode", -1},
		{"confirm", func(b *FileButton) { b.Confirm = "hold 9s" }, nil, "confirm", -1},
		{"state", func(b *FileButton) { b.State = "x" }, []string{"y"}, "state", -1},
		{"state without light", func(b *FileButton) { b.State = "x" }, nil, "state", -1},
	}
	for _, c := range cases {
		b := ok
		c.mod(&b)
		err := CheckButton(dir, b, c.states)
		var fe *FieldError
		if !errors.As(err, &fe) {
			t.Errorf("%s: want a FieldError, got %v", c.name, err)
			continue
		}
		if fe.Field != c.field || fe.Step != c.step {
			t.Errorf("%s: field %q step %d, want %q %d (%v)", c.name, fe.Field, fe.Step, c.field, c.step, err)
		}
	}
	good := ok
	good.State = "x"
	if err := CheckButton(dir, good, []string{"x"}); err != nil {
		t.Errorf("declared state: %v", err)
	}
}

func TestLoadErrorCarriesField(t *testing.T) {
	dir := folder(t, map[string]string{"a.toml": "profile = 0\n[[button]]\npage = 1\nposition = 2\ncolor = \"red\"\naction = [\"A\"]\n"})
	_, err := Load(dir)
	var fe *FieldError
	if !errors.As(err, &fe) || fe.Field != "color" {
		t.Fatalf("want a color FieldError, got %v", err)
	}
	want := `a.toml: page 1 position 2: color must be #RRGGBB or #RRGGBBAA, got "red"`
	if err.Error() != want {
		t.Errorf("text %q, want %q", err.Error(), want)
	}
}

func TestLoadWithNilRemovesAFile(t *testing.T) {
	dir := folder(t, map[string]string{"a.toml": btn(""), "b.toml": prof(3)})
	if err := os.Mkdir(filepath.Join(dir, "games"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "games/x.toml", "name = \"X\"\nprogram = \"x.exe\"\n[devices]\nkb7 = 2\n")
	cfg, err := LoadWith(dir, map[string][]byte{"b.toml": nil, "games/x.toml": nil})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Profiles) != 1 || cfg.Profiles[0].Number != 0 {
		t.Errorf("profiles = %+v, want only a.toml", cfg.Profiles)
	}
	if len(cfg.Games) != 0 {
		t.Errorf("games = %+v, want none", cfg.Games)
	}
	disk, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(disk.Profiles) != 2 || len(disk.Games) != 1 {
		t.Errorf("disk changed: %+v %+v", disk.Profiles, disk.Games)
	}
}
