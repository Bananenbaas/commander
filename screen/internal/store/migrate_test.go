// SPDX-License-Identifier: GPL-2.0-or-later

package store

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/layout"
)

// folder writes files (paths relative to the folder, slashes) into a new
// temporary folder and returns it.
func folder(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, data := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, p, []byte(data))
	}
	return dir
}

// layoutFiles decodes every layout file of dir by its relative path.
func layoutFiles(t *testing.T, dir string) map[string]*layout.File {
	t.Helper()
	m := map[string]*layout.File{}
	for _, n := range names(t, dir) {
		if filepath.Ext(n) != ".toml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			t.Fatal(err)
		}
		f, err := layout.DecodeFile(data)
		if err != nil {
			t.Fatal(err)
		}
		m[n] = f
	}
	return m
}

func migrationFolder(t *testing.T, extra map[string]string) string {
	files := map[string]string{
		"kb7-0.toml": "profile = 0\n[[button]]\npage = 1\nposition = 0\naction = [\"A\"]\n",
		"kb7-1.toml": "profile = 1\nswitch_when_running = \"StarCitizen.exe\"\n[[button]]\npage = 1\nposition = 0\naction = [\"B\"]\n",
		"kb7-2.toml": "profile = 2\nswitch_when_running = \"Desktop.exe\"\n[[button]]\npage = 1\nposition = 0\naction = [\"C\"]\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return folder(t, files)
}

func TestMigration(t *testing.T) {
	dir := migrationFolder(t, nil)
	cfg, err := layout.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := Migration(cfg, layoutFiles(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	var rels []string
	for _, w := range writes {
		rels = append(rels, w.Rel)
	}
	want := []string{"games/desktop.toml", "kb7-1.toml", "games/starcitizen.toml", "kb7-2.toml", "games/desktop-2.toml"}
	if !reflect.DeepEqual(rels, want) {
		t.Fatalf("writes = %v, want %v", rels, want)
	}

	game := func(i int) *layout.GameFile {
		g, err := layout.DecodeGameFile(writes[i].Data)
		if err != nil {
			t.Fatalf("%s: %v", writes[i].Rel, err)
		}
		return g
	}
	if g := game(0); g.Devices["kb7"] != int64(0) || g.Program != "" {
		t.Errorf("desktop = %+v, want kb7 0 and no program", g)
	}
	if g := game(2); g.Name != "StarCitizen" || g.Program != "StarCitizen.exe" || g.Devices["kb7"] != int64(1) {
		t.Errorf("starcitizen = %+v", g)
	}
	if g := game(4); g.Name != "Desktop 2" || g.Program != "Desktop.exe" || g.Devices["kb7"] != int64(2) {
		t.Errorf("desktop-2 = %+v", g)
	}
	for _, i := range []int{1, 3} {
		f, err := layout.DecodeFile(writes[i].Data)
		if err != nil {
			t.Fatalf("%s: %v", writes[i].Rel, err)
		}
		if f.SwitchWhenRunning != "" {
			t.Errorf("%s still has switch_when_running %q", writes[i].Rel, f.SwitchWhenRunning)
		}
		if len(f.Buttons) != 1 {
			t.Errorf("%s lost its buttons: %+v", writes[i].Rel, f.Buttons)
		}
	}

	files := map[string][]byte{}
	var last *layout.Config
	for _, w := range writes {
		files[w.Rel] = w.Data
		if last, err = layout.LoadWith(dir, files); err != nil {
			t.Fatalf("after writing %s the folder does not load: %v", w.Rel, err)
		}
	}
	for _, p := range last.Profiles {
		if p.SwitchWhenRunning != "" {
			t.Errorf("%s still has switch_when_running", p.File)
		}
	}
	if p, g := last.Choose([]string{"StarCitizen.exe"}); p != 1 || g == nil || g.Name != "StarCitizen" {
		t.Errorf("Choose(StarCitizen.exe) = %d, %+v; want profile 1", p, g)
	}
	if p, _ := last.Choose(nil); p != 0 {
		t.Errorf("desktop profile = %d, want 0", p)
	}
}

func TestMigrationKeepsExistingDesktop(t *testing.T) {
	dir := migrationFolder(t, map[string]string{"games/desktop.toml": "[devices]\nkb7 = 0\n"})
	cfg, err := layout.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := Migration(cfg, layoutFiles(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range writes {
		if w.Rel == "games/desktop.toml" {
			t.Fatal("an existing games/desktop.toml must not be written")
		}
	}
	if len(writes) != 4 || writes[0].Rel != "kb7-1.toml" {
		t.Fatalf("writes start with %q, %d writes", writes[0].Rel, len(writes))
	}
}

func TestMigrationNames(t *testing.T) {
	dir := folder(t, map[string]string{
		"kb7-0.toml":               "profile = 0\n",
		"kb7-1.toml":               "profile = 1\nswitch_when_running = 'C:\\Games\\Star Citizen!.EXE'\n",
		"kb7-2.toml":               "profile = 2\nswitch_when_running = \"star_citizen-.exe\"\n",
		"kb7-3.toml":               "profile = 3\nswitch_when_running = \".exe\"\n",
		"games/star-citizen-.toml": "name = \"Elite\"\nprogram = \"elite.exe\"\n[devices]\n",
	})
	cfg, err := layout.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := Migration(cfg, layoutFiles(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, w := range writes {
		if g, err := layout.DecodeGameFile(w.Data); err == nil && g.Program != "" {
			got[w.Rel] = g.Name
		}
	}
	want := map[string]string{
		"games/star-citizen--2.toml": "Star Citizen!",
		"games/star_citizen-.toml":   "star_citizen-",
		"games/game.toml":            "Game",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("games = %v, want %v", got, want)
	}
}
