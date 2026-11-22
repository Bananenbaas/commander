// SPDX-License-Identifier: GPL-2.0-or-later

package store

import (
	"errors"
	"fmt"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/action"
	"github.com/Bananenbaas/commander/screen/internal/layout"
)

func steps(t *testing.T, ss ...string) []action.Step {
	t.Helper()
	var out []action.Step
	for _, s := range ss {
		st, err := action.ParseStep(s)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, st)
	}
	return out
}

func TestDecide(t *testing.T) {
	red := color.NRGBA{R: 0xFF, A: 0xFF}
	base := func() *layout.Profile {
		return &layout.Profile{Number: 1, File: "kb7-sc.toml", Buttons: []layout.Button{
			{Page: 1, Position: 0, Title: "Gear", Label: "GEAR", Color: red, Action: steps(t, "G")},
			{Page: 1, Position: 1, Title: "Boost", Color: red, Mode: layout.Hold, Action: steps(t, "LShift")},
			{Page: 2, Position: 0, Title: "Light", Color: red, State: "gear", Picture: "p.png"},
		}}
	}
	cases := []struct {
		name   string
		change func(p *layout.Profile)
		want   Change
	}{
		{"same", func(p *layout.Profile) {}, NoChange},
		{"action", func(p *layout.Profile) { p.Buttons[0].Action = steps(t, "H") }, Reload},
		{"mode", func(p *layout.Profile) { p.Buttons[1].Mode = layout.Tap }, Reload},
		{"confirm", func(p *layout.Profile) { p.Buttons[0].Confirm = time.Second }, Reload},
		{"state", func(p *layout.Profile) { p.Buttons[2].State = "quantum" }, Reload},
		{"title", func(p *layout.Profile) { p.Buttons[0].Title = "Wheels" }, Apply},
		{"colour", func(p *layout.Profile) { p.Buttons[0].Color = color.NRGBA{B: 0xFF, A: 0xFF} }, Apply},
		{"symbol", func(p *layout.Profile) { p.Buttons[0].Symbol = "s.png" }, Apply},
		{"picture", func(p *layout.Profile) { p.Buttons[2].Picture = "q.png" }, Apply},
		{"label", func(p *layout.Profile) { p.Buttons[0].Label = "WHEELS" }, Apply},
		{"added", func(p *layout.Profile) {
			p.Buttons = append(p.Buttons, layout.Button{Page: 3, Position: 0, Action: steps(t, "A")})
		}, Apply},
		{"removed", func(p *layout.Profile) { p.Buttons = p.Buttons[:2] }, Apply},
		{"moved", func(p *layout.Profile) { p.Buttons[1].Position = 5 }, Apply},
		{"reordered only", func(p *layout.Profile) { p.Buttons[0], p.Buttons[1] = p.Buttons[1], p.Buttons[0] }, NoChange},
		{"action and title", func(p *layout.Profile) {
			p.Buttons[0].Action = steps(t, "H")
			p.Buttons[1].Title = "Afterburner"
		}, Apply},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := base()
			c.change(p)
			if got := Decide(base(), p); got != c.want {
				t.Fatalf("Decide = %v, want %v", got, c.want)
			}
		})
	}
	if got := Decide(nil, base()); got != Apply {
		t.Errorf("Decide(nil, p) = %v, want apply", got)
	}
	for c, s := range map[Change]string{NoChange: "none", Reload: "reload", Apply: "apply"} {
		if c.String() != s {
			t.Errorf("%d.String() = %q, want %q", c, c.String(), s)
		}
	}
}

const handSC = `# Star Citizen, written by hand.
profile = 1
name = "SC"   # shown on the keyboard

[light]
fps = 30

[[light.layer]]
effect = "state"
name = "gear"
toggle = "N"
keys = ["N"]
color = "#30E070"

[stored_light]
color = "#112233"
brightness = 50

# The landing gear.
[[button]]
page = 1
position = 0
title = "Gear"
label = "GEAR"
color = "#FF0000"
action = ["N"]
state = "gear"
confirm = "hold 1s"

[[button]]
page = 1
position = 1
title = "Boost"
mode = "hold"
action = ["LShift"]

[[button]]
page = 2
position = 3
title = "Hail"
action = ["open https://example.org", "wait 600ms", "LCtrl+H"]
`

const handDesk = "profile = 0\n[[button]]\npage = 1\nposition = 0\ntitle = \"Mail\"\naction = [\"A\"]\n"

func handFolder(t *testing.T) string {
	return folder(t, map[string]string{"kb7-desk.toml": handDesk, "kb7-sc.toml": handSC})
}

// buttonsOf returns the buttons of the layout file rel and the file's version.
func buttonsOf(t *testing.T, dir, rel string) ([]layout.FileButton, string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}
	f, err := layout.DecodeFile(data)
	if err != nil {
		t.Fatal(err)
	}
	return f.Buttons, Version(data)
}

func mustLoad(t *testing.T, dir string) *layout.Config {
	t.Helper()
	cfg, err := layout.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// snapshot returns every file below dir by relative path, with its bytes.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		m[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSaveButtons(t *testing.T) {
	dir := handFolder(t)
	before := mustLoad(t, dir)
	buttons, base := buttonsOf(t, dir, "kb7-sc.toml")
	buttons[1].Title = "Afterburner"

	res, err := SaveButtons(dir, 1, base, buttons)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "kb7-sc.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), layout.Header) {
		t.Errorf("saved file does not start with the header:\n%s", data)
	}
	if res.Version != Version(data) {
		t.Errorf("Version = %q, want %q", res.Version, Version(data))
	}
	if res.Change != Apply {
		t.Errorf("Change = %v, want apply", res.Change)
	}
	if res.Migrated {
		t.Error("Migrated without any switch_when_running")
	}
	if len(res.Backups) != 1 || !strings.HasPrefix(res.Backups[0], "backup/kb7-sc-") {
		t.Fatalf("Backups = %v", res.Backups)
	}
	if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(res.Backups[0]))); err != nil || string(b) != handSC {
		t.Fatalf("backup does not hold the original (%v)", err)
	}

	after := mustLoad(t, dir)
	old, _ := layout.DecodeFile([]byte(handSC))
	saved, err := layout.DecodeFile(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old.Light, saved.Light) {
		t.Errorf("light changed:\n%+v\n%+v", old.Light, saved.Light)
	}
	for i := range before.Profiles {
		before.Profiles[i].Light, after.Profiles[i].Light = nil, nil
	}
	before.Profiles[1].Buttons[1].Title = "Afterburner"
	if !reflect.DeepEqual(before.Profiles, after.Profiles) {
		t.Errorf("profiles differ beyond the title:\nbefore %+v\nafter  %+v", before.Profiles, after.Profiles)
	}
	if desk, _ := os.ReadFile(filepath.Join(dir, "kb7-desk.toml")); string(desk) != handDesk {
		t.Error("the other layout was rewritten")
	}
}

func TestSaveButtonsMigratesOnce(t *testing.T) {
	dir := migrationFolder(t, nil)
	buttons, base := buttonsOf(t, dir, "kb7-0.toml")
	buttons[0].Title = "Mail"
	res, err := SaveButtons(dir, 0, base, buttons)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Migrated || res.Change < Reload {
		t.Fatalf("first save: Migrated %v, Change %v", res.Migrated, res.Change)
	}
	if len(res.Backups) != 3 {
		t.Errorf("first save backed up %v, want the three hand-written layouts", res.Backups)
	}
	cfg := mustLoad(t, dir)
	for _, p := range cfg.Profiles {
		if p.SwitchWhenRunning != "" {
			t.Errorf("%s keeps switch_when_running", filepath.Base(p.File))
		}
	}
	if p, _ := cfg.Choose([]string{"StarCitizen.exe"}); p != 1 {
		t.Errorf("StarCitizen.exe chooses %d, want 1", p)
	}
	if cfg.Desktop == nil || *cfg.Desktop.KB7 != 0 {
		t.Errorf("Desktop = %+v", cfg.Desktop)
	}

	buttons[0].Title = "Inbox"
	res, err = SaveButtons(dir, 0, res.Version, buttons)
	if err != nil {
		t.Fatal(err)
	}
	if res.Migrated {
		t.Error("second save migrated again")
	}
	if len(res.Backups) != 0 {
		t.Errorf("second save made backups %v", res.Backups)
	}
	if res.Change != Apply {
		t.Errorf("second save Change = %v, want apply", res.Change)
	}
}

func TestSaveButtonsRefuses(t *testing.T) {
	// Fifteen different pictures here and the desktop layout's one.
	sixteen := func(t *testing.T, dir string) []layout.FileButton {
		var bs []layout.FileButton
		for i := 0; i < 15; i++ {
			bs = append(bs, layout.FileButton{Page: 1 + i/12, Position: i % 12, Title: fmt.Sprintf("B%d", i), Action: []string{"A"}})
		}
		return bs
	}
	badColour := func(t *testing.T, dir string) []layout.FileButton {
		bs, _ := buttonsOf(t, dir, "kb7-sc.toml")
		bs[1].Color = "red"
		return bs
	}
	cases := []struct {
		name    string
		dir     func(t *testing.T) string
		slot    byte
		stale   bool
		buttons func(t *testing.T, dir string) []layout.FileButton
		check   func(t *testing.T, err error)
	}{
		{"stale version", handFolder, 1, true,
			func(t *testing.T, dir string) []layout.FileButton {
				bs, _ := buttonsOf(t, dir, "kb7-sc.toml")
				return bs
			},
			func(t *testing.T, err error) {
				if !errors.Is(err, ErrChanged) {
					t.Fatalf("err = %v, want ErrChanged", err)
				}
			}},
		{"bad colour", handFolder, 1, false, badColour, func(t *testing.T, err error) {
			var be *ButtonError
			if !errors.As(err, &be) || be.Page != 1 || be.Position != 1 || be.Err.Field != "color" {
				t.Fatalf("err = %#v, want a ButtonError for page 1 position 1, field color", err)
			}
		}},
		{"bad colour before migrating", func(t *testing.T) string {
			return migrationFolder(t, map[string]string{"kb7-sc.toml": "profile = 3\n[[button]]\npage = 1\nposition = 0\naction = [\"A\"]\n[[button]]\npage = 1\nposition = 1\naction = [\"A\"]\n"})
		}, 3, false, badColour, func(t *testing.T, err error) {
			var be *ButtonError
			if !errors.As(err, &be) || be.Err.Field != "color" {
				t.Fatalf("err = %v, want a ButtonError for color", err)
			}
		}},
		{"duplicate position", handFolder, 1, false, func(t *testing.T, dir string) []layout.FileButton {
			bs, _ := buttonsOf(t, dir, "kb7-sc.toml")
			bs[1].Position = 0
			return bs
		}, func(t *testing.T, err error) {
			var be *ButtonError
			if !errors.As(err, &be) || be.Page != 1 || be.Position != 0 || be.Err.Field != "position" {
				t.Fatalf("err = %v, want a ButtonError for the position", err)
			}
		}},
		{"another layout has an error", func(t *testing.T) string {
			return folder(t, map[string]string{
				"kb7-sc.toml":  handSC,
				"kb7-bad.toml": "profile = 2\n[[button]]\npage = 1\nposition = 0\ncolor = \"red\"\naction = [\"A\"]\n",
			})
		}, 1, false, func(t *testing.T, dir string) []layout.FileButton {
			bs, _ := buttonsOf(t, dir, "kb7-sc.toml")
			return bs
		},
			func(t *testing.T, err error) {
				want := `kb7-bad.toml has an error, fix it first: kb7-bad.toml: page 1 position 0: color must be #RRGGBB or #RRGGBBAA, got "red"`
				var be *ButtonError
				if err == nil || err.Error() != want || errors.As(err, &be) {
					t.Fatalf("err = %v, want %q", err, want)
				}
			}},
		{"sixteen pictures", handFolder, 1, false, sixteen, func(t *testing.T, err error) {
			if want := "The KB7 holds 15 different pictures; this would be 16."; err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		}},
		{"picture that does not render", func(t *testing.T) string {
			return folder(t, map[string]string{"kb7-sc.toml": handSC, "pictures/bad.png": "not a PNG"})
		}, 1, false, func(t *testing.T, dir string) []layout.FileButton {
			bs, _ := buttonsOf(t, dir, "kb7-sc.toml")
			bs[2].Picture = "pictures/bad.png"
			return bs
		}, func(t *testing.T, err error) {
			var be *ButtonError
			if !errors.As(err, &be) || be.Page != 2 || be.Position != 3 || be.Err.Field != "picture" {
				t.Fatalf("err = %v, want a ButtonError for the picture of page 2 position 3", err)
			}
		}},
		{"no layout", handFolder, 4, false, sixteen, func(t *testing.T, err error) {
			if want := "There is no layout for profile 5."; err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := c.dir(t)
			base := "0000000000000000"
			if !c.stale {
				for _, n := range names(t, dir) {
					data, _ := os.ReadFile(filepath.Join(dir, n))
					if f, err := layout.DecodeFile(data); err == nil && f.Profile == int(c.slot) {
						base = Version(data)
					}
				}
			}
			bs := c.buttons(t, dir)
			before := snapshot(t, dir)
			_, err := SaveButtons(dir, c.slot, base, bs)
			c.check(t, err)
			if after := snapshot(t, dir); !reflect.DeepEqual(before, after) {
				t.Fatalf("the folder changed:\nbefore %v\nafter  %v", keys(before), keys(after))
			}
		})
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSaveButtonsInterrupted(t *testing.T) {
	dir := migrationFolder(t, nil)
	broken := errors.New("disk full")
	orig := newSaveWriter
	t.Cleanup(func() { newSaveWriter = orig })
	newSaveWriter = func(dir string) *writer {
		w := newWriter(dir)
		calls := 0
		next := w.writeFile
		w.writeFile = func(path string, data []byte) error {
			calls++
			if calls == 3 {
				return broken
			}
			return next(path, data)
		}
		return w
	}

	buttons, base := buttonsOf(t, dir, "kb7-0.toml")
	buttons[0].Title = "Mail"
	res, err := SaveButtons(dir, 0, base, buttons)
	if !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the write error", err)
	}
	if len(res.Backups) != 1 {
		t.Errorf("Backups = %v, want the one made before the failing write", res.Backups)
	}
	if !res.Written {
		t.Error("Written = false, but files were written before the failure")
	}
	mustLoad(t, dir)
	for rel := range snapshot(t, dir) {
		if strings.Contains(rel, ".tmp-") {
			t.Errorf("temporary file %s left behind", rel)
		}
	}
}

func TestSaveButtonsFirstWriteFails(t *testing.T) {
	dir := migrationFolder(t, nil)
	broken := errors.New("disk full")
	orig := newSaveWriter
	t.Cleanup(func() { newSaveWriter = orig })
	newSaveWriter = func(dir string) *writer {
		w := newWriter(dir)
		w.writeFile = func(string, []byte) error { return broken }
		return w
	}
	buttons, base := buttonsOf(t, dir, "kb7-0.toml")
	buttons[0].Title = "Mail"
	res, err := SaveButtons(dir, 0, base, buttons)
	if !errors.Is(err, broken) {
		t.Fatalf("err = %v, want the write error", err)
	}
	if res.Written {
		t.Error("Written = true, but no file was written")
	}
}
