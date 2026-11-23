// SPDX-License-Identifier: GPL-2.0-or-later

package control

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/store"
)

// usedFolder has Desktop and Star Citizen on profile 1 and nothing on
// profile 0.
func usedFolder(t *testing.T) string {
	return filesFolder(t, map[string]string{
		"kb7-1.toml":         "profile = 0\nname = \"Desk\"\n",
		"kb7-2.toml":         "profile = 1\nname = \"Games\"\n",
		"games/desktop.toml": "name = \"Desktop\"\n\n[devices]\nkb7 = 1\n",
		"games/sc.toml":      "name = \"Star Citizen\"\nprogram = \"StarCitizen.exe\"\n\n[devices]\nkb7 = 1\n",
	})
}

func TestProfileInfo(t *testing.T) {
	dir := usedFolder(t)
	s, _ := countingDeps(t, dir)
	m := call(t, s, "profileInfo", `{"profile":1}`)
	want := map[string]any{
		"profile": float64(1), "name": "Games", "version": versionOf(t, dir, "kb7-2.toml"),
		"usedBy": []any{"Desktop", "Star Citizen"}, "canDuplicate": true,
	}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("got  %v\nwant %v", m, want)
	}
	m = call(t, s, "profileInfo", `{"profile":0}`)
	if !reflect.DeepEqual(m["usedBy"], []any{}) || m["name"] != "Desk" {
		t.Errorf("profile 0: %v", m)
	}

	// Desktop without a file of its own is on the lowest profile no game uses.
	if err := os.Remove(filepath.Join(dir, "games", "desktop.toml")); err != nil {
		t.Fatal(err)
	}
	if m := call(t, s, "profileInfo", `{"profile":0}`); !reflect.DeepEqual(m["usedBy"], []any{"Desktop"}) {
		t.Errorf("profile 0 without desktop.toml: %v", m)
	}
	for _, args := range []string{`{"profile":2}`, `{"profile":5}`, `{"profile":-1}`, `{}`, `x`} {
		if _, err := s.Handle(context.Background(), "profileInfo", json.RawMessage(args)); err == nil {
			t.Errorf("%s: no error", args)
		}
	}
}

func TestProfileInfoFull(t *testing.T) {
	files := map[string]string{}
	for n := range 5 {
		files[string(rune('a'+n))+".toml"] = "profile = " + string(rune('0'+n)) + "\n"
	}
	s, _ := countingDeps(t, filesFolder(t, files))
	if m := call(t, s, "profileInfo", `{"profile":4}`); m["canDuplicate"] != false {
		t.Errorf("five layouts: %v", m)
	}
	m := call(t, s, "duplicateProfile", `{"profile":4}`)
	if m["ok"] != false || m["error"] != "The KB7 has five profiles and all five are in use." {
		t.Errorf("sixth: %v", m)
	}
}

func TestSaveProfileName(t *testing.T) {
	dir := usedFolder(t)
	s, c := countingDeps(t, dir)
	m := call(t, s, "saveProfileName", `{"profile":1,"version":"stale","name":"New"}`)
	want := map[string]any{"ok": false, "changed": true, "error": "This profile was changed outside Commander since you opened it."}
	if !reflect.DeepEqual(m, want) {
		t.Errorf("stale: %v", m)
	}
	v := versionOf(t, dir, "kb7-2.toml")
	m = call(t, s, "saveProfileName", argsOf(t, map[string]any{"profile": 1, "version": v, "name": "A name far too long for the keyboard"}))
	if m["ok"] != false || m["error"] == "" || m["changed"] != nil {
		t.Errorf("long name: %v", m)
	}
	if c.apply != 0 || c.reload != 0 {
		t.Errorf("apply %d reload %d after refusals", c.apply, c.reload)
	}
	m = call(t, s, "saveProfileName", argsOf(t, map[string]any{"profile": 1, "version": v, "name": "Flight"}))
	if m["ok"] != true || m["change"] != "apply" || m["migrated"] != false || c.apply != 1 || c.reload != 0 {
		t.Fatalf("save: %v apply %d reload %d", m, c.apply, c.reload)
	}
	if _, ok := m["backups"].([]any); !ok {
		t.Errorf("backups %v", m["backups"])
	}
	if got := call(t, s, "profileInfo", `{"profile":1}`); got["name"] != "Flight" || got["version"] != m["version"] {
		t.Errorf("after save: %v", got)
	}
}

func TestDuplicateProfile(t *testing.T) {
	dir := usedFolder(t)
	s, c := countingDeps(t, dir)
	m := call(t, s, "duplicateProfile", `{"profile":1}`)
	if m["ok"] != true || m["newProfile"] != float64(2) || m["change"] != "apply" || c.apply != 1 || c.reload != 0 {
		t.Fatalf("duplicate: %v apply %d reload %d", m, c.apply, c.reload)
	}
	if got := call(t, s, "profileInfo", `{"profile":2}`); got["name"] != "Games copy" {
		t.Errorf("copy: %v", got)
	}
	if _, err := s.Handle(context.Background(), "duplicateProfile", json.RawMessage(`{"profile":9}`)); err == nil {
		t.Error("profile 9: no error")
	}
}

func TestDeleteProfile(t *testing.T) {
	dir := usedFolder(t)
	s, c := countingDeps(t, dir)
	m := call(t, s, "deleteProfile", argsOf(t, map[string]any{"profile": 1, "version": versionOf(t, dir, "kb7-2.toml")}))
	want := map[string]any{"ok": false, "error": "Desktop and Star Citizen use this profile; choose another profile there first."}
	if !reflect.DeepEqual(m, want) || c.apply != 0 || c.reload != 0 {
		t.Errorf("used: %v apply %d reload %d", m, c.apply, c.reload)
	}
	m = call(t, s, "deleteProfile", `{"profile":0,"version":"stale"}`)
	if m["ok"] != false || m["changed"] != true {
		t.Errorf("stale: %v", m)
	}
	m = call(t, s, "deleteProfile", argsOf(t, map[string]any{"profile": 0, "version": versionOf(t, dir, "kb7-1.toml")}))
	if m["ok"] != true || m["change"] != "apply" || c.apply != 1 || c.reload != 0 {
		t.Fatalf("delete: %v apply %d reload %d", m, c.apply, c.reload)
	}
	if b, _ := m["backups"].([]any); len(b) != 1 {
		t.Errorf("backups %v", m["backups"])
	}
	if _, err := os.Stat(filepath.Join(dir, "kb7-1.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("kb7-1.toml still there: %v", err)
	}
}

func TestDeleteProfileFailureAfterWriteReloads(t *testing.T) {
	s, c := countingDeps(t, usedFolder(t))
	orig := deleteLayout
	t.Cleanup(func() { deleteLayout = orig })
	deleteLayout = func(string, byte, string) (store.Result, error) {
		return store.Result{Written: true, Change: store.Apply}, errors.New("disk full")
	}
	m := call(t, s, "deleteProfile", `{"profile":0,"version":"v"}`)
	if m["ok"] != false || m["error"] != "disk full" || c.reload != 1 || c.apply != 0 {
		t.Errorf("partial write: %v reload %d apply %d", m, c.reload, c.apply)
	}
}

func TestProfileCallsNeedAVersion(t *testing.T) {
	s, c := countingDeps(t, usedFolder(t))
	for _, call := range []string{"saveProfileName", "deleteProfile"} {
		for _, args := range []string{`{"profile":0,"name":"X"}`, `{"profile":0,"version":"","name":"X"}`} {
			if _, err := s.Handle(context.Background(), call, json.RawMessage(args)); err == nil {
				t.Errorf("%s %s: no error", call, args)
			}
		}
	}
	if c.apply != 0 || c.reload != 0 {
		t.Errorf("apply %d reload %d", c.apply, c.reload)
	}
}
