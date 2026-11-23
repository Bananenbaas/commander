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

func kb7(n byte) *byte { return &n }

// gameFolder has two layouts and no game files.
func gameFolder(t *testing.T, extra map[string]string) string {
	files := map[string]string{
		"kb7-0.toml": "profile = 0\n",
		"kb7-1.toml": "profile = 1\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return folder(t, files)
}

// fileVersion is the Version of the file rel of dir.
func fileVersion(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return Version(data)
}

func gameOf(t *testing.T, dir, rel string) *layout.GameFile {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), layout.Header) {
		t.Errorf("%s does not start with the header:\n%s", rel, data)
	}
	g, err := layout.DecodeGameFile(data)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestSaveGameNew(t *testing.T) {
	dir := gameFolder(t, nil)
	file, res, err := SaveGame(dir, "", "", GameEdit{Name: "Star Citizen", Program: "StarCitizen.exe", KB7: kb7(1)})
	if err != nil {
		t.Fatal(err)
	}
	if file != "games/star-citizen.toml" {
		t.Fatalf("file = %q", file)
	}
	if res.Change != Reload || res.Migrated || !res.Written || len(res.Backups) != 0 {
		t.Errorf("result = %+v", res)
	}
	if res.Version != fileVersion(t, dir, file) {
		t.Errorf("Version = %q, want the file's", res.Version)
	}
	if g := gameOf(t, dir, file); g.Name != "Star Citizen" || g.Program != "StarCitizen.exe" || g.Devices["kb7"] != int64(1) {
		t.Errorf("game = %+v", g)
	}
	cfg := mustLoad(t, dir)
	if p, g := cfg.Choose([]string{"StarCitizen.exe"}); p != 1 || g == nil || g.Name != "Star Citizen" {
		t.Errorf("Choose = %d, %+v", p, g)
	}

	// A second game of the same slug gets -2; one without a KB7 profile has none.
	file, _, err = SaveGame(dir, "", "", GameEdit{Name: "Star-Citizen", Program: "Other.exe"})
	if err != nil {
		t.Fatal(err)
	}
	if file != "games/star-citizen-2.toml" {
		t.Errorf("second file = %q", file)
	}
	if g := gameOf(t, dir, file); g.Devices["kb7"] != nil {
		t.Errorf("game without a profile = %+v", g)
	}
	// Never desktop.toml.
	dir = gameFolder(t, map[string]string{"games/desktop.toml": "name = \"Home\"\n[devices]\nkb7 = 0\n"})
	file, _, err = SaveGame(dir, "", "", GameEdit{Name: "DESKTOP", Program: "d.exe", KB7: kb7(0)})
	if err != nil {
		t.Fatal(err)
	}
	if file != "games/desktop-2.toml" {
		t.Errorf("file for a game called DESKTOP = %q", file)
	}
}

func TestSaveGameRenameKeepsFile(t *testing.T) {
	dir := gameFolder(t, map[string]string{"games/sc.toml": "name = \"SC\"\nprogram = \"StarCitizen.exe\"\n[devices]\nkb7 = 1\n"})
	file, res, err := SaveGame(dir, "games/sc.toml", fileVersion(t, dir, "games/sc.toml"),
		GameEdit{Name: "Star Citizen", Program: "StarCitizen.exe", KB7: kb7(0)})
	if err != nil {
		t.Fatal(err)
	}
	if file != "games/sc.toml" {
		t.Fatalf("file = %q", file)
	}
	if len(res.Backups) != 1 || !strings.HasPrefix(res.Backups[0], "backup/games-sc-") {
		t.Errorf("Backups = %v, want one of the hand-written file", res.Backups)
	}
	if g := gameOf(t, dir, file); g.Name != "Star Citizen" || g.Devices["kb7"] != int64(0) {
		t.Errorf("game = %+v", g)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "games"))
	if len(entries) != 2 {
		t.Errorf("games/ holds %d files, want sc.toml and the pinned desktop.toml", len(entries))
	}
}

func TestSaveGameClashes(t *testing.T) {
	dir := gameFolder(t, map[string]string{
		"games/sc.toml":    "name = \"Star Citizen\"\nprogram = \"StarCitizen.exe\"\n[devices]\nkb7 = 1\n",
		"games/elite.toml": "name = \"Elite\"\nprogram = \"elite.exe\"\n[devices]\nkb7 = 0\n",
	})
	before := snapshot(t, dir)
	cases := []struct {
		id   string
		g    GameEdit
		want string
	}{
		{"", GameEdit{Name: "Other", Program: "starcitizen.EXE", KB7: kb7(0)}, "Star Citizen already switches on StarCitizen.exe."},
		{"", GameEdit{Name: "star citizen", Program: "x.exe"}, "Another game profile is called Star Citizen."},
		{"games/elite.toml", GameEdit{Name: "Elite", Program: "C:\\Games\\StarCitizen.exe"}, "Star Citizen already switches on StarCitizen.exe."},
		{"games/elite.toml", GameEdit{Name: "STAR CITIZEN", Program: "elite.exe"}, "Another game profile is called Star Citizen."},
	}
	for _, c := range cases {
		base := ""
		if c.id != "" {
			base = fileVersion(t, dir, c.id)
		}
		_, res, err := SaveGame(dir, c.id, base, c.g)
		if err == nil || err.Error() != c.want {
			t.Errorf("SaveGame(%q, %+v) = %v, want %q", c.id, c.g, err, c.want)
		}
		if res.Written {
			t.Errorf("SaveGame(%q, %+v) wrote", c.id, c.g)
		}
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(before, after) {
		t.Errorf("files changed: %v", keys(after))
	}
}

func TestSaveGameRefusesBeforeLoading(t *testing.T) {
	// The folder does not load, so these errors come before any load.
	dir := folder(t, map[string]string{"kb7-0.toml": "profile = 9\n"})
	cases := []struct {
		id   string
		g    GameEdit
		want string
	}{
		{"", GameEdit{Name: " ", Program: "x.exe"}, "Give the game profile a name."},
		{"", GameEdit{Name: "X"}, "Choose the program that starts it."},
		{"desktop", GameEdit{Name: "Desktop"}, "Desktop needs a KB7 profile."},
	}
	for _, c := range cases {
		if _, _, err := SaveGame(dir, c.id, "", c.g); err == nil || err.Error() != c.want {
			t.Errorf("SaveGame(%q, %+v) = %v, want %q", c.id, c.g, err, c.want)
		}
	}
}

func TestSaveGameMigratesLayoutGame(t *testing.T) {
	dir := migrationFolder(t, nil)
	file, res, err := SaveGame(dir, "layout:StarCitizen.exe", "",
		GameEdit{Name: "Star Citizen", Program: "StarCitizen.exe", KB7: kb7(1)})
	if err != nil {
		t.Fatal(err)
	}
	if file != "games/starcitizen.toml" {
		t.Fatalf("file = %q, want the migrated file", file)
	}
	if !res.Migrated || res.Change != Reload {
		t.Errorf("result = %+v", res)
	}
	if g := gameOf(t, dir, file); g.Name != "Star Citizen" || g.Program != "StarCitizen.exe" || g.Devices["kb7"] != int64(1) {
		t.Errorf("game = %+v", g)
	}
	cfg := mustLoad(t, dir)
	for _, p := range cfg.Profiles {
		if p.SwitchWhenRunning != "" {
			t.Errorf("%s keeps switch_when_running", filepath.Base(p.File))
		}
	}
	if cfg.Desktop == nil || *cfg.Desktop.KB7 != 0 {
		t.Errorf("Desktop = %+v", cfg.Desktop)
	}
	if len(cfg.Games) != 2 {
		t.Errorf("games = %+v, want Star Citizen and Desktop 2", cfg.Games)
	}
	// The id of the layout game is gone now.
	if _, _, err := SaveGame(dir, "layout:StarCitizen.exe", "", GameEdit{Name: "X", Program: "StarCitizen.exe"}); !errors.Is(err, ErrChanged) {
		t.Errorf("stale layout id: %v, want ErrChanged", err)
	}
}

func TestSaveGameNewMigrates(t *testing.T) {
	dir := migrationFolder(t, nil)
	file, res, err := SaveGame(dir, "", "", GameEdit{Name: "StarCitizen", Program: "sc2.exe"})
	if err == nil || err.Error() != "Another game profile is called StarCitizen." {
		t.Fatalf("SaveGame = %q, %+v, %v", file, res, err)
	}
	file, res, err = SaveGame(dir, "", "", GameEdit{Name: "Star Citizen", Program: "sc2.exe"})
	if err != nil {
		t.Fatal(err)
	}
	if file != "games/star-citizen.toml" || !res.Migrated {
		t.Errorf("file %q, result %+v", file, res)
	}
	if len(mustLoad(t, dir).Games) != 3 {
		t.Error("want the two migrated games and the new one")
	}
}

func TestSaveDesktop(t *testing.T) {
	dir := gameFolder(t, nil)
	file, res, err := SaveGame(dir, "desktop", "", GameEdit{Name: "Desktop", KB7: kb7(1)})
	if err != nil {
		t.Fatal(err)
	}
	if file != "games/desktop.toml" || res.Change != Reload {
		t.Fatalf("file %q, result %+v", file, res)
	}
	if g := gameOf(t, dir, file); g.Program != "" || g.Devices["kb7"] != int64(1) {
		t.Errorf("desktop = %+v", g)
	}
	cfg := mustLoad(t, dir)
	if p, _ := cfg.Choose(nil); p != 1 {
		t.Errorf("desktop profile = %d, want 1", p)
	}
	// With the file there, base must be its version.
	if _, _, err := SaveGame(dir, "desktop", "", GameEdit{Name: "Desktop", KB7: kb7(0)}); !errors.Is(err, ErrChanged) {
		t.Errorf("empty base with a desktop file: %v, want ErrChanged", err)
	}
	if _, _, err := SaveGame(dir, "desktop", res.Version, GameEdit{Name: "Desktop", KB7: kb7(0)}); err != nil {
		t.Fatal(err)
	}
	if p, _ := mustLoad(t, dir).Choose(nil); p != 0 {
		t.Errorf("desktop profile = %d, want 0", p)
	}
}

func TestSaveGameStaleBase(t *testing.T) {
	dir := gameFolder(t, map[string]string{"games/sc.toml": "name = \"SC\"\nprogram = \"StarCitizen.exe\"\n[devices]\nkb7 = 1\n"})
	before := snapshot(t, dir)
	if _, _, err := SaveGame(dir, "games/sc.toml", "0000000000000000", GameEdit{Name: "SC", Program: "StarCitizen.exe"}); !errors.Is(err, ErrChanged) {
		t.Errorf("SaveGame: %v, want ErrChanged", err)
	}
	if _, _, err := SaveGame(dir, "games/gone.toml", "0000000000000000", GameEdit{Name: "SC", Program: "x.exe"}); !errors.Is(err, ErrChanged) {
		t.Errorf("SaveGame of a missing file: %v, want ErrChanged", err)
	}
	if _, err := DeleteGame(dir, "games/sc.toml", "0000000000000000"); !errors.Is(err, ErrChanged) {
		t.Errorf("DeleteGame: %v, want ErrChanged", err)
	}
	if after := snapshot(t, dir); !reflect.DeepEqual(before, after) {
		t.Errorf("files changed: %v", keys(after))
	}
}

func TestDeleteGame(t *testing.T) {
	hand := "# mine\nname = \"SC\"\nprogram = \"StarCitizen.exe\"\n[devices]\nkb7 = 1\n"
	dir := gameFolder(t, map[string]string{"games/sc.toml": hand})
	res, err := DeleteGame(dir, "games/sc.toml", fileVersion(t, dir, "games/sc.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Change != Reload || !res.Written || len(res.Backups) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(res.Backups[0]))); err != nil || string(b) != hand {
		t.Errorf("backup does not hold the original (%v)", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "games", "sc.toml")); !os.IsNotExist(err) {
		t.Errorf("games/sc.toml still there (%v)", err)
	}
	if len(mustLoad(t, dir).Games) != 0 {
		t.Error("the game still loads")
	}

	// A file Commander wrote is removed without a backup.
	file, saved, err := SaveGame(dir, "", "", GameEdit{Name: "Elite", Program: "elite.exe"})
	if err != nil {
		t.Fatal(err)
	}
	if res, err = DeleteGame(dir, file, saved.Version); err != nil {
		t.Fatal(err)
	}
	if len(res.Backups) != 0 {
		t.Errorf("Backups = %v, want none", res.Backups)
	}
}

func TestDeleteDesktop(t *testing.T) {
	dir := gameFolder(t, map[string]string{"games/desktop.toml": "[devices]\nkb7 = 0\n"})
	for _, id := range []string{"desktop", "games/desktop.toml"} {
		if _, err := DeleteGame(dir, id, fileVersion(t, dir, "games/desktop.toml")); err == nil || err.Error() != "Desktop cannot be deleted." {
			t.Errorf("DeleteGame(%q) = %v", id, err)
		}
	}
}

func TestDeleteLayoutGame(t *testing.T) {
	dir := migrationFolder(t, nil)
	res, err := DeleteGame(dir, "layout:StarCitizen.exe", "")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Migrated || res.Change != Reload {
		t.Errorf("result = %+v", res)
	}
	cfg := mustLoad(t, dir)
	if p, _ := cfg.Choose([]string{"StarCitizen.exe"}); p != 0 {
		t.Errorf("StarCitizen.exe still switches to %d", p)
	}
	if len(cfg.Games) != 1 || cfg.Games[0].Program != "Desktop.exe" {
		t.Errorf("games = %+v", cfg.Games)
	}
	if _, err := os.Stat(filepath.Join(dir, "games", "starcitizen.toml")); !os.IsNotExist(err) {
		t.Errorf("games/starcitizen.toml was written (%v)", err)
	}
}

func TestSaveDesktopByFile(t *testing.T) {
	dir := gameFolder(t, map[string]string{"games/desktop.toml": "[devices]\nkb7 = 0\n"})
	file, res, err := SaveGame(dir, "games/desktop.toml", fileVersion(t, dir, "games/desktop.toml"), GameEdit{Name: "Desktop", KB7: kb7(1)})
	if err != nil {
		t.Fatal(err)
	}
	if file != "games/desktop.toml" || res.Change != Reload {
		t.Fatalf("file %q, result %+v", file, res)
	}
	if p, _ := mustLoad(t, dir).Choose(nil); p != 1 {
		t.Errorf("desktop profile = %d, want 1", p)
	}
	if _, _, err := SaveGame(dir, "games/desktop.toml", res.Version, GameEdit{Name: "Desktop"}); err == nil || err.Error() != "Desktop needs a KB7 profile." {
		t.Errorf("Desktop without a profile by its file: %v", err)
	}
}

func TestGameChangesKeepDesktop(t *testing.T) {
	// Profile 0 is Desktop only because it is the lowest free one.
	dir := gameFolder(t, nil)
	if _, _, err := SaveGame(dir, "", "", GameEdit{Name: "Mail", Program: "mail.exe", KB7: kb7(0)}); err != nil {
		t.Fatal(err)
	}
	if g := gameOf(t, dir, "games/desktop.toml"); g.Devices["kb7"] != int64(0) || g.Program != "" {
		t.Errorf("desktop.toml = %+v, want kb7 0", g)
	}
	if p, _ := mustLoad(t, dir).Choose(nil); p != 0 {
		t.Errorf("adding a game moved Desktop to %d", p)
	}

	// Profile 0 is a game's, so Desktop is 1 until the game goes.
	dir = gameFolder(t, map[string]string{"games/sc.toml": "name = \"SC\"\nprogram = \"StarCitizen.exe\"\n[devices]\nkb7 = 0\n"})
	if p, _ := mustLoad(t, dir).Choose(nil); p != 1 {
		t.Fatalf("desktop profile before = %d, want 1", p)
	}
	if _, err := DeleteGame(dir, "games/sc.toml", fileVersion(t, dir, "games/sc.toml")); err != nil {
		t.Fatal(err)
	}
	if g := gameOf(t, dir, "games/desktop.toml"); g.Devices["kb7"] != int64(1) {
		t.Errorf("desktop.toml = %+v, want kb7 1", g)
	}
	if p, _ := mustLoad(t, dir).Choose(nil); p != 1 {
		t.Errorf("deleting a game moved Desktop to %d", p)
	}
}
