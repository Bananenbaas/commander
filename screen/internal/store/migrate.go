// SPDX-License-Identifier: GPL-2.0-or-later

package store

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Bananenbaas/commander/screen/internal/layout"
)

// Migration returns the writes that move every switch_when_running of cfg
// into games/, in the order that keeps every intermediate folder loadable:
// games/desktop.toml first (only when cfg.Desktop is nil; kb7 =
// cfg.DesktopProfile() of the folder before the migration), then per layout,
// in profile order, the layout without switch_when_running followed by its
// game file. files gives each layout's File by its path relative to the
// folder; the returned Files have SwitchWhenRunning cleared. With no
// switch_when_running in cfg there is nothing to migrate and no write.
func Migration(cfg *layout.Config, files map[string]*layout.File) ([]Write, error) {
	var programs []layout.Profile
	for _, p := range cfg.Profiles {
		if p.SwitchWhenRunning != "" {
			programs = append(programs, p)
		}
	}
	if len(programs) == 0 {
		return nil, nil
	}
	sort.SliceStable(programs, func(i, j int) bool { return programs[i].Number < programs[j].Number })

	// Slugs and names already taken: every file and name in games/, and the
	// Desktop game's.
	slugs := map[string]bool{"desktop": true}
	gamesDir := filepath.Join(cfg.Dir, "games")
	used := map[string]bool{"desktop": true}
	for _, g := range cfg.Games {
		if filepath.Dir(g.File) == gamesDir {
			slugs[strings.ToLower(strings.TrimSuffix(filepath.Base(g.File), filepath.Ext(g.File)))] = true
			used[strings.ToLower(g.Name)] = true
		}
	}
	if cfg.Desktop != nil {
		used[strings.ToLower(cfg.Desktop.Name)] = true
	}

	var writes []Write
	if cfg.Desktop == nil {
		data, err := (&layout.GameFile{Name: "Desktop", Devices: map[string]any{"kb7": int64(cfg.DesktopProfile())}}).Encode()
		if err != nil {
			return nil, err
		}
		writes = append(writes, Write{Rel: "games/desktop.toml", Data: data})
	}
	for _, p := range programs {
		rel, err := filepath.Rel(cfg.Dir, p.File)
		if err != nil {
			return nil, err
		}
		rel = filepath.ToSlash(rel)
		given, ok := files[rel]
		if !ok {
			return nil, fmt.Errorf("migration: no file given for %s", rel)
		}
		f := *given
		f.SwitchWhenRunning = ""
		data, err := f.Encode()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		writes = append(writes, Write{Rel: rel, Data: data})

		slug := unique(programSlug(p.SwitchWhenRunning), "-", slugs)
		name := unique(programName(p.SwitchWhenRunning), " ", used)
		g := &layout.GameFile{Name: name, Program: p.SwitchWhenRunning, Devices: map[string]any{"kb7": int64(p.Number)}}
		if data, err = g.Encode(); err != nil {
			return nil, err
		}
		writes = append(writes, Write{Rel: "games/" + slug + ".toml", Data: data})
	}
	return writes, nil
}

// unique returns s, or s with sep and 2, 3, ... appended, whichever is
// first not in taken (compared in lower case), and adds it to taken.
func unique(s, sep string, taken map[string]bool) string {
	out := s
	for n := 2; taken[strings.ToLower(out)]; n++ {
		out = fmt.Sprintf("%s%s%d", s, sep, n)
	}
	taken[strings.ToLower(out)] = true
	return out
}

// programBase is the program's file name without folders and ".exe".
func programBase(program string) string {
	if i := strings.LastIndexAny(program, `/\`); i >= 0 {
		program = program[i+1:]
	}
	if strings.HasSuffix(strings.ToLower(program), ".exe") {
		program = program[:len(program)-len(".exe")]
	}
	return program
}

// programSlug is the game file name for a program: slug of its file name
// without folders and ".exe".
func programSlug(program string) string { return slug(programBase(program)) }

// slug is a game file name for s: lower case, every character outside a-z,
// 0-9, - and _ replaced by -, "game" when empty.
func slug(s string) string {
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, strings.ToLower(s))
	if s == "" {
		return "game"
	}
	return s
}

// programName is the game's name for a program: its file name without
// ".exe", case kept, "Game" when empty.
func programName(program string) string {
	if n := programBase(program); n != "" {
		return n
	}
	return "Game"
}
