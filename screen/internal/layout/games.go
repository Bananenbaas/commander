// SPDX-License-Identifier: GPL-2.0-or-later

package layout

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Game is one game profile: a program to watch and the keyboard profile to
// show while it runs.
type Game struct {
	Name    string
	File    string // absolute path; for a game built from a layout, that layout's path
	Program string // "" only for the Desktop game
	KB7     *byte  // KB7 profile 0-4; nil = don't switch
}

// gamesFolder is the folder of game profiles inside the configuration folder.
const gamesFolder = "games"

// desktopGameFile is the file in games/ that describes the Desktop game.
const desktopGameFile = "desktop.toml"

type rawGame struct {
	Name    string         `toml:"name"`
	Program *string        `toml:"program"`
	Devices map[string]any `toml:"devices"`
}

// programKey normalises a program name the way procs.Running compares them:
// lower case, without directory parts, ".exe" optional.
func programKey(s string) string {
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSuffix(strings.ToLower(s), ".exe")
}

// loadGames reads dir/games, if there is one, and adds a game for every layout
// program that no game file claims. Errors name the file as games/<file>.toml.
func loadGames(dir string, profiles []Profile) (games []Game, desktop *Game, err error) {
	gdir := filepath.Join(dir, gamesFolder)
	var names []string
	if fi, serr := os.Stat(gdir); serr == nil && fi.IsDir() {
		entries, err := os.ReadDir(gdir)
		if err != nil {
			return nil, nil, err
		}
		for _, e := range entries {
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".toml") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
	} else if serr != nil && !os.IsNotExist(serr) {
		return nil, nil, serr
	}

	byProgram := map[string]string{} // program key -> file label
	byName := map[string]string{}    // lower-case name -> file label
	for _, fn := range names {
		label := gamesFolder + "/" + fn
		g, err := loadGame(filepath.Join(gdir, fn), label, strings.EqualFold(fn, desktopGameFile))
		if err != nil {
			return nil, nil, err
		}
		if prev, ok := byName[strings.ToLower(g.Name)]; ok {
			return nil, nil, fmt.Errorf("%s and %s both use the name %q", prev, label, g.Name)
		}
		byName[strings.ToLower(g.Name)] = label
		if g.Program == "" {
			desktop = g
			continue
		}
		if prev, ok := byProgram[programKey(g.Program)]; ok {
			return nil, nil, fmt.Errorf("%s and %s both name the program %q", prev, label, g.Program)
		}
		byProgram[programKey(g.Program)] = label
		games = append(games, *g)
	}

	var fromLayouts []Profile
	for _, p := range profiles {
		if p.SwitchWhenRunning == "" {
			continue
		}
		layoutFile := filepath.Base(p.File)
		if prev, ok := byProgram[programKey(p.SwitchWhenRunning)]; ok {
			return nil, nil, fmt.Errorf("%s: switch_when_running %q is also the program of %s; remove one of them", layoutFile, p.SwitchWhenRunning, prev)
		}
		// A game built from a layout is named after its program.
		if prev, ok := byName[strings.ToLower(p.SwitchWhenRunning)]; ok {
			return nil, nil, fmt.Errorf("%s: switch_when_running %q is also the name of %s; rename the game", layoutFile, p.SwitchWhenRunning, prev)
		}
		fromLayouts = append(fromLayouts, p)
	}
	sort.SliceStable(fromLayouts, func(i, j int) bool { return fromLayouts[i].Number < fromLayouts[j].Number })
	for _, p := range fromLayouts {
		n := p.Number
		games = append(games, Game{Name: p.SwitchWhenRunning, File: p.File, Program: p.SwitchWhenRunning, KB7: &n})
	}
	return games, desktop, nil
}

// loadGame reads one file of games/. The Desktop game has no program and must
// give a kb7 profile.
func loadGame(path, label string, isDesktop bool) (*Game, error) {
	var raw rawGame
	md, err := toml.DecodeFile(path, &raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("%s: unknown key(s): %s", label, strings.Join(keys, ", "))
	}
	g := &Game{Name: raw.Name, File: path}
	if v, ok := raw.Devices["kb7"]; ok {
		n, isInt := v.(int64)
		if !isInt || n < 0 || n > maxProfile {
			return nil, fmt.Errorf("%s: kb7 must be 0 to %d, got %v", label, maxProfile, v)
		}
		b := byte(n)
		g.KB7 = &b
	}
	if isDesktop {
		if raw.Program != nil {
			return nil, fmt.Errorf("%s: the desktop game has no program", label)
		}
		if g.KB7 == nil {
			return nil, fmt.Errorf("%s: kb7 is required in [devices]", label)
		}
		if g.Name == "" {
			g.Name = "Desktop"
		}
		return g, nil
	}
	if g.Name == "" {
		return nil, fmt.Errorf("%s: name is required", label)
	}
	if raw.Program == nil || *raw.Program == "" {
		return nil, fmt.Errorf("%s: program is required", label)
	}
	g.Program = *raw.Program
	return g, nil
}
