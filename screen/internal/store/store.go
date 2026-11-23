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
	"github.com/Bananenbaas/commander/screen/internal/light"
)

// Change says what Commander has to do after a save.
type Change int

const (
	NoChange Change = iota
	Reload          // actions, modes, confirm, states, game profiles, or the lighting of a profile with a stored colour changed
	Apply           // buttons added, removed or moved, a title, label, colour, symbol or picture, [stored_light], or the lighting of a profile without a stored colour changed
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
// changed in every way. A changed [light] is an apply when the new profile
// has no [stored_light] colour (the stored still follows the lighting), else
// a reload.
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
	if !reflect.DeepEqual(old.Light, new.Light) {
		if new.StoredLight == nil || new.StoredLight.Color.A == 0 {
			return Apply
		}
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
	Version  string // the new version of the section saved: the lighting when the save replaced it, else the buttons
	Change   Change
	Backups  []string // relative paths of the backups made
	Migrated bool
	Written  bool // at least one file was written, even if the save then failed
}

// ErrChanged: a section of the layout on disk no longer has the version the
// save was based on.
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

// LightError is a save refused because of the saved layout's lighting.
type LightError struct {
	Layer   int    // 1-based, bottom first, counting switched-off layers; 0 = the lighting as a whole
	Setting string // "" when not about one setting
	Err     error  // for Layer 0, its text names the table: "light: ..." or "stored_light: ..."
}

func (e *LightError) Error() string {
	if e.Layer == 0 {
		return e.Err.Error()
	}
	return fmt.Sprintf("light.layer %d: %v", e.Layer, e.Err)
}

func (e *LightError) Unwrap() error { return e.Err }

// Edit says which sections of a layout a save replaces; nil fields are kept
// as they are in the file on disk.
type Edit struct {
	Buttons     *[]layout.FileButton
	Light       **layout.FileLight  // set to a nil *FileLight to remove [light]
	StoredLight **layout.FileStored // set to a nil *FileStored to remove [stored_light]
}

// Section versions: the first 16 hex digits of the SHA-256 of the section
// encoded on its own (buttons: the [[button]] tables; lighting: [light] and
// [stored_light]).

// ButtonsVersion is the version of f's buttons.
func ButtonsVersion(f *layout.File) string {
	return sectionVersion(&layout.File{Buttons: f.Buttons})
}

// LightVersion is the version of f's lighting.
func LightVersion(f *layout.File) string {
	return sectionVersion(&layout.File{Light: f.Light, StoredLight: f.StoredLight})
}

func sectionVersion(f *layout.File) string {
	data, err := f.Encode()
	if err != nil {
		// Not the version of any section that encodes. Two sections that
		// fail the same way share it, so the version check passes there;
		// such a save fails later, when the edited file is encoded.
		return Version([]byte("unencodable: " + err.Error()))
	}
	return Version(data)
}

// saveMu serializes saves.
var saveMu sync.Mutex

// newSaveWriter makes the writer a save uses; tests replace it.
var newSaveWriter = newWriter

// SaveButtons replaces the buttons of the layout of KB7 profile slot (0-4)
// with buttons, after checking they still have version base (ButtonsVersion).
func SaveButtons(dir string, slot byte, base string, buttons []layout.FileButton) (Result, error) {
	return SaveProfile(dir, slot, map[string]string{"buttons": base}, Edit{Buttons: &buttons})
}

// SaveProfile replaces the sections e gives of the layout of KB7 profile slot
// (0-4), after checking each replaced section still has its base version
// (bases maps "buttons" and "light" to it; ErrChanged when one differs). It
// migrates first when any layout has switch_when_running. Nothing is written
// unless the folder with every new file loads and its pictures fit.
func SaveProfile(dir string, slot byte, bases map[string]string, e Edit) (Result, error) {
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
	saved, err := pickLayout(abs, layouts, slot)
	if err != nil {
		return Result{}, err
	}
	lighting := e.Light != nil || e.StoredLight != nil
	if e.Buttons != nil && ButtonsVersion(saved.file) != bases["buttons"] {
		return Result{}, ErrChanged
	}
	if lighting && LightVersion(saved.file) != bases["light"] {
		return Result{}, ErrChanged
	}

	edited := *saved.file
	if e.Buttons != nil {
		if err := checkPlaces(*e.Buttons); err != nil {
			return Result{}, err
		}
		edited.Buttons = append([]layout.FileButton(nil), *e.Buttons...)
	}
	if e.Light != nil {
		edited.Light = *e.Light
	}
	if e.StoredLight != nil {
		edited.StoredLight = *e.StoredLight
	}
	buttons := edited.Buttons
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
	final, err := layout.DecodeFile(data)
	if err != nil {
		return Result{}, err
	}
	version := ButtonsVersion(final)
	if lighting {
		version = LightVersion(final)
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
		res.Written = true
	}
	res.Version = version
	return res, nil
}

// LayoutFile returns the name (relative to dir) and the bytes of the layout
// file of KB7 profile slot (0-4), the file SaveButtons replaces.
func LayoutFile(dir string, slot byte) (rel string, data []byte, err error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, err
	}
	layouts, err := readLayouts(abs)
	if err != nil {
		return "", nil, err
	}
	l, err := pickLayout(abs, layouts, slot)
	if err != nil {
		return "", nil, err
	}
	return l.rel, l.data, nil
}

// pickLayout finds the layout of slot among layouts. When none decodes for
// it, a file that does not decode may be the one, and then its error is the
// one to show.
func pickLayout(abs string, layouts []layoutFile, slot byte) (*layoutFile, error) {
	for i := range layouts {
		if layouts[i].file != nil && layouts[i].file.Profile == int(slot) {
			return &layouts[i], nil
		}
	}
	if _, err := layout.Load(abs); err != nil {
		return nil, otherFile(err)
	}
	return nil, fmt.Errorf("There is no layout for profile %d.", int(slot)+1)
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
// layout saved into a *LightError when it concerns the layout's lighting, a
// *ButtonError when it concerns one of its buttons, else into the sentence
// naming the file that has the error.
func refusal(err error, saved string, buttons []layout.FileButton) error {
	rest, ok := strings.CutPrefix(err.Error(), saved+": ")
	if ok {
		var le *light.LayerError
		if layer, setting, isLight := layout.LightError(err); isLight && errors.As(err, &le) {
			return &LightError{Layer: layer, Setting: setting, Err: le.Err}
		}
		if strings.HasPrefix(rest, "light: ") || strings.HasPrefix(rest, "stored_light: ") {
			return &LightError{Err: errors.New(rest)}
		}
	}
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
