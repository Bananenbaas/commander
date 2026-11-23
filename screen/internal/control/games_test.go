// SPDX-License-Identifier: GPL-2.0-or-later

package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/audio"
	"github.com/Bananenbaas/commander/screen/internal/procs"
	"github.com/Bananenbaas/commander/screen/internal/store"
)

// filesFolder writes files (relative path to content) into a new folder.
func filesFolder(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// gamesFolder has two layouts and Star Citizen on profile 1; Desktop has no
// file of its own.
func gamesFolder(t *testing.T) string {
	return filesFolder(t, map[string]string{
		"kb7-1.toml":    "profile = 0\nname = \"Desk\"\n",
		"kb7-2.toml":    "profile = 1\n",
		"games/sc.toml": "name = \"Star Citizen\"\nprogram = \"StarCitizen.exe\"\n\n[devices]\nkb7 = 1\n",
	})
}

func versionOf(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return store.Version(data)
}

func countingDeps(t *testing.T, dir string) (*Service, *counts) {
	t.Helper()
	c := &counts{}
	d := (&fake{}).deps(dir)
	d.Reload = func() { c.reload++ }
	d.Apply = func() { c.apply++ }
	return newService(t, d), c
}

func argsOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

var wantProfiles = []any{
	map[string]any{"number": float64(0), "name": "Desk"},
	map[string]any{"number": float64(1), "name": ""},
}

func TestGameProfileDesktopWithoutFile(t *testing.T) {
	s, _ := countingDeps(t, gamesFolder(t))
	m := call(t, s, "gameProfile", `{"id":"desktop"}`)
	want := map[string]any{
		"id": "desktop", "file": "", "version": "", "name": "Desktop", "program": "",
		"kb7": float64(0), "desktop": true, "profiles": wantProfiles,
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("got  %v\nwant %v", m, want)
	}
}

func TestGameProfileDesktopFile(t *testing.T) {
	dir := gamesFolder(t)
	if err := os.WriteFile(filepath.Join(dir, "games", "desktop.toml"), []byte("name = \"Home\"\n\n[devices]\nkb7 = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, _ := countingDeps(t, dir)
	m := call(t, s, "gameProfile", `{"id":"games/desktop.toml"}`)
	if m["id"] != "desktop" || m["file"] != "games/desktop.toml" || m["version"] != versionOf(t, dir, "games/desktop.toml") ||
		m["name"] != "Home" || m["kb7"] != float64(1) || m["desktop"] != true {
		t.Errorf("desktop: %v", m)
	}
}

func TestGameProfileFile(t *testing.T) {
	dir := gamesFolder(t)
	s, _ := countingDeps(t, dir)
	m := call(t, s, "gameProfile", `{"id":"games/sc.toml"}`)
	want := map[string]any{
		"id": "games/sc.toml", "file": "games/sc.toml", "version": versionOf(t, dir, "games/sc.toml"),
		"name": "Star Citizen", "program": "StarCitizen.exe", "kb7": float64(1), "desktop": false, "profiles": wantProfiles,
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("got  %v\nwant %v", m, want)
	}
}

func TestGameProfileFromLayout(t *testing.T) {
	dir := filesFolder(t, map[string]string{
		"kb7-1.toml": "profile = 0\n",
		"kb7-2.toml": "profile = 1\nswitch_when_running = \"Elite.exe\"\n",
	})
	s, _ := countingDeps(t, dir)
	m := call(t, s, "gameProfile", `{"id":"layout:Elite.exe"}`)
	if m["id"] != "layout:Elite.exe" || m["file"] != "kb7-2.toml" || m["version"] != "" ||
		m["program"] != "Elite.exe" || m["kb7"] != float64(1) || m["desktop"] != false || m["name"] == "" {
		t.Errorf("layout game: %v", m)
	}

	st := call(t, s, "state", "")
	g := st["games"].([]any)[0].(map[string]any)
	if g["id"] != "layout:Elite.exe" {
		t.Errorf("state game id %v", g["id"])
	}
}

func TestGameProfileNew(t *testing.T) {
	s, _ := countingDeps(t, gamesFolder(t))
	m := call(t, s, "gameProfile", `{"id":""}`)
	want := map[string]any{
		"id": "", "file": "", "version": "", "name": "", "program": "",
		"kb7": nil, "desktop": false, "profiles": wantProfiles,
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("got  %v\nwant %v", m, want)
	}
}

func TestGameProfileUnknown(t *testing.T) {
	s, _ := countingDeps(t, gamesFolder(t))
	for _, args := range []string{`{"id":"games/gone.toml"}`, `{"id":"layout:Gone.exe"}`, `{"id":"kb7-1.toml"}`, `nope`} {
		if _, err := s.Handle(context.Background(), "gameProfile", json.RawMessage(args)); err == nil {
			t.Errorf("%s: no error", args)
		}
	}
}

func TestStateGameIDs(t *testing.T) {
	s, _ := countingDeps(t, gamesFolder(t))
	g := call(t, s, "state", "")["games"].([]any)[0].(map[string]any)
	if g["id"] != "games/sc.toml" {
		t.Errorf("id %v", g["id"])
	}
}

func TestSaveGameProfileNew(t *testing.T) {
	dir := gamesFolder(t)
	s, c := countingDeps(t, dir)
	m := call(t, s, "saveGameProfile", `{"id":"","version":"","name":"Elite Dangerous","program":"EliteDangerous64.exe","kb7":0}`)
	if m["ok"] != true || m["id"] != "games/elite-dangerous.toml" || m["change"] != "reload" || m["migrated"] != false {
		t.Fatalf("answer: %v", m)
	}
	if m["version"] != versionOf(t, dir, "games/elite-dangerous.toml") {
		t.Errorf("version %v", m["version"])
	}
	if _, ok := m["backups"].([]any); !ok {
		t.Errorf("backups %v", m["backups"])
	}
	if c.reload != 1 || c.apply != 0 {
		t.Errorf("reload %d apply %d", c.reload, c.apply)
	}
	got := call(t, s, "gameProfile", `{"id":"games/elite-dangerous.toml"}`)
	if got["name"] != "Elite Dangerous" || got["program"] != "EliteDangerous64.exe" || got["kb7"] != float64(0) {
		t.Errorf("saved: %v", got)
	}
}

func TestSaveGameProfileDontSwitch(t *testing.T) {
	dir := gamesFolder(t)
	s, c := countingDeps(t, dir)
	m := call(t, s, "saveGameProfile", argsOf(t, map[string]any{
		"id": "games/sc.toml", "version": versionOf(t, dir, "games/sc.toml"),
		"name": "Star Citizen", "program": "StarCitizen.exe", "kb7": nil,
	}))
	if m["ok"] != true || m["id"] != "games/sc.toml" || c.reload != 1 {
		t.Fatalf("answer: %v reload %d", m, c.reload)
	}
	if got := call(t, s, "gameProfile", `{"id":"games/sc.toml"}`); got["kb7"] != nil {
		t.Errorf("kb7 %v", got["kb7"])
	}
}

func TestSaveGameProfileRefused(t *testing.T) {
	dir := gamesFolder(t)
	s, c := countingDeps(t, dir)
	m := call(t, s, "saveGameProfile", `{"id":"","version":"","name":" ","program":"x.exe","kb7":0}`)
	if m["ok"] != false || m["error"] != "Give the game profile a name." || m["changed"] != nil {
		t.Errorf("empty name: %v", m)
	}
	m = call(t, s, "saveGameProfile", `{"id":"","version":"","name":"Other","program":"StarCitizen.exe","kb7":0}`)
	if m["ok"] != false || m["error"] != "Star Citizen already switches on StarCitizen.exe." {
		t.Errorf("same program: %v", m)
	}
	m = call(t, s, "saveGameProfile", `{"id":"games/sc.toml","version":"stale","name":"Star Citizen","program":"StarCitizen.exe","kb7":1}`)
	want := map[string]any{"ok": false, "changed": true, "error": "This game profile was changed outside Commander since you opened it."}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("stale: %v", m)
	}
	if c.reload != 0 || c.apply != 0 {
		t.Errorf("reload %d apply %d after refusals", c.reload, c.apply)
	}
	for _, args := range []string{`{"id":"","name":"A","program":"a.exe","kb7":5}`, `{"id":"","name":"A","program":"a.exe","kb7":-1}`, `[]`} {
		if _, err := s.Handle(context.Background(), "saveGameProfile", json.RawMessage(args)); err == nil {
			t.Errorf("%s: no error", args)
		}
	}
}

func TestDeleteGameProfile(t *testing.T) {
	dir := gamesFolder(t)
	s, c := countingDeps(t, dir)
	m := call(t, s, "deleteGameProfile", `{"id":"desktop","version":""}`)
	if m["ok"] != false || m["error"] != "Desktop cannot be deleted." || c.reload != 0 {
		t.Errorf("desktop: %v reload %d", m, c.reload)
	}
	m = call(t, s, "deleteGameProfile", `{"id":"games/sc.toml","version":"stale"}`)
	if m["ok"] != false || m["changed"] != true || c.reload != 0 {
		t.Errorf("stale: %v reload %d", m, c.reload)
	}
	m = call(t, s, "deleteGameProfile", argsOf(t, map[string]any{"id": "games/sc.toml", "version": versionOf(t, dir, "games/sc.toml")}))
	if m["ok"] != true || m["change"] != "reload" || c.reload != 1 || c.apply != 0 {
		t.Fatalf("delete: %v reload %d apply %d", m, c.reload, c.apply)
	}
	if _, err := os.Stat(filepath.Join(dir, "games", "sc.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("games/sc.toml still there: %v", err)
	}
}

func TestRunningPrograms(t *testing.T) {
	t.Setenv("SystemRoot", `C:\Windows`)
	d := (&fake{}).deps(gamesFolder(t))
	d.Running = func() ([]procs.Proc, error) {
		return []procs.Proc{
			{Name: "System"},
			{Name: "Idle"},
			{Name: "Registry"},
			{Name: "svchost.exe", Path: `C:\Windows\System32\svchost.exe`},
			{Name: "SVCHOST.EXE"},
			{Name: "csrss.exe"},
			{Name: "explorer.exe", Path: `c:\windows\explorer.exe`},
			{Name: "notepad.exe", Path: `C:\WINDOWS\system32\notepad.exe`},
			{Name: "StarCitizen.exe", Path: `D:\Games\StarCitizen\Bin64\StarCitizen.exe`},
			{Name: "Discord.exe", Path: `C:\Users\p\AppData\Local\Discord\Discord.exe`},
			{Name: "discord.exe", Path: `C:\Users\p\AppData\Local\Discord\app\discord.exe`},
			{Name: "Commander.exe", Path: `C:\Program Files\Commander\commander.exe`},
			{Name: "kb7screen.exe"},
			{Name: "steam.exe"},
			{Name: "WindowsTerminal.exe", Path: `C:\WindowsApps\WindowsTerminal.exe`},
			{Name: ""},
		}, nil
	}
	s := newService(t, d)
	m := call(t, s, "runningPrograms", "")
	want := []any{"Discord.exe", "StarCitizen.exe", "steam.exe", "WindowsTerminal.exe"}
	if !reflect.DeepEqual(m["programs"], want) {
		t.Errorf("got  %v\nwant %v", m["programs"], want)
	}

	d.Running = func() ([]procs.Proc, error) { return nil, errors.New("no access") }
	if _, err := newService(t, d).Handle(context.Background(), "runningPrograms", nil); err == nil {
		t.Error("no error from a failed list")
	}
	d.Running = nil
	if m := call(t, newService(t, d), "runningPrograms", ""); !reflect.DeepEqual(m["programs"], []any{}) {
		t.Errorf("without Running: %v", m)
	}
}

func TestMixer(t *testing.T) {
	d := (&fake{}).deps(gamesFolder(t))
	none := map[string]any{
		"available": false,
		"system":    map[string]any{"name": "", "level": float64(0), "muted": false},
		"programs":  []any{},
	}
	if m := call(t, newService(t, d), "mixer", ""); !reflect.DeepEqual(m, none) {
		t.Errorf("nil Mixer: %v", m)
	}
	d.Mixer = func() (audio.Session, []audio.Session, bool, error) { return audio.Session{}, nil, false, nil }
	if m := call(t, newService(t, d), "mixer", ""); !reflect.DeepEqual(m, none) {
		t.Errorf("no audio: %v", m)
	}
	d.Mixer = func() (audio.Session, []audio.Session, bool, error) {
		return audio.Session{}, nil, true, errors.New("closed")
	}
	if m := call(t, newService(t, d), "mixer", ""); !reflect.DeepEqual(m, none) {
		t.Errorf("failing audio: %v", m)
	}

	d.Mixer = func() (audio.Session, []audio.Session, bool, error) {
		return audio.Session{ID: "sys", Name: "Speakers", Level: 40, Muted: true},
			[]audio.Session{
				{ID: "1", Name: "Discord", Level: 80, Active: true},
				{ID: "2", Name: "Spotify", Level: 100, Muted: true},
			}, true, nil
	}
	want := map[string]any{
		"available": true,
		"system":    map[string]any{"name": "Speakers", "level": float64(40), "muted": true},
		"programs": []any{
			map[string]any{"name": "Discord", "level": float64(80), "muted": false, "active": true},
			map[string]any{"name": "Spotify", "level": float64(100), "muted": true, "active": false},
		},
	}
	if m := call(t, newService(t, d), "mixer", ""); !reflect.DeepEqual(m, want) {
		t.Errorf("got  %v\nwant %v", m, want)
	}
}

func TestMixerLogsAFailureOnce(t *testing.T) {
	d := (&fake{}).deps(gamesFolder(t))
	var lines []string
	d.Logf = func(format string, args ...any) { lines = append(lines, fmt.Sprintf(format, args...)) }
	var err error
	ok := true
	d.Mixer = func() (audio.Session, []audio.Session, bool, error) { return audio.Session{}, nil, ok, err }
	s := newService(t, d)

	err = errors.New("no output device")
	for range 3 {
		call(t, s, "mixer", "")
	}
	if len(lines) != 1 {
		t.Fatalf("three failing reads: %q", lines)
	}
	err = nil
	call(t, s, "mixer", "")
	err = errors.New("no output device")
	call(t, s, "mixer", "")
	if len(lines) != 2 {
		t.Fatalf("a success, then the failure again: %q", lines)
	}
	// ok=false also forgets the failure.
	err, ok = nil, false
	call(t, s, "mixer", "")
	err, ok = errors.New("no output device"), true
	call(t, s, "mixer", "")
	call(t, s, "mixer", "")
	if len(lines) != 3 {
		t.Errorf("no audio, then the failure twice: %q", lines)
	}
}
