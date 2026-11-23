// SPDX-License-Identifier: GPL-2.0-or-later

package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/layout"
)

const noRoom = "The KB7 has five profiles and all five are in use."

func TestSaveProfileName(t *testing.T) {
	hand := "# mine\nprofile = 1\nname = \"Old\"\n[[button]]\npage = 1\nposition = 0\ntitle = \"A\"\naction = [\"A\"]\n"
	dir := folder(t, map[string]string{"kb7-0.toml": "profile = 0\n", "kb7-1.toml": hand})
	res, err := SaveProfileName(dir, 1, fileVersion(t, dir, "kb7-1.toml"), "  Flight ")
	if err != nil {
		t.Fatal(err)
	}
	if res.Change != Apply || !res.Written || len(res.Backups) != 1 || res.Migrated {
		t.Fatalf("result = %+v", res)
	}
	if res.Version != fileVersion(t, dir, "kb7-1.toml") {
		t.Errorf("Version = %q, want the file's", res.Version)
	}
	if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(res.Backups[0]))); err != nil || string(b) != hand {
		t.Errorf("backup does not hold the original (%v)", err)
	}
	if p := profileOf(mustLoad(t, dir), 1); p.Name != "Flight" || len(p.Buttons) != 1 {
		t.Errorf("profile = %+v", p)
	}

	// An empty name removes the name.
	if _, err = SaveProfileName(dir, 1, res.Version, ""); err != nil {
		t.Fatal(err)
	}
	if p := profileOf(mustLoad(t, dir), 1); p.Name != "" {
		t.Errorf("name = %q, want none", p.Name)
	}
	if f := fileOf(t, dir, "kb7-1.toml"); f.Name != nil {
		t.Errorf("name key = %q, want absent", *f.Name)
	}
}

func TestSaveProfileNameRefuses(t *testing.T) {
	dir := folder(t, map[string]string{"kb7-0.toml": "profile = 0\n"})
	base := fileVersion(t, dir, "kb7-0.toml")
	before := snapshot(t, dir)
	for name, want := range map[string]string{
		strings.Repeat("x", 20): "A name on the keyboard is at most 19 characters.",
		"Café":                  "The keyboard can only show plain letters, digits and symbols (no accents or emoji).",
	} {
		if _, err := SaveProfileName(dir, 0, base, name); err == nil || err.Error() != want {
			t.Errorf("SaveProfileName(%q) = %v, want %q", name, err, want)
		}
	}
	if _, err := SaveProfileName(dir, 0, "0000000000000000", "Ok"); !errors.Is(err, ErrChanged) {
		t.Errorf("stale base: %v", err)
	}
	if _, err := SaveProfileName(dir, 3, base, "Ok"); err == nil || err.Error() != "There is no layout for profile 4." {
		t.Errorf("missing slot: %v", err)
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
		t.Errorf("a refused save changed the folder")
	}
}

func TestSaveProfileNameMigrates(t *testing.T) {
	dir := migrationFolder(t, nil)
	_, data, err := LayoutFile(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	res, err := SaveProfileName(dir, 0, Version(data), "Desk")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Migrated || res.Change != Apply {
		t.Errorf("result = %+v", res)
	}
	after := mustLoad(t, dir)
	for _, p := range after.Profiles {
		if p.SwitchWhenRunning != "" {
			t.Errorf("profile %d still has switch_when_running", p.Number)
		}
	}
	if p := profileOf(after, 0); p.Name != "Desk" {
		t.Errorf("name = %q", p.Name)
	}
	if len(after.Games) != 2 {
		t.Errorf("games = %+v", after.Games)
	}
}

func TestDuplicateProfile(t *testing.T) {
	dir := folder(t, map[string]string{
		"kb7-desk.toml": "profile = 0\nname = \"Desk\"\n[[button]]\npage = 1\nposition = 0\ntitle = \"A\"\naction = [\"A\"]\n",
		"kb7-4.toml":    "profile = 4\nname = \"Nineteen chars long\"\n",
	})
	n, res, err := DuplicateProfile(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || res.Change != Apply || !res.Written || res.Migrated {
		t.Fatalf("n = %d, result = %+v", n, res)
	}
	data, err := os.ReadFile(filepath.Join(dir, "kb7-2.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), layout.Header) {
		t.Errorf("no header:\n%s", data)
	}
	if res.Version != Version(data) {
		t.Errorf("Version = %q", res.Version)
	}
	cfg := mustLoad(t, dir)
	if p := profileOf(cfg, 1); p == nil || p.Name != "Desk copy" || len(p.Buttons) != 1 || p.Buttons[0].Title != "A" {
		t.Errorf("copy = %+v", p)
	}
	if p := profileOf(cfg, 0); p.Name != "Desk" {
		t.Errorf("original = %+v", p)
	}

	// The next copy takes the next free slot; a name that does not fit with
	// " copy" is cut to fit the limit instead.
	n, _, err = DuplicateProfile(dir, 4)
	if err != nil || n != 2 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	if p := profileOf(mustLoad(t, dir), 2); p.Name != "Nineteen chars copy" {
		t.Errorf("long name = %q", p.Name)
	}
}

func TestDuplicateProfileFileNameClash(t *testing.T) {
	dir := folder(t, map[string]string{
		"kb7-1.toml":   "profile = 0\n",
		"kb7-2.toml":   "profile = 4\n",
		"kb7-2-2.toml": "profile = 3\n",
	})
	n, _, err := DuplicateProfile(dir, 0)
	if err != nil || n != 1 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "kb7-2-3.toml")); err != nil {
		t.Errorf("want kb7-2-3.toml: %v", err)
	}
	if p := profileOf(mustLoad(t, dir), 1); p == nil || p.Name != "" {
		t.Errorf("copy of an unnamed profile = %+v", p)
	}
}

func TestDuplicateProfileDropsProgramAndPinsDesktop(t *testing.T) {
	// Desktop is profile 2 (0 has a program). The copy lands in slot 1 and
	// would become Desktop if it were not pinned.
	dir := folder(t, map[string]string{
		"kb7-0.toml": "profile = 0\nswitch_when_running = \"a.exe\"\n",
		"kb7-2.toml": "profile = 2\n",
	})
	n, res, err := DuplicateProfile(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || !res.Migrated {
		t.Fatalf("n = %d, result = %+v", n, res)
	}
	cfg := mustLoad(t, dir)
	if got := cfg.DesktopProfile(); got != 2 {
		t.Errorf("Desktop moved to %d", got)
	}
	if p := profileOf(cfg, 1); p == nil || p.SwitchWhenRunning != "" {
		t.Errorf("copy = %+v", p)
	}
	if len(cfg.Games) != 1 || cfg.Games[0].Program != "a.exe" {
		t.Errorf("games = %+v", cfg.Games)
	}

	// Without a migration the pin is written when there is no desktop.toml.
	dir = folder(t, map[string]string{"kb7-2.toml": "profile = 2\n", "kb7-3.toml": "profile = 3\n"})
	if _, _, err := DuplicateProfile(dir, 3); err != nil {
		t.Fatal(err)
	}
	cfg = mustLoad(t, dir)
	if cfg.Desktop == nil || cfg.DesktopProfile() != 2 {
		t.Errorf("desktop = %+v", cfg.Desktop)
	}
}

func TestDuplicateProfileRefuses(t *testing.T) {
	files := map[string]string{}
	for _, n := range []string{"0", "1", "2", "3"} {
		files["kb7-"+n+".toml"] = "profile = " + n + "\n"
	}
	dir := folder(t, files)
	if n, _, err := DuplicateProfile(dir, 0); err != nil || n != 4 {
		t.Fatalf("last free slot: n = %d, err = %v", n, err)
	}
	before := snapshot(t, dir)
	if _, _, err := DuplicateProfile(dir, 0); err == nil || err.Error() != noRoom {
		t.Fatalf("err = %v", err)
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
		t.Error("a refused duplicate changed the folder")
	}
	dir = folder(t, map[string]string{"kb7-0.toml": "profile = 0\n"})
	if _, _, err := DuplicateProfile(dir, 2); err == nil || err.Error() != "There is no layout for profile 3." {
		t.Errorf("missing slot: %v", err)
	}
}

func TestDeleteProfile(t *testing.T) {
	dir := gameFolder(t, map[string]string{"kb7-2.toml": "profile = 2\nname = \"Spare\"\n"})
	_, data, err := LayoutFile(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	res, err := DeleteProfile(dir, 2, Version(data))
	if err != nil {
		t.Fatal(err)
	}
	if res.Change != Apply || !res.Written || len(res.Backups) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if !strings.HasPrefix(res.Backups[0], "backup/kb7-2-") {
		t.Errorf("backup = %q", res.Backups[0])
	}
	if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(res.Backups[0]))); err != nil || string(b) != string(data) {
		t.Errorf("backup does not hold the layout (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "kb7-2.toml")); !os.IsNotExist(err) {
		t.Errorf("layout still there (%v)", err)
	}
	cfg := mustLoad(t, dir)
	if len(cfg.Profiles) != 2 || profileOf(cfg, 2) != nil {
		t.Errorf("profiles = %+v", cfg.Profiles)
	}
}

func TestDeleteProfileBacksUpCommandersOwnFile(t *testing.T) {
	dir := gameFolder(t, nil)
	if _, _, err := DuplicateProfile(dir, 0); err != nil {
		t.Fatal(err)
	}
	rel, data, err := LayoutFile(dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), layout.Header) {
		t.Fatal("the copy is not in Commander's form")
	}
	res, err := DeleteProfile(dir, 2, Version(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Backups) != 1 {
		t.Fatalf("backups = %v", res.Backups)
	}
	if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(res.Backups[0]))); err != nil || string(b) != string(data) {
		t.Errorf("backup does not hold the layout (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, rel)); !os.IsNotExist(err) {
		t.Errorf("%s still there", rel)
	}
}

func TestDeleteProfileInUse(t *testing.T) {
	sc := "name = \"Star Citizen\"\nprogram = \"sc.exe\"\n[devices]\nkb7 = 1\n"
	cases := []struct {
		files map[string]string
		want  string
	}{
		{map[string]string{"games/desktop.toml": "[devices]\nkb7 = 1\n", "games/sc.toml": sc},
			"Desktop and Star Citizen use this profile; choose another profile there first."},
		{map[string]string{"games/sc.toml": sc},
			"Star Citizen uses this profile; choose another profile there first."},
		{map[string]string{"games/desktop.toml": "[devices]\nkb7 = 1\n"},
			"Desktop uses this profile; choose another profile there first."},
		{map[string]string{
			"kb7-2.toml":         "profile = 2\n",
			"games/desktop.toml": "[devices]\nkb7 = 1\n",
			"games/a.toml":       "name = \"A\"\nprogram = \"a.exe\"\n[devices]\nkb7 = 1\n",
			"games/b.toml":       "name = \"B\"\nprogram = \"b.exe\"\n[devices]\nkb7 = 1\n",
		}, "Desktop, A and B use this profile; choose another profile there first."},
	}
	for _, c := range cases {
		dir := gameFolder(t, c.files)
		before := snapshot(t, dir)
		_, err := DeleteProfile(dir, 1, fileVersion(t, dir, "kb7-1.toml"))
		if err == nil || err.Error() != c.want {
			t.Errorf("err = %v, want %q", err, c.want)
		}
		if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
			t.Error("a refused delete changed the folder")
		}
	}
}

func TestDeleteProfileUsedByLayoutGame(t *testing.T) {
	dir := migrationFolder(t, nil)
	before := snapshot(t, dir)
	_, data, _ := LayoutFile(dir, 1)
	_, err := DeleteProfile(dir, 1, Version(data))
	if want := "StarCitizen uses this profile; choose another profile there first."; err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
	// Desktop is profile 0 here, pinned by the migration.
	_, data, _ = LayoutFile(dir, 0)
	if _, err = DeleteProfile(dir, 0, Version(data)); err == nil || err.Error() != "Desktop uses this profile; choose another profile there first." {
		t.Errorf("err = %v", err)
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(after, before) {
		t.Error("a refused delete changed the folder")
	}
}

func TestDeleteProfileMigrates(t *testing.T) {
	dir := migrationFolder(t, map[string]string{"kb7-3.toml": "profile = 3\n"})
	_, data, _ := LayoutFile(dir, 3)
	res, err := DeleteProfile(dir, 3, Version(data))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Migrated {
		t.Errorf("result = %+v", res)
	}
	cfg := mustLoad(t, dir)
	if profileOf(cfg, 3) != nil || len(cfg.Games) != 2 || cfg.Desktop == nil || cfg.DesktopProfile() != 0 {
		t.Errorf("profiles = %+v, games = %+v, desktop = %+v", cfg.Profiles, cfg.Games, cfg.Desktop)
	}
}

func TestDeleteProfileKeepsDesktopPut(t *testing.T) {
	// Desktop is profile 1 (lowest without a program); deleting profile 2
	// must not move it, and deleting nothing else changes the Desktop file.
	dir := folder(t, map[string]string{
		"kb7-0.toml": "profile = 0\nswitch_when_running = \"a.exe\"\n",
		"kb7-1.toml": "profile = 1\n",
		"kb7-2.toml": "profile = 2\n",
	})
	_, data, _ := LayoutFile(dir, 2)
	if _, err := DeleteProfile(dir, 2, Version(data)); err != nil {
		t.Fatal(err)
	}
	if got := mustLoad(t, dir).DesktopProfile(); got != 1 {
		t.Errorf("Desktop = %d", got)
	}
}

func TestDeleteProfileRefuses(t *testing.T) {
	dir := folder(t, map[string]string{"kb7-0.toml": "profile = 0\n"})
	base := fileVersion(t, dir, "kb7-0.toml")
	if _, err := DeleteProfile(dir, 0, "0000000000000000"); !errors.Is(err, ErrChanged) {
		t.Errorf("stale base: %v", err)
	}
	if _, err := DeleteProfile(dir, 1, base); err == nil || err.Error() != "There is no layout for profile 2." {
		t.Errorf("missing slot: %v", err)
	}
	if _, err := DeleteProfile(dir, 0, base); err == nil || err.Error() != "The last profile cannot be deleted." {
		t.Errorf("last profile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "kb7-0.toml")); err != nil {
		t.Errorf("layout gone: %v", err)
	}
}
