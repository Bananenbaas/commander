// SPDX-License-Identifier: GPL-2.0-or-later

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Bananenbaas/commander/screen/internal/layout"
)

// GameEdit is a game profile as the window edits it.
type GameEdit struct {
	Name    string
	Program string // "" only for Desktop
	KB7     *byte  // nil = don't switch (not allowed for Desktop)
}

// Game ids: "desktop", the game's file relative to the folder
// ("games/x.toml"), "layout:<program>" for a game still built from a
// layout's switch_when_running, or "" for a new game.
const (
	desktopID    = "desktop"
	layoutPrefix = "layout:"
	desktopRel   = "games/desktop.toml"
)

// GameFieldError is a refusal of a game profile that concerns one of its
// fields: "name" or "program". Its text is a sentence for the player.
type GameFieldError struct {
	Field string
	Msg   string
}

func (e *GameFieldError) Error() string { return e.Msg }

// SaveGame saves the game id names ("desktop", "games/x.toml",
// "layout:<program>", or "" for a new one) after checking base (the
// Version of its file; "" for a new game or a game not in a file yet).
// It returns the game's file afterwards. It migrates first when any layout
// has switch_when_running; a new game's file is games/<slug of its name>.toml
// and a saved game keeps its file. A new game is placed after the games
// there are (its order). When g.KB7 is a slot that has no layout yet, an
// empty layout is made for it, named after the game, and the change is an
// Apply. Nothing is written unless the folder with every new file loads.
func SaveGame(dir, id, base string, g GameEdit) (file string, r Result, err error) {
	name, program, err := checkGame(isDesktop(id), g)
	if err != nil {
		return "", Result{}, err
	}

	saveMu.Lock()
	defer saveMu.Unlock()
	t, err := findGame(dir, id, base)
	if err != nil {
		return "", Result{}, err
	}
	rel := t.rel
	if rel == "" {
		taken := map[string]bool{"desktop": true}
		entries, err := os.ReadDir(filepath.Join(t.abs, "games"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", Result{}, err
		}
		for _, e := range entries {
			taken[strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))] = true
		}
		for _, w := range t.writes {
			if n, ok := strings.CutPrefix(w.Rel, "games/"); ok {
				taken[strings.ToLower(strings.TrimSuffix(n, ".toml"))] = true
			}
		}
		rel = "games/" + unique(slug(name), "-", taken) + ".toml"
	}

	gf := layout.GameFile{Name: name, Devices: map[string]any{}}
	if !t.desktop {
		gf.Program = program
	}
	if t.old != nil {
		gf.Order = t.old.Order
		for k, v := range t.old.Devices {
			gf.Devices[k] = v
		}
	} else if id == "" {
		for _, other := range t.before.Games {
			gf.Order = max(gf.Order, int64(other.Order))
		}
		gf.Order++
	}
	delete(gf.Devices, "kb7")
	if g.KB7 != nil {
		gf.Devices["kb7"] = int64(*g.KB7)
	}
	data, err := gf.Encode()
	if err != nil {
		return "", Result{}, err
	}
	// A write of the same file (games/desktop.toml from the migration or the
	// pin of Desktop) is replaced in place, so the order stays loadable.
	writes := append([]Write(nil), t.writes...)
	found := false
	for i := range writes {
		if writes[i].Rel == rel {
			writes[i].Data, found = data, true
		}
	}
	if !found {
		writes = append(writes, Write{Rel: rel, Data: data})
	}
	change := Reload
	if g.KB7 != nil && profileOf(t.before, *g.KB7) == nil {
		w, err := emptyLayout(t.abs, *g.KB7, name)
		if err != nil {
			return "", Result{}, err
		}
		// The layout goes first, so the folder loads after every write.
		writes = append([]Write{w}, writes...)
		change = Apply
	}
	all := map[string][]byte{}
	for _, w := range writes {
		all[w.Rel] = w.Data
	}
	if _, err := layout.LoadWith(t.abs, all); err != nil {
		return "", Result{}, gameRefusal(err, rel, t.before)
	}

	res := Result{Change: change, Migrated: t.migrated}
	if err := commit(t.abs, writes, &res); err != nil {
		return rel, res, err
	}
	res.Version = Version(data)
	return rel, res, nil
}

// isDesktop reports whether id names the Desktop game: "desktop" or its file.
func isDesktop(id string) bool { return id == desktopID || strings.EqualFold(id, desktopRel) }

// checkGame checks an edit before anything is loaded and returns its name
// and program without surrounding spaces.
func checkGame(desktop bool, g GameEdit) (name, program string, err error) {
	name, program = strings.TrimSpace(g.Name), strings.TrimSpace(g.Program)
	if desktop {
		if program != "" {
			return "", "", errors.New("Desktop has no program.")
		}
		if g.KB7 == nil {
			return "", "", errors.New("Desktop needs a KB7 profile.")
		}
		if name == "" {
			name = "Desktop"
		}
		return name, "", nil
	}
	if name == "" {
		return "", "", &GameFieldError{"name", "Give the game profile a name."}
	}
	if program == "" {
		return "", "", &GameFieldError{"program", "Choose the program that starts it."}
	}
	return name, program, nil
}

// DeleteGame removes the game id names (as SaveGame) after checking base,
// backing its file up when it is hand-written. It migrates first when any
// layout has switch_when_running; a game still built from a layout is then
// left out of the migration. Desktop cannot be deleted.
func DeleteGame(dir, id, base string) (Result, error) {
	if isDesktop(id) {
		return Result{}, errors.New("Desktop cannot be deleted.")
	}
	if id == "" {
		return Result{}, errors.New("store: no game to delete")
	}

	saveMu.Lock()
	defer saveMu.Unlock()
	t, err := findGame(dir, id, base)
	if err != nil {
		return Result{}, err
	}
	if t.desktop {
		return Result{}, errors.New("Desktop cannot be deleted.")
	}
	var writes []Write
	fromMigration := false // the game's file would be made by the migration
	for _, w := range t.writes {
		if w.Rel == t.rel {
			fromMigration = true
			continue
		}
		writes = append(writes, w)
	}
	all := map[string][]byte{}
	for _, w := range writes {
		all[w.Rel] = w.Data
	}
	if !fromMigration {
		all[t.rel] = nil
	}
	if _, err := layout.LoadWith(t.abs, all); err != nil {
		return Result{}, otherFile(err)
	}

	res := Result{Change: Reload, Migrated: t.migrated}
	if err := commit(t.abs, writes, &res); err != nil {
		return res, err
	}
	if fromMigration {
		return res, nil
	}
	w := newSaveWriter(t.abs)
	b, err := w.backup(t.rel, gameLossless)
	if err != nil {
		return res, err
	}
	if b != "" {
		res.Backups = append(res.Backups, b)
	}
	if err := os.Remove(w.abs(t.rel)); err != nil {
		return res, err
	}
	res.Written = true
	return res, nil
}

// gameTarget is the game a save or delete works on.
type gameTarget struct {
	abs      string
	rel      string // the game's file (after the migration); "" for a new game
	desktop  bool
	old      *layout.GameFile // the game's file as it is now, nil when there is none
	writes   []Write          // the migration, when one is needed
	migrated bool
	before   *layout.Config // the folder after the migration, before the edit
}

// findGame loads the folder, finds the game id and checks its base version
// (ErrChanged when the game is gone or its file has another version), and
// works out the migration.
func findGame(dir, id, base string) (*gameTarget, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	cfg, err := layout.Load(abs)
	if err != nil {
		return nil, otherFile(err)
	}
	t := &gameTarget{abs: abs, before: cfg}
	relOf := func(file string) string {
		r, err := filepath.Rel(abs, file)
		if err != nil {
			return ""
		}
		return filepath.ToSlash(r)
	}
	inFile := true // the game has a file whose version base is
	program := ""  // the program of a game built from a layout
	switch {
	case isDesktop(id):
		t.desktop = true
		if cfg.Desktop != nil {
			t.rel = relOf(cfg.Desktop.File)
		} else {
			t.rel, inFile = desktopRel, false
		}
	case strings.HasPrefix(id, layoutPrefix):
		program, inFile = strings.TrimPrefix(id, layoutPrefix), false
		found := false
		for _, g := range cfg.Games {
			if g.Program == program && !strings.HasPrefix(relOf(g.File), "games/") {
				found = true
			}
		}
		if !found {
			return nil, ErrChanged
		}
	case id == "":
		inFile = false
	default:
		for _, g := range cfg.Games {
			if relOf(g.File) == id && strings.HasPrefix(id, "games/") {
				t.rel = id
			}
		}
		if t.rel == "" {
			if strings.HasPrefix(id, "games/") {
				return nil, ErrChanged
			}
			return nil, fmt.Errorf("store: unknown game id %q", id)
		}
	}
	if inFile {
		data, err := os.ReadFile(filepath.Join(abs, filepath.FromSlash(t.rel)))
		if err != nil {
			return nil, err
		}
		if Version(data) != base {
			return nil, ErrChanged
		}
		if t.old, err = layout.DecodeGameFile(data); err != nil {
			return nil, err
		}
	} else if base != "" {
		return nil, ErrChanged
	}

	for _, p := range cfg.Profiles {
		t.migrated = t.migrated || p.SwitchWhenRunning != ""
	}
	if !t.migrated {
		if cfg.Desktop != nil {
			return t, nil
		}
		// Without games/desktop.toml, Desktop is the lowest free profile, and
		// a game added, moved or removed could move it; pin it where it is.
		data, err := (&layout.GameFile{Name: "Desktop", Devices: map[string]any{"kb7": int64(cfg.DesktopProfile())}}).Encode()
		if err != nil {
			return nil, err
		}
		t.writes = []Write{{Rel: desktopRel, Data: data}}
		if t.before, err = layout.LoadWith(abs, map[string][]byte{desktopRel: data}); err != nil {
			return nil, otherFile(err)
		}
		return t, nil
	}
	// The migration writes games/desktop.toml when there is none, which pins
	// Desktop the same way.
	layouts, err := readLayouts(abs)
	if err != nil {
		return nil, err
	}
	files := map[string]*layout.File{}
	for _, l := range layouts {
		if l.file != nil {
			files[l.rel] = l.file
		}
	}
	if t.writes, err = Migration(cfg, files); err != nil {
		return nil, err
	}
	all := map[string][]byte{}
	for _, w := range t.writes {
		all[w.Rel] = w.Data
		if program == "" || !strings.HasPrefix(w.Rel, "games/") {
			continue
		}
		g, err := layout.DecodeGameFile(w.Data)
		if err != nil {
			return nil, err
		}
		if g.Program == program {
			t.rel, t.old = w.Rel, g
		}
	}
	if t.before, err = layout.LoadWith(abs, all); err != nil {
		return nil, otherFile(err)
	}
	return t, nil
}

// clash matches the load errors of two game files with the same name or
// program.
var clash = regexp.MustCompile(`^(games/.+?) and (games/.+?) both (use the name|name the program) `)

// gameRefusal turns a load error of the folder with the game file rel saved
// into the sentence for the player: a clash names the other game of before
// by its name, any other error names its file.
func gameRefusal(err error, rel string, before *layout.Config) error {
	m := clash.FindStringSubmatch(err.Error())
	if m == nil || (m[1] != rel && m[2] != rel) {
		return otherFile(err)
	}
	other := m[1]
	if other == rel {
		other = m[2]
	}
	games := append([]layout.Game(nil), before.Games...)
	if before.Desktop != nil {
		games = append(games, *before.Desktop)
	}
	for _, g := range games {
		r, rerr := filepath.Rel(before.Dir, g.File)
		if rerr != nil || filepath.ToSlash(r) != other {
			continue
		}
		if m[3] == "use the name" {
			return &GameFieldError{"name", fmt.Sprintf("Another game profile is called %s.", g.Name)}
		}
		return &GameFieldError{"program", fmt.Sprintf("%s already switches on %s.", g.Name, g.Program)}
	}
	return otherFile(err)
}

// commit backs up and writes writes in order; res records the backups and
// whether anything was written.
func commit(abs string, writes []Write, res *Result) error {
	w := newSaveWriter(abs)
	for _, wr := range writes {
		lossless := layoutLossless
		if strings.HasPrefix(wr.Rel, "games/") {
			lossless = gameLossless
		}
		b, err := w.backup(wr.Rel, lossless)
		if err != nil {
			return err
		}
		if b != "" {
			res.Backups = append(res.Backups, b)
		}
		if err := w.put(wr.Rel, wr.Data); err != nil {
			return err
		}
		res.Written = true
	}
	return nil
}
