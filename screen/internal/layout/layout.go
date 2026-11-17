// SPDX-License-Identifier: GPL-2.0-or-later

// Package layout loads the configuration folder: one TOML file per profile.
package layout

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
)

// Mode says how a button's action is carried out.
type Mode int

const (
	// Tap presses each chord and releases it shortly after.
	Tap Mode = iota
	// Hold keeps one chord down for as long as the touch lasts.
	Hold
)

// Button is one configured button.
type Button struct {
	Page, Position int
	Title, Label   string
	Symbol         string // absolute path, or empty
	Picture        string // absolute path, or empty
	Color          color.NRGBA
	Mode           Mode
	Action         []action.Step
}

// Profile is the content of one layout file.
type Profile struct {
	Number            byte
	File              string
	SwitchWhenRunning string
	Buttons           []Button
}

// Config is a loaded configuration folder.
type Config struct {
	Dir      string
	Profiles []Profile
}

type rawFile struct {
	Profile           *int        `toml:"profile"`
	SwitchWhenRunning string      `toml:"switch_when_running"`
	Buttons           []rawButton `toml:"button"`
}

type rawButton struct {
	Page     *int     `toml:"page"`
	Position *int     `toml:"position"`
	Title    string   `toml:"title"`
	Label    string   `toml:"label"`
	Color    string   `toml:"color"`
	Symbol   string   `toml:"symbol"`
	Picture  string   `toml:"picture"`
	Action   []string `toml:"action"`
	Mode     string   `toml:"mode"`
}

var defaultColor = color.NRGBA{R: 0x00, G: 0xAA, B: 0xFF, A: 0xFF}

// maxProfile is the highest of the keyboard's five profiles.
const maxProfile = 4

// Load reads every *.toml file directly in dir, sorted by name.
func Load(dir string) (*Config, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".toml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no *.toml layout files in %s", abs)
	}
	cfg := &Config{Dir: abs}
	owner := map[byte]string{}
	for _, name := range names {
		p, err := loadFile(abs, filepath.Join(abs, name))
		if err != nil {
			return nil, err
		}
		if prev, ok := owner[p.Number]; ok {
			return nil, fmt.Errorf("%s and %s both define profile %d", prev, name, p.Number)
		}
		owner[p.Number] = name
		cfg.Profiles = append(cfg.Profiles, *p)
	}
	if len(cfg.Profiles) == maxProfile+1 {
		desktop := false
		for _, p := range cfg.Profiles {
			if p.SwitchWhenRunning == "" {
				desktop = true
			}
		}
		if !desktop {
			return nil, fmt.Errorf("every profile 0 to %d has switch_when_running, so no profile is left for the desktop; remove it from one layout", maxProfile)
		}
	}
	return cfg, nil
}

func loadFile(dir, path string) (*Profile, error) {
	name := filepath.Base(path)
	var raw rawFile
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("%s: unknown key(s): %s", name, strings.Join(keys, ", "))
	}
	if raw.Profile == nil {
		return nil, fmt.Errorf("%s: profile is required", name)
	}
	if *raw.Profile < 0 || *raw.Profile > maxProfile {
		return nil, fmt.Errorf("%s: profile must be 0 to %d, got %d", name, maxProfile, *raw.Profile)
	}
	p := &Profile{Number: byte(*raw.Profile), File: path, SwitchWhenRunning: raw.SwitchWhenRunning}
	seen := map[[2]int]bool{}
	for i, rb := range raw.Buttons {
		if rb.Page == nil || rb.Position == nil {
			return nil, fmt.Errorf("%s: button %d: page and position are required", name, i+1)
		}
		where := fmt.Sprintf("%s: page %d position %d", name, *rb.Page, *rb.Position)
		b, err := buildButton(dir, rb)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
		key := [2]int{b.Page, b.Position}
		if seen[key] {
			return nil, fmt.Errorf("%s: duplicate button", where)
		}
		seen[key] = true
		p.Buttons = append(p.Buttons, b)
	}
	return p, nil
}

func buildButton(dir string, rb rawButton) (Button, error) {
	b := Button{Page: *rb.Page, Position: *rb.Position, Title: rb.Title, Label: rb.Label, Color: defaultColor}
	if b.Page != 1 {
		return b, fmt.Errorf("page must be 1; pages 2 and 3 are not measured yet")
	}
	if b.Position < 0 || b.Position > 11 {
		return b, fmt.Errorf("position must be 0 to 11")
	}
	if len(b.Label) > 11 {
		return b, fmt.Errorf("label must be at most 11 characters")
	}
	for _, r := range b.Label {
		if r < 0x20 || r > 0x7e {
			return b, fmt.Errorf("label must be printable ASCII")
		}
	}
	if rb.Color != "" {
		c, err := parseColor(rb.Color)
		if err != nil {
			return b, err
		}
		b.Color = c
	}
	if rb.Symbol != "" && rb.Picture != "" {
		return b, fmt.Errorf("symbol and picture cannot both be set")
	}
	var err error
	if rb.Symbol != "" {
		if b.Symbol, err = resolveFile(dir, "symbol", rb.Symbol); err != nil {
			return b, err
		}
	}
	if rb.Picture != "" {
		if b.Picture, err = resolveFile(dir, "picture", rb.Picture); err != nil {
			return b, err
		}
	}
	if len(rb.Action) == 0 {
		return b, fmt.Errorf("action must hold at least one step")
	}
	for _, s := range rb.Action {
		st, err := action.ParseStep(s)
		if err != nil {
			return b, fmt.Errorf("action %q: %w", s, err)
		}
		b.Action = append(b.Action, st)
	}
	switch rb.Mode {
	case "", "tap":
		b.Mode = Tap
	case "hold":
		b.Mode = Hold
		if _, ok := b.Action[0].(action.Chord); len(b.Action) != 1 || !ok {
			return b, fmt.Errorf("hold needs exactly one step and it must be a key chord")
		}
	default:
		return b, fmt.Errorf("mode must be \"tap\" or \"hold\", got %q", rb.Mode)
	}
	return b, nil
}

func resolveFile(dir, what, rel string) (string, error) {
	p := filepath.Join(dir, filepath.FromSlash(rel))
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return "", fmt.Errorf("%s file %q does not exist", what, rel)
	}
	return p, nil
}

func parseColor(s string) (color.NRGBA, error) {
	h, ok := strings.CutPrefix(s, "#")
	if !ok || (len(h) != 6 && len(h) != 8) {
		return color.NRGBA{}, fmt.Errorf("color must be #RRGGBB or #RRGGBBAA, got %q", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("color must be #RRGGBB or #RRGGBBAA, got %q", s)
	}
	if len(h) == 6 {
		v = v<<8 | 0xFF
	}
	return color.NRGBA{R: byte(v >> 24), G: byte(v >> 16), B: byte(v >> 8), A: byte(v)}, nil
}

// DesktopProfile returns the profile the keyboard is to show while none of
// the programs named in switch_when_running runs: the lowest profile whose
// layout has no switch_when_running. When every layout has one, it is the
// lowest profile, 0 to 4, that no layout gives a program; that profile has no
// layout, and its touches are not acted on. Only when all five profiles have
// a program is it profile 0.
func (c *Config) DesktopProfile() byte {
	best := -1
	program := map[byte]bool{}
	for _, p := range c.Profiles {
		if p.SwitchWhenRunning != "" {
			program[p.Number] = true
		} else if best < 0 || int(p.Number) < best {
			best = int(p.Number)
		}
	}
	if best >= 0 {
		return byte(best)
	}
	for n := byte(0); n <= maxProfile; n++ {
		if !program[n] {
			return n
		}
	}
	return 0
}
