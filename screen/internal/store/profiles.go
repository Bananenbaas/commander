// SPDX-License-Identifier: GPL-2.0-or-later

package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/proto"
)

// SaveProfileName gives the layout of KB7 profile slot (0-4) the name name
// (without surrounding spaces; "" removes the name), after checking the
// layout file still has version base: the Version of the whole file, since
// the name lives outside both sections that SaveProfile versions. It migrates
// first when any layout has switch_when_running. The change is an Apply.
func SaveProfileName(dir string, slot byte, base, name string) (Result, error) {
	name = strings.TrimSpace(name)
	if name != "" {
		if len(name) > proto.MaxNameLen {
			return Result{}, fmt.Errorf("A name on the keyboard is at most %d characters.", proto.MaxNameLen)
		}
		if proto.CheckName(name) != nil {
			return Result{}, errors.New("The keyboard can only show plain letters, digits and symbols (no accents or emoji).")
		}
	}

	saveMu.Lock()
	defer saveMu.Unlock()
	t, err := findProfile(dir, slot, base)
	if err != nil {
		return Result{}, err
	}
	edited := *t.saved.file
	edited.Name = nil
	if name != "" {
		edited.Name = &name
	}
	data, err := edited.Encode()
	if err != nil {
		return Result{}, err
	}
	writes, migrated, err := t.prepare(map[string]*layout.File{t.saved.rel: &edited}, false)
	if err != nil {
		return Result{}, err
	}
	// A layout the migration rewrites is already in writes, with the name.
	if fileBytes(writes, t.saved.rel) == nil {
		writes = append(writes, Write{Rel: t.saved.rel, Data: data})
	}
	if _, err := layout.LoadWith(t.abs, asFiles(writes)); err != nil {
		return Result{}, otherFile(err)
	}

	res := Result{Change: Apply, Migrated: migrated}
	if err := commit(t.abs, writes, &res); err != nil {
		return res, err
	}
	res.Version = Version(fileBytes(writes, t.saved.rel))
	return res, nil
}

// DuplicateProfile copies the layout of KB7 profile slot (0-4) into the
// lowest slot that has no layout and returns that slot. The copy is the file
// kb7-<slot+1>.toml (-2, -3 on a clash), without switch_when_running (games
// own programs), named like the original with " copy" added, the original cut
// to make room when the name would be too long. Desktop stays on its
// profile. The change is an Apply.
func DuplicateProfile(dir string, slot byte) (newSlot byte, r Result, err error) {
	saveMu.Lock()
	defer saveMu.Unlock()
	t, err := findProfile(dir, slot, "")
	if err != nil {
		return 0, Result{}, err
	}
	have := map[int]bool{}
	taken := map[string]bool{}
	for _, l := range t.layouts {
		taken[strings.ToLower(strings.TrimSuffix(l.rel, filepath.Ext(l.rel)))] = true
		if l.file != nil {
			have[l.file.Profile] = true
		}
	}
	free := -1
	for n := 0; n <= 4 && free < 0; n++ {
		if !have[n] {
			free = n
		}
	}
	if free < 0 {
		return 0, Result{}, errors.New(fiveProfiles)
	}

	cp := *t.saved.file
	cp.Profile = free
	cp.SwitchWhenRunning = ""
	cp.Name = nil
	if orig := t.saved.file.Name; orig != nil && *orig != "" {
		n := *orig + " copy"
		if len(n) > proto.MaxNameLen {
			n = strings.TrimRight((*orig)[:min(len(*orig), proto.MaxNameLen-len(" copy"))], " ") + " copy"
		}
		cp.Name = &n
	}
	data, err := cp.Encode()
	if err != nil {
		return 0, Result{}, err
	}
	rel := unique(fmt.Sprintf("kb7-%d", free+1), "-", taken) + ".toml"

	writes, migrated, err := t.prepare(nil, true)
	if err != nil {
		return 0, Result{}, err
	}
	writes = append(writes, Write{Rel: rel, Data: data})
	if _, err := layout.LoadWith(t.abs, asFiles(writes)); err != nil {
		return 0, Result{}, otherFile(err)
	}

	res := Result{Change: Apply, Migrated: migrated}
	if err := commit(t.abs, writes, &res); err != nil {
		return 0, res, err
	}
	res.Version = Version(data)
	return byte(free), res, nil
}

// DeleteProfile removes the layout of KB7 profile slot (0-4) after checking
// the file still has version base (the Version of the whole file). It is
// refused while a game profile, Desktop included, uses the profile, and for
// the last layout. The file is moved to backup/ (also when it is in
// Commander's form), so it can be restored. It migrates first when any layout
// has switch_when_running, and keeps Desktop on its profile. The change is an
// Apply.
func DeleteProfile(dir string, slot byte, base string) (Result, error) {
	saveMu.Lock()
	defer saveMu.Unlock()
	t, err := findProfile(dir, slot, base)
	if err != nil {
		return Result{}, err
	}
	writes, migrated, err := t.prepare(nil, true)
	if err != nil {
		return Result{}, err
	}
	if len(t.cfg.Profiles) < 2 {
		return Result{}, errors.New("The last profile cannot be deleted.")
	}
	with, err := layout.LoadWith(t.abs, asFiles(writes))
	if err != nil {
		return Result{}, otherFile(err)
	}
	var users []string
	if with.Desktop != nil && *with.Desktop.KB7 == slot {
		users = append(users, "Desktop")
	}
	for _, g := range with.Games {
		if g.KB7 != nil && *g.KB7 == slot {
			users = append(users, g.Name)
		}
	}
	if len(users) > 0 {
		verb := "use"
		if len(users) == 1 {
			verb = "uses"
		}
		return Result{}, fmt.Errorf("%s %s this profile; choose another profile there first.", joinNames(users), verb)
	}
	gone := asFiles(writes)
	gone[t.saved.rel] = nil
	if _, err := layout.LoadWith(t.abs, gone); err != nil {
		return Result{}, otherFile(err)
	}

	res := Result{Change: Apply, Migrated: migrated}
	if err := commit(t.abs, writes, &res); err != nil {
		return res, err
	}
	w := newSaveWriter(t.abs)
	b, err := w.backup(t.saved.rel, func([]byte) bool { return false })
	if err != nil {
		return res, err
	}
	res.Backups = append(res.Backups, b)
	if err := os.Remove(w.abs(t.saved.rel)); err != nil {
		return res, err
	}
	res.Written = true
	return res, nil
}

// fiveProfiles is the refusal of a sixth profile.
const fiveProfiles = "The KB7 has five profiles and all five are in use."

// joinNames joins names with ", " and "and": "A", "A and B", "A, B and C".
func joinNames(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// profileTarget is the layout a profile call works on.
type profileTarget struct {
	abs     string
	cfg     *layout.Config
	layouts []layoutFile
	saved   *layoutFile
}

// findProfile loads the folder and finds the layout of slot; with a base, the
// file must still have that version (ErrChanged).
func findProfile(dir string, slot byte, base string) (*profileTarget, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	cfg, err := layout.Load(abs)
	if err != nil {
		return nil, otherFile(err)
	}
	layouts, err := readLayouts(abs)
	if err != nil {
		return nil, err
	}
	saved, err := pickLayout(abs, layouts, slot)
	if err != nil {
		return nil, err
	}
	if base != "" && Version(saved.data) != base {
		return nil, ErrChanged
	}
	return &profileTarget{abs: abs, cfg: cfg, layouts: layouts, saved: saved}, nil
}

// prepare returns the writes that must come first: the migration of every
// switch_when_running (replaced gives the layouts to use in place of the ones
// on disk), or, when there is none and pin is set, games/desktop.toml at the
// profile Desktop has now, so that adding or removing a layout cannot move it.
func (t *profileTarget) prepare(replaced map[string]*layout.File, pin bool) (writes []Write, migrated bool, err error) {
	for _, p := range t.cfg.Profiles {
		migrated = migrated || p.SwitchWhenRunning != ""
	}
	if migrated {
		files := map[string]*layout.File{}
		for _, l := range t.layouts {
			if l.file != nil {
				files[l.rel] = l.file
			}
		}
		for rel, f := range replaced {
			files[rel] = f
		}
		writes, err = Migration(t.cfg, files)
		return writes, true, err
	}
	if pin && t.cfg.Desktop == nil {
		data, err := (&layout.GameFile{Name: "Desktop", Devices: map[string]any{"kb7": int64(t.cfg.DesktopProfile())}}).Encode()
		if err != nil {
			return nil, false, err
		}
		writes = []Write{{Rel: desktopRel, Data: data}}
	}
	return writes, false, nil
}

func asFiles(writes []Write) map[string][]byte {
	m := map[string][]byte{}
	for _, w := range writes {
		m[w.Rel] = w.Data
	}
	return m
}

func fileBytes(writes []Write, rel string) []byte {
	for _, w := range writes {
		if w.Rel == rel {
			return w.Data
		}
	}
	return nil
}
