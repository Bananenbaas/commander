// SPDX-License-Identifier: GPL-2.0-or-later

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/Bananenbaas/commander/screen/internal/apply"
	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/ledger"
)

// Change says what Commander has to do after a save.
type Change int

const (
	NoChange Change = iota
	Reload          // actions, modes, confirm, states or game profiles changed
	Apply           // buttons added, removed or moved, or a title, label, colour, symbol or picture changed
)

func (c Change) String() string {
	switch c {
	case NoChange:
		return "none"
	case Reload:
		return "reload"
	case Apply:
		return "apply"
	}
	return fmt.Sprintf("Change(%d)", int(c))
}

// Decide compares one profile before and after a save. Buttons are matched
// by page and position. A nil profile (one that did not load) counts as
// changed in every way. Lighting layers are not compared: a buttons save
// does not change them.
func Decide(old, new *layout.Profile) Change {
	if old == nil || new == nil {
		return Apply
	}
	if old.Name != new.Name || !reflect.DeepEqual(old.StoredLight, new.StoredLight) {
		return Apply
	}
	change := NoChange
	if old.SwitchWhenRunning != new.SwitchWhenRunning {
		change = Reload
	}
	if len(old.Buttons) != len(new.Buttons) {
		return Apply
	}
	before := map[[2]int]layout.Button{}
	for _, b := range old.Buttons {
		before[[2]int{b.Page, b.Position}] = b
	}
	for _, b := range new.Buttons {
		o, ok := before[[2]int{b.Page, b.Position}]
		if !ok {
			return Apply
		}
		if o.Title != b.Title || o.Label != b.Label || o.Color != b.Color || o.Symbol != b.Symbol || o.Picture != b.Picture {
			return Apply
		}
		if o.Mode != b.Mode || o.State != b.State || o.Confirm != b.Confirm || !reflect.DeepEqual(o.Action, b.Action) {
			change = Reload
		}
	}
	return change
}

// Result tells what a save did.
type Result struct {
	Version  string // the new version of the saved layout
	Change   Change
	Backups  []string // relative paths of the backups made
	Migrated bool
}

// ErrChanged: the layout on disk no longer has the version the save was based on.
var ErrChanged = errors.New("the file was changed outside Commander")

// ButtonError is a button of the saved layout that is wrong.
type ButtonError struct {
	Page, Position int
	Err            *layout.FieldError
}

func (e *ButtonError) Error() string {
	return fmt.Sprintf("page %d position %d: %v", e.Page, e.Position, e.Err)
}

func (e *ButtonError) Unwrap() error { return e.Err }

// saveMu serializes saves.
var saveMu sync.Mutex

// newSaveWriter makes the writer a save uses; tests replace it.
var newSaveWriter = newWriter

// SaveButtons replaces the buttons of the layout of KB7 profile slot (0-4)
// with buttons, after checking the file still has version base. It
// migrates first when any layout has switch_when_running. Nothing is
// written unless the folder with every new file loads and its pictures fit.
func SaveButtons(dir string, slot byte, base string, buttons []layout.FileButton) (Result, error) {
	saveMu.Lock()
	defer saveMu.Unlock()
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Result{}, err
	}
	layouts, err := readLayouts(abs)
	if err != nil {
		return Result{}, err
	}
	var saved *layoutFile
	for i := range layouts {
		if layouts[i].file != nil && layouts[i].file.Profile == int(slot) {
			saved = &layouts[i]
			break
		}
	}
	if saved == nil {
		// A file that does not decode may be the one; then its error is
		// the one to show.
		if _, err := layout.Load(abs); err != nil {
			return Result{}, otherFile(err)
		}
		return Result{}, fmt.Errorf("There is no layout for profile %d.", int(slot)+1)
	}
	if Version(saved.data) != base {
		return Result{}, ErrChanged
	}
	if err := checkPlaces(buttons); err != nil {
		return Result{}, err
	}

	edited := *saved.file
	edited.Buttons = append([]layout.FileButton(nil), buttons...)
	data, err := edited.Encode()
	if err != nil {
		return Result{}, err
	}
	writes := []Write{{Rel: saved.rel, Data: data}}
	cfg, err := layout.LoadWith(abs, map[string][]byte{saved.rel: data})
	if err != nil {
		return Result{}, refusal(err, saved.rel, buttons)
	}

	migrated := false
	for _, p := range cfg.Profiles {
		migrated = migrated || p.SwitchWhenRunning != ""
	}
	if migrated {
		files := map[string]*layout.File{saved.rel: &edited}
		for _, l := range layouts {
			if l.file != nil && l.rel != saved.rel {
				files[l.rel] = l.file
			}
		}
		if writes, err = Migration(cfg, files); err != nil {
			return Result{}, err
		}
		found := false
		for _, w := range writes {
			if w.Rel == saved.rel {
				data, found = w.Data, true
			}
		}
		if !found {
			writes = append(writes, Write{Rel: saved.rel, Data: data})
		}
		all := map[string][]byte{}
		for _, w := range writes {
			all[w.Rel] = w.Data
		}
		if cfg, err = layout.LoadWith(abs, all); err != nil {
			return Result{}, refusal(err, saved.rel, buttons)
		}
	}

	n, err := apply.CheckPictures(cfg)
	if err != nil {
		return Result{}, refusal(err, saved.rel, buttons)
	}
	if n > ledger.MaxSlot {
		return Result{}, fmt.Errorf("The KB7 holds %d different pictures; this would be %d.", ledger.MaxSlot, n)
	}

	var oldP, newP *layout.Profile
	if old, err := layout.Load(abs); err == nil {
		oldP = profileOf(old, slot)
	}
	newP = profileOf(cfg, slot)
	res := Result{Change: Decide(oldP, newP), Migrated: migrated}
	if migrated && res.Change < Reload {
		res.Change = Reload
	}

	w := newSaveWriter(abs)
	for _, wr := range writes {
		lossless := layoutLossless
		if strings.HasPrefix(wr.Rel, "games/") {
			lossless = gameLossless
		}
		b, err := w.backup(wr.Rel, lossless)
		if err != nil {
			return res, err
		}
		if b != "" {
			res.Backups = append(res.Backups, b)
		}
		if err := w.put(wr.Rel, wr.Data); err != nil {
			return res, err
		}
	}
	res.Version = Version(data)
	return res, nil
}

// layoutFile is one layout file of the folder; file is nil when it does
// not decode.
type layoutFile struct {
	rel  string
	data []byte
	file *layout.File
}

// readLayouts reads every *.toml file directly in dir, as Load does.
func readLayouts(dir string) ([]layoutFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []layoutFile
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".toml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		f, _ := layout.DecodeFile(data)
		out = append(out, layoutFile{rel: e.Name(), data: data, file: f})
	}
	return out, nil
}

func profileOf(cfg *layout.Config, slot byte) *layout.Profile {
	for i := range cfg.Profiles {
		if cfg.Profiles[i].Number == slot {
			return &cfg.Profiles[i]
		}
	}
	return nil
}

// checkPlaces refuses two buttons in the same place.
func checkPlaces(buttons []layout.FileButton) error {
	seen := map[[2]int]bool{}
	for _, b := range buttons {
		k := [2]int{b.Page, b.Position}
		if seen[k] {
			return &ButtonError{Page: b.Page, Position: b.Position,
				Err: &layout.FieldError{Field: "position", Step: -1, Err: errors.New("duplicate button")}}
		}
		seen[k] = true
	}
	return nil
}

// refusal turns an error of loading or rendering the folder with the new
// layout saved into a *ButtonError when it concerns one of the new buttons,
// else into the sentence naming the file that has the error.
func refusal(err error, saved string, buttons []layout.FileButton) error {
	rest, ok := strings.CutPrefix(err.Error(), saved+": ")
	var page, pos int
	if ok {
		if _, serr := fmt.Sscanf(rest, "page %d position %d:", &page, &pos); serr != nil {
			ok = false
		}
	}
	if !ok {
		return otherFile(err)
	}
	var fe *layout.FieldError
	if errors.As(err, &fe) {
		return &ButtonError{Page: page, Position: pos, Err: fe}
	}
	// A picture or symbol that does not render.
	field := "symbol"
	for _, b := range buttons {
		if b.Page == page && b.Position == pos && b.Picture != "" {
			field = "picture"
		}
	}
	inner := errors.Unwrap(err)
	if inner == nil {
		inner = err
	}
	return &ButtonError{Page: page, Position: pos, Err: &layout.FieldError{Field: field, Step: -1, Err: inner}}
}

// otherFile is the sentence for a load error that the save cannot fix.
func otherFile(err error) error {
	msg := err.Error()
	return fmt.Errorf("%s has an error, fix it first: %s", fileNamed(msg), msg)
}

// fileNamed returns the file a load error names: its prefix "<file>: ", else
// the first word that is a .toml file.
func fileNamed(msg string) string {
	isFile := func(s string) bool { return strings.HasSuffix(strings.ToLower(s), ".toml") }
	if i := strings.Index(msg, ": "); i > 0 && isFile(msg[:i]) {
		return msg[:i]
	}
	for _, w := range strings.Fields(msg) {
		if w = strings.TrimRight(w, ":,;"); isFile(w) {
			return w
		}
	}
	return "The configuration folder"
}

// layoutLossless reports whether Commander's encoding of a layout file's
// bytes gives the same bytes, so overwriting it loses nothing.
func layoutLossless(data []byte) bool {
	f, err := layout.DecodeFile(data)
	if err != nil {
		return false
	}
	out, err := f.Encode()
	return err == nil && string(out) == string(data)
}

// gameLossless is layoutLossless for a file of games/.
func gameLossless(data []byte) bool {
	g, err := layout.DecodeGameFile(data)
	if err != nil {
		return false
	}
	out, err := g.Encode()
	return err == nil && string(out) == string(data)
}
