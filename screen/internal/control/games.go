// SPDX-License-Identifier: GPL-2.0-or-later

package control

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/store"
)

// gameChangedText answers a save or delete of a game changed on disk.
const gameChangedText = "This game profile was changed outside Commander since you opened it."

// Game ids as the window uses them (see store.SaveGame).
const (
	desktopID    = "desktop"
	desktopRel   = "games/desktop.toml"
	layoutPrefix = "layout:"
)

// relOf is file relative to dir, with forward slashes; "" when it is not
// inside dir.
func relOf(dir, file string) string {
	r, err := filepath.Rel(dir, file)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(r)
}

// gameID is the id the window names g by: its file in games/, or
// "layout:<program>" for a game still built from a layout.
func gameID(cfg *layout.Config, g layout.Game) string {
	if rel := relOf(cfg.Dir, g.File); strings.HasPrefix(rel, "games/") {
		return rel
	}
	return layoutPrefix + g.Program
}

type profileName struct {
	Number int    `json:"number"`
	Name   string `json:"name"`
}

type gameProfileOut struct {
	ID       string        `json:"id"`
	File     string        `json:"file"`
	Version  string        `json:"version"`
	Name     string        `json:"name"`
	Program  string        `json:"program"`
	KB7      *int          `json:"kb7"`
	Desktop  bool          `json:"desktop"`
	Profiles []profileName `json:"profiles"`
}

func (s *Service) gameProfile(args json.RawMessage) (any, error) {
	var a struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("gameProfile: bad arguments: %w", err)
	}
	cfg, err := s.d.Load(s.d.Dir)
	if err != nil {
		return nil, fmt.Errorf("gameProfile: %w", err)
	}
	out := gameProfileOut{ID: a.ID, Profiles: []profileName{}}
	for _, p := range cfg.Profiles {
		out.Profiles = append(out.Profiles, profileName{int(p.Number), p.Name})
	}

	var g *layout.Game
	switch {
	case a.ID == desktopID || strings.EqualFold(a.ID, desktopRel):
		out.ID, out.Desktop = desktopID, true
		if cfg.Desktop == nil {
			n := int(cfg.DesktopProfile())
			out.Name, out.KB7 = "Desktop", &n
			return out, nil
		}
		g = cfg.Desktop
	case a.ID == "":
		return out, nil
	default:
		for i := range cfg.Games {
			if gameID(cfg, cfg.Games[i]) == a.ID {
				g = &cfg.Games[i]
			}
		}
		if g == nil {
			return nil, fmt.Errorf("gameProfile: no game profile %q", a.ID)
		}
	}

	out.File, out.Name, out.Program = relOf(cfg.Dir, g.File), g.Name, g.Program
	if g.KB7 != nil {
		n := int(*g.KB7)
		out.KB7 = &n
	}
	if !strings.HasPrefix(a.ID, layoutPrefix) {
		data, err := os.ReadFile(g.File)
		if err != nil {
			return nil, fmt.Errorf("gameProfile: %w", err)
		}
		out.Version = store.Version(data)
	}
	return out, nil
}

func (s *Service) saveGameProfile(args json.RawMessage) (any, error) {
	var a struct {
		ID      string `json:"id"`
		Version string `json:"version"`
		Name    string `json:"name"`
		Program string `json:"program"`
		KB7     *int   `json:"kb7"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("saveGameProfile: bad arguments: %w", err)
	}
	g := store.GameEdit{Name: a.Name, Program: a.Program}
	if a.KB7 != nil {
		if *a.KB7 < 0 || *a.KB7 > 4 {
			return nil, fmt.Errorf("saveGameProfile: unknown profile %d", *a.KB7)
		}
		n := byte(*a.KB7)
		g.KB7 = &n
	}
	file, res, err := store.SaveGame(s.d.Dir, a.ID, a.Version, g)
	out := s.saveAnswer("saveGameProfile", gameChangedText, res, err, noRefusals)
	if out["ok"] == true {
		out["id"] = file
	}
	return out, nil
}

func (s *Service) deleteGameProfile(args json.RawMessage) (any, error) {
	var a struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("deleteGameProfile: bad arguments: %w", err)
	}
	res, err := store.DeleteGame(s.d.Dir, a.ID, a.Version)
	return s.saveAnswer("deleteGameProfile", gameChangedText, res, err, noRefusals), nil
}

// noRefusals is a saveAnswer refusal function for calls whose refusals are
// the store's sentences, answered as they are.
func noRefusals(error) map[string]any { return nil }

// windowsOwn are the processes of Windows itself that are not under the
// Windows folder (or whose path cannot be read), and Commander's own.
var windowsOwn = map[string]bool{
	"system": true, "idle": true, "registry": true, "smss.exe": true, "csrss.exe": true,
	"wininit.exe": true, "services.exe": true, "lsass.exe": true, "svchost.exe": true,
	"dwm.exe": true, "winlogon.exe": true, "fontdrvhost.exe": true, "sihost.exe": true,
	"conhost.exe": true, "runtimebroker.exe": true,
	"commander.exe": true, "kb7screen.exe": true,
}

// windowsFolder is the Windows folder, lower case and ending in a separator;
// "" when it is not known.
func windowsFolder() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = os.Getenv("windir")
	}
	if root == "" {
		return ""
	}
	return strings.ToLower(strings.TrimRight(root, `\/`)) + `\`
}

// runningPrograms answers the programs a game can switch on: the names of
// the running processes without Windows' own and Commander's, each once,
// sorted without regard to case.
func (s *Service) runningPrograms() (any, error) {
	programs := []string{}
	if s.d.Running == nil {
		return map[string]any{"programs": programs}, nil
	}
	list, err := s.d.Running()
	if err != nil {
		return nil, fmt.Errorf("runningPrograms: %w", err)
	}
	win := windowsFolder()
	seen := map[string]bool{}
	for _, p := range list {
		low := strings.ToLower(p.Name)
		if low == "" || windowsOwn[low] || seen[low] {
			continue
		}
		if win != "" && strings.HasPrefix(strings.ToLower(strings.ReplaceAll(p.Path, "/", `\`)), win) {
			continue
		}
		seen[low] = true
		programs = append(programs, p.Name)
	}
	sort.Slice(programs, func(i, j int) bool {
		a, b := strings.ToLower(programs[i]), strings.ToLower(programs[j])
		if a != b {
			return a < b
		}
		return programs[i] < programs[j]
	})
	return map[string]any{"programs": programs}, nil
}

type volumeOut struct {
	Name  string `json:"name"`
	Level int    `json:"level"`
	Muted bool   `json:"muted"`
}

type programVolumeOut struct {
	Name   string `json:"name"`
	Level  int    `json:"level"`
	Muted  bool   `json:"muted"`
	Active bool   `json:"active"`
}

type mixerOut struct {
	Available bool               `json:"available"`
	System    volumeOut          `json:"system"`
	Programs  []programVolumeOut `json:"programs"`
}

// mixer answers the volumes the keyboard's mixer page shows. Without audio,
// or when it cannot be read, it is not available; that is no error, as the
// window asks every second, and the same failure is logged only once.
func (s *Service) mixer() mixerOut {
	none := mixerOut{Programs: []programVolumeOut{}}
	if s.d.Mixer == nil {
		return none
	}
	sys, progs, ok, err := s.d.Mixer()
	text := ""
	if ok && err != nil {
		text = err.Error()
	}
	s.mixMu.Lock()
	changed := text != s.mixErr
	s.mixErr = text
	s.mixMu.Unlock()
	if text != "" && changed {
		s.logf("control: mixer: %v", err)
	}
	if !ok || err != nil {
		return none
	}
	out := mixerOut{Available: true, System: volumeOut{sys.Name, sys.Level, sys.Muted}, Programs: []programVolumeOut{}}
	for _, p := range progs {
		out.Programs = append(out.Programs, programVolumeOut{p.Name, p.Level, p.Muted, p.Active})
	}
	return out
}
