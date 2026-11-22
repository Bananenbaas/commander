// SPDX-License-Identifier: GPL-2.0-or-later

package layout

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// gamesDir writes files into dir/games.
func gamesDir(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	g := filepath.Join(dir, "games")
	if err := os.MkdirAll(g, 0o755); err != nil {
		t.Fatal(err)
	}
	for n, c := range files {
		write(t, g, n, c)
	}
}

const gameSC = `
name = "Star Citizen"
program = "StarCitizen.exe"

[devices]
kb7 = 2
`

const gameDesktop = `
[devices]
kb7 = 0
`

func TestGamesLoad(t *testing.T) {
	dir := folder(t, map[string]string{
		"desktop.toml": desktopTOML,
		"two.toml":     "profile = 2\n",
	})
	gamesDir(t, dir, map[string]string{
		"desktop.toml": gameDesktop,
		"sc.toml":      gameSC,
		"music.toml":   "name = \"Music\"\nprogram = \"Spotify.exe\"\n",
	})
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Games) != 2 {
		t.Fatalf("games: %+v", c.Games)
	}
	if c.Games[0].Name != "Music" || c.Games[0].KB7 != nil {
		t.Fatalf("first game: %+v", c.Games[0])
	}
	if c.Games[1].KB7 == nil || *c.Games[1].KB7 != 2 {
		t.Fatalf("second game: %+v", c.Games[1])
	}
	if c.Games[1].File != filepath.Join(c.Dir, "games", "sc.toml") {
		t.Fatalf("file: %q", c.Games[1].File)
	}
	if c.Desktop == nil || c.Desktop.Name != "Desktop" || c.Desktop.Program != "" {
		t.Fatalf("desktop: %+v", c.Desktop)
	}
	if c.DesktopProfile() != 0 {
		t.Fatal("desktop profile")
	}
}

func TestGamesFromLayouts(t *testing.T) {
	dir := folder(t, map[string]string{"desktop.toml": desktopTOML, "sc.toml": scTOML})
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	one := byte(1)
	want := []Game{{Name: "Game.exe", Program: "Game.exe", KB7: &one, File: filepath.Join(c.Dir, "sc.toml")}}
	if !reflect.DeepEqual(c.Games, want) {
		t.Fatalf("games: %+v", c.Games)
	}
	if c.Desktop != nil {
		t.Fatalf("desktop: %+v", c.Desktop)
	}
}

func TestGamesErrors(t *testing.T) {
	const swLayout = "profile = 1\nswitch_when_running = \"STARCITIZEN\"\n"
	tests := []struct {
		name    string
		layouts map[string]string
		games   map[string]string
		want    []string
	}{
		{"no name", nil, map[string]string{"a.toml": "program = \"A.exe\"\n"}, []string{"games/a.toml", "name is required"}},
		{"no program", nil, map[string]string{"a.toml": "name = \"A\"\n"}, []string{"games/a.toml", "program is required"}},
		{"desktop with program", nil, map[string]string{"desktop.toml": "program = \"A.exe\"\n[devices]\nkb7 = 0\n"}, []string{"games/desktop.toml: the desktop game has no program"}},
		{"desktop without kb7", nil, map[string]string{"desktop.toml": "name = \"Home\"\n"}, []string{"kb7 is required"}},
		{"kb7 too high", nil, map[string]string{"a.toml": "name = \"A\"\nprogram = \"A.exe\"\n[devices]\nkb7 = 5\n"}, []string{"kb7 must be 0 to 4"}},
		{"unknown key", nil, map[string]string{"a.toml": "name = \"A\"\nprogram = \"A.exe\"\ncolour = 1\n"}, []string{"games/a.toml", "unknown key"}},
		{"same program", nil, map[string]string{
			"a.toml": "name = \"A\"\nprogram = \"StarCitizen.exe\"\n",
			"b.toml": "name = \"B\"\nprogram = \"starcitizen\"\n",
		}, []string{"games/a.toml", "games/b.toml"}},
		{"same name", nil, map[string]string{
			"a.toml": "name = \"Game\"\nprogram = \"A.exe\"\n",
			"b.toml": "name = \"game\"\nprogram = \"B.exe\"\n",
		}, []string{"games/a.toml", "games/b.toml"}},
		{"layout program is a game's name", map[string]string{"o.toml": "profile = 1\nswitch_when_running = \"Other.exe\"\n"}, map[string]string{
			"x.toml": "name = \"Other.exe\"\nprogram = \"x.exe\"\n",
		}, []string{"o.toml", "games/x.toml"}},
		{"game named like the desktop", nil, map[string]string{
			"desktop.toml": gameDesktop,
			"a.toml":       "name = \"Desktop\"\nprogram = \"A.exe\"\n",
		}, []string{"games/a.toml", "games/desktop.toml"}},
		{"layout program in a game", map[string]string{"sc.toml": swLayout}, map[string]string{
			"star.toml": "name = \"SC\"\nprogram = \"StarCitizen.exe\"\n",
		}, []string{"sc.toml", "games/star.toml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layouts := map[string]string{"desktop.toml": desktopTOML}
			for k, v := range tt.layouts {
				layouts[k] = v
			}
			dir := folder(t, layouts)
			gamesDir(t, dir, tt.games)
			_, err := Load(dir)
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
		})
	}
}

func TestGamesIgnoresOtherDevicesAndFiles(t *testing.T) {
	dir := folder(t, map[string]string{"desktop.toml": desktopTOML})
	gamesDir(t, dir, map[string]string{
		"a.toml":     "name = \"A\"\nprogram = \"A.exe\"\n[devices]\nkb7 = 1\neverest = 3\n",
		"readme.txt": "not a game",
	})
	old := filepath.Join(dir, "games", "old")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, old, "x.toml", "this is not toml at all")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Games) != 1 || c.Games[0].KB7 == nil || *c.Games[0].KB7 != 1 {
		t.Fatalf("games: %+v", c.Games)
	}
}

func TestGamesWithoutDesktopFile(t *testing.T) {
	dir := folder(t, map[string]string{"desktop.toml": desktopTOML})
	gamesDir(t, dir, map[string]string{"a.toml": "name = \"A\"\nprogram = \"A.exe\"\n"})
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Desktop != nil || len(c.Games) != 1 {
		t.Fatalf("desktop %+v games %+v", c.Desktop, c.Games)
	}
	if c.DesktopProfile() != 0 {
		t.Fatal("desktop profile")
	}
}

func TestGamesDesktopLiftsTheFiveProgramRule(t *testing.T) {
	layouts := map[string]string{}
	games := map[string]string{"desktop.toml": gameDesktop}
	for _, n := range []string{"0", "1", "2", "3", "4"} {
		layouts["p"+n+".toml"] = "profile = " + n + "\nswitch_when_running = \"P" + n + ".exe\"\n"
		games["g"+n+".toml"] = "name = \"G" + n + "\"\nprogram = \"P" + n + ".exe\"\n"
	}
	dir := folder(t, layouts)
	if _, err := Load(dir); err == nil {
		t.Fatal("five programs without a games folder must be refused")
	}
	gamesDir(t, dir, games)
	for n := range 5 {
		// The programs now live in games/; the layouts no longer name them.
		write(t, dir, fmt.Sprintf("p%d.toml", n), fmt.Sprintf("profile = %d\n", n))
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Desktop == nil || c.DesktopProfile() != 0 || len(c.Games) != 5 {
		t.Fatalf("desktop %+v games %+v", c.Desktop, c.Games)
	}
}

func TestGamesFromLayoutsComeAfterFilesByProfile(t *testing.T) {
	dir := folder(t, map[string]string{
		"desktop.toml": desktopTOML,
		"p3.toml":      "profile = 3\nswitch_when_running = \"Three.exe\"\n",
		"p1.toml":      "profile = 1\nswitch_when_running = \"One.exe\"\n",
	})
	gamesDir(t, dir, map[string]string{"zz.toml": "name = \"Zed\"\nprogram = \"Zed.exe\"\n"})
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, g := range c.Games {
		got = append(got, g.Name)
	}
	if want := []string{"Zed", "One.exe", "Three.exe"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
}

func testGames() []Game {
	two, three := byte(2), byte(3)
	return []Game{
		{Name: "A", Program: "a.exe", KB7: &two},
		{Name: "B", Program: "b.exe"},
		{Name: "C", Program: "c.exe", KB7: &three},
	}
}

func TestStartOrder(t *testing.T) {
	g := testGames()
	run := func(names ...string) map[string]bool {
		m := map[string]bool{}
		for _, n := range names {
			m[n] = true
		}
		return m
	}
	tests := []struct {
		name    string
		prev    []string
		running map[string]bool
		want    []string
	}{
		{"same poll uses games order", nil, run("b.exe", "a.exe"), []string{"a.exe", "b.exe"}},
		{"new program goes in front", []string{"a.exe"}, run("a.exe", "c.exe"), []string{"c.exe", "a.exe"}},
		{"stopped programs drop out", []string{"c.exe", "a.exe"}, run("a.exe"), []string{"a.exe"}},
		{"nothing running", []string{"a.exe"}, run(), nil},
	}
	for _, tc := range tests {
		got := StartOrder(tc.prev, tc.running, g)
		if len(got) != len(tc.want) || (len(got) > 0 && !reflect.DeepEqual(got, tc.want)) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestChoose(t *testing.T) {
	zero := byte(0)
	g := testGames()
	c := &Config{Games: g, Desktop: &Game{Name: "Desktop", KB7: &zero}}
	tests := []struct {
		name  string
		order []string
		want  byte
		game  *Game
	}{
		{"nothing running", nil, 0, nil},
		{"most recent game with a profile", []string{"c.exe", "a.exe"}, 3, &g[2]},
		{"game without kb7 does not switch", []string{"b.exe", "a.exe"}, 2, &g[0]},
		{"only a game without kb7", []string{"b.exe"}, 0, nil},
	}
	for _, tc := range tests {
		p, got := c.Choose(tc.order)
		if p != tc.want || got != tc.game && (got == nil || tc.game == nil || got.Name != tc.game.Name) {
			t.Errorf("%s: got %d, %v; want %d, %v", tc.name, p, got, tc.want, tc.game)
		}
	}
}

func TestChooseLegacyFolder(t *testing.T) {
	c, err := Load(folder(t, map[string]string{"desktop.toml": desktopTOML, "sc.toml": scTOML}))
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := c.Choose([]string{"Game.exe"}); p != 1 {
		t.Errorf("running: got %d, want 1", p)
	}
	if p, g := c.Choose(nil); p != 0 || g != nil {
		t.Errorf("idle: got %d, %v, want 0, nil", p, g)
	}
}

func TestDesktopProfileSkipsProfilesGamesClaim(t *testing.T) {
	dir := folder(t, map[string]string{
		"desktop.toml": "profile = 2\n",
		"sc.toml":      "profile = 1\n",
	})
	gamesDir(t, dir, map[string]string{"sc.toml": "name = \"SC\"\nprogram = \"SC.exe\"\n[devices]\nkb7 = 1\n"})
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.DesktopProfile(); got != 2 {
		t.Fatalf("desktop profile %d, want 2", got)
	}
}

func TestGamesFilesClaimingAllFiveProfilesAreRefused(t *testing.T) {
	dir := folder(t, map[string]string{"p0.toml": "profile = 0\n"})
	games := map[string]string{}
	for n := range 5 {
		games[fmt.Sprintf("g%d.toml", n)] = fmt.Sprintf("name = \"G%d\"\nprogram = \"P%d.exe\"\n[devices]\nkb7 = %d\n", n, n, n)
	}
	gamesDir(t, dir, games)
	if _, err := Load(dir); err == nil {
		t.Fatal("games naming all five profiles without a desktop game must be refused")
	}
}

func TestGamesFilesSortIgnoringCase(t *testing.T) {
	dir := folder(t, map[string]string{"desktop.toml": desktopTOML})
	gamesDir(t, dir, map[string]string{
		"Zed.toml":  "name = \"Zed\"\nprogram = \"Zed.exe\"\n",
		"apex.toml": "name = \"Apex\"\nprogram = \"Apex.exe\"\n",
	})
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Games) != 2 || c.Games[0].Name != "Apex" || c.Games[1].Name != "Zed" {
		t.Fatalf("games: %+v", c.Games)
	}
}

func TestTwoDesktopGameFilesAreRefused(t *testing.T) {
	dir := folder(t, map[string]string{"desktop.toml": desktopTOML})
	gamesDir(t, dir, map[string]string{"desktop.toml": gameDesktop})
	gdir := filepath.Join(dir, "games")
	write(t, gdir, "Desktop.toml", gameDesktop)
	entries, err := os.ReadDir(gdir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Skip("case-insensitive file system")
	}
	_, err = Load(dir)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, w := range []string{"games/Desktop.toml", "games/desktop.toml"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q lacks %q", err, w)
		}
	}
}
