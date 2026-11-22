// SPDX-License-Identifier: GPL-2.0-or-later

package control

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/bridge"
	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/run"
	"github.com/Bananenbaas/commander/screen/internal/supervisor"
)

const profile0 = `
profile = 0

[light]
fps = 30

[[light.layer]]
effect = "wave"
colors = ["#0040FF", "#00C0FF"]
speed = 0.5
direction = "right"

[[button]]
page = 1
position = 0
title = "Browser"
action = ["A"]

[[button]]
page = 1
position = 1
title = "Two"
action = ["B"]
`

const gameSC = `
name = "Star Citizen"
program = "StarCitizen.exe"

[devices]
kb7 = 2
`

func writeFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "desk.toml"), []byte(profile0), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "games"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "games", "sc.toml"), []byte(gameSC), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

type fake struct {
	autostart, updates bool
	theme              string
	sets               []string
	checked, installed int
	opened             []string
	tag, url           string
}

func (f *fake) deps(dir string) Deps {
	return Deps{
		Version:      "1.2.3",
		Dir:          dir,
		Load:         layout.Load,
		Autostart:    func() (bool, error) { return f.autostart, nil },
		SetAutostart: func(on bool) error { f.autostart = on; f.sets = append(f.sets, "autostart"); return nil },
		CheckUpdates: func() (bool, error) { return f.updates, nil },
		SetCheckUpdates: func(on bool) error {
			f.updates = on
			f.sets = append(f.sets, "updates")
			return nil
		},
		Theme:    func() (string, error) { return f.theme, nil },
		SetTheme: func(s string) error { f.theme = s; f.sets = append(f.sets, "theme"); return nil },
		CheckNow: func(context.Context) { f.checked++; f.tag, f.url = "v2.0.0", "https://example.invalid/r" },
		Offered: func() (string, string, bool) {
			return f.tag, f.url, f.tag != ""
		},
		Install: func(context.Context) { f.installed++ },
		Open:    func(p string) error { f.opened = append(f.opened, p); return nil },
		LogPath: "/logs/commander.log",
	}
}

func call(t *testing.T, s *Service, name, args string) map[string]any {
	t.Helper()
	var raw json.RawMessage
	if args != "" {
		raw = json.RawMessage(args)
	}
	v, err := s.Handle(context.Background(), name, raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %s: %v", name, b, err)
	}
	return m
}

var running = supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 1, Name: "SC"}}

func TestStateCall(t *testing.T) {
	dir := writeFolder(t)
	s := New((&fake{}).deps(dir))
	s.SetState(running)
	m := call(t, s, "state", "")
	if m["version"] != "1.2.3" || m["phase"] != "running" || m["status"] != "SC" || m["failed"] != false || m["error"] != "" {
		t.Errorf("head: %v", m)
	}
	if m["folder"] != dir || m["loadError"] != "" {
		t.Errorf("folder: %v", m)
	}
	kb := m["keyboard"].(map[string]any)
	if kb["connected"] != true || kb["waiting"] != "" || kb["profile"] != float64(1) || kb["name"] != "SC" {
		t.Errorf("keyboard: %v", kb)
	}
	ps := m["profiles"].([]any)
	if len(ps) != 1 {
		t.Fatalf("profiles: %v", ps)
	}
	p := ps[0].(map[string]any)
	if p["number"] != float64(0) || p["buttons"] != float64(2) || p["lit"] != true || p["file"] == "" || p["name"] != "" {
		t.Errorf("profile: %v", p)
	}
	gs := m["games"].([]any)
	if len(gs) != 1 {
		t.Fatalf("games: %v", gs)
	}
	g := gs[0].(map[string]any)
	if g["name"] != "Star Citizen" || g["program"] != "StarCitizen.exe" || g["kb7"] != float64(2) || g["file"] == "" {
		t.Errorf("game: %v", g)
	}
	d := m["desktop"].(map[string]any)
	if d["name"] != "Desktop" || d["kb7"] != float64(0) {
		t.Errorf("desktop: %v", d)
	}
}

func TestStateNotConnectedWhileWaiting(t *testing.T) {
	s := New((&fake{}).deps(writeFolder(t)))
	s.SetState(supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: -1, Waiting: "keyboard"}})
	kb := call(t, s, "state", "")["keyboard"].(map[string]any)
	if kb["connected"] != false || kb["waiting"] != "keyboard" {
		t.Errorf("keyboard: %v", kb)
	}
}

func TestStateWithLoadError(t *testing.T) {
	d := (&fake{}).deps(t.TempDir())
	d.Load = func(string) (*layout.Config, error) { return nil, os.ErrNotExist }
	s := New(d)
	s.SetState(supervisor.State{Phase: supervisor.Failed, Run: run.Status{Profile: -1}, Err: "boom"})
	m := call(t, s, "state", "")
	if m["loadError"] == "" || m["failed"] != true || m["error"] != "boom" {
		t.Errorf("m: %v", m)
	}
	if ps, ok := m["profiles"].([]any); !ok || len(ps) != 0 {
		t.Errorf("profiles: %v", m["profiles"])
	}
	if gs, ok := m["games"].([]any); !ok || len(gs) != 0 {
		t.Errorf("games: %v", m["games"])
	}
}

func TestStateEventOnSetState(t *testing.T) {
	s := New((&fake{}).deps(writeFolder(t)))
	s.SetState(running)
	select {
	case msg := <-s.Events():
		if msg.Event != "state" {
			t.Fatalf("event %q", msg.Event)
		}
		var ev map[string]any
		if err := json.Unmarshal(msg.Data, &ev); err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(ev)
		b, _ := json.Marshal(call(t, s, "state", ""))
		if string(a) != string(b) {
			t.Errorf("event %s != call %s", a, b)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}

func drain(s *Service) (last bridge.Message) {
	for len(s.Events()) > 0 {
		last = <-s.Events()
	}
	return last
}

func TestSetStateNeverBlocks(t *testing.T) {
	s := New((&fake{}).deps(writeFolder(t)))
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			s.SetState(supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: i % 5}})
			s.Show()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked")
	}
	if last := drain(s); last.Event != "show" {
		t.Errorf("last event %q", last.Event)
	}
}

func TestLatestStateKept(t *testing.T) {
	s := New((&fake{}).deps(writeFolder(t)))
	for i := 0; i < 50; i++ {
		s.SetState(supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 1, Name: "n" + string(rune('A'+i%26))}})
	}
	last := drain(s)
	if !strings.Contains(string(last.Data), `"name":"nX"`) { // i = 49 -> 'A'+23
		t.Errorf("latest state lost: %s", last.Data)
	}
}

func TestSaveSettingsAppliesOnlyGivenFields(t *testing.T) {
	f := &fake{theme: "light"}
	s := New(f.deps(writeFolder(t)))
	m := call(t, s, "saveSettings", `{"checkUpdates":true}`)
	if len(f.sets) != 1 || f.sets[0] != "updates" {
		t.Errorf("sets: %v", f.sets)
	}
	if m["checkUpdates"] != true || m["startWithWindows"] != false || m["theme"] != "light" {
		t.Errorf("m: %v", m)
	}
	if m["folder"] == "" || m["logPath"] != "/logs/commander.log" {
		t.Errorf("m: %v", m)
	}
	m = call(t, s, "saveSettings", `{"startWithWindows":true,"theme":"dark"}`)
	if !f.autostart || f.theme != "dark" || m["theme"] != "dark" {
		t.Errorf("f: %+v m: %v", f, m)
	}
}

func TestSettingsMapsUnknownTheme(t *testing.T) {
	f := &fake{theme: "purple"}
	s := New(f.deps(writeFolder(t)))
	if m := call(t, s, "settings", ""); m["theme"] != "system" {
		t.Errorf("theme %v", m["theme"])
	}
}

func TestSaveSettingsRefusesBadTheme(t *testing.T) {
	f := &fake{theme: "light"}
	s := New(f.deps(writeFolder(t)))
	_, err := s.Handle(context.Background(), "saveSettings", json.RawMessage(`{"checkUpdates":true,"theme":"purple"}`))
	if err == nil || !strings.Contains(err.Error(), "saveSettings") {
		t.Errorf("err %v", err)
	}
	if f.theme != "light" || len(f.sets) != 0 {
		t.Errorf("something changed: %+v", f)
	}
	_, err = s.Handle(context.Background(), "saveSettings", json.RawMessage(`{bad`))
	if err == nil || !strings.Contains(err.Error(), "saveSettings") {
		t.Errorf("err %v", err)
	}
}

func TestUpdateCalls(t *testing.T) {
	f := &fake{}
	s := New(f.deps(writeFolder(t)))
	m := call(t, s, "update", "")
	if m["current"] != "1.2.3" || m["available"] != "" || m["url"] != "" {
		t.Errorf("update: %v", m)
	}
	m = call(t, s, "checkUpdate", "")
	if f.checked != 1 || m["available"] != "v2.0.0" || m["url"] != "https://example.invalid/r" {
		t.Errorf("checkUpdate: %v", m)
	}
	m = call(t, s, "installUpdate", "")
	if f.installed != 1 || m["available"] != "v2.0.0" {
		t.Errorf("installUpdate: %v", m)
	}
}

func TestOpenCalls(t *testing.T) {
	f := &fake{}
	dir := writeFolder(t)
	s := New(f.deps(dir))
	for _, c := range []string{"openFolder", "openLog"} {
		v, err := s.Handle(context.Background(), c, nil)
		if err != nil || v != nil {
			t.Errorf("%s: %v %v", c, v, err)
		}
	}
	if len(f.opened) != 2 || f.opened[0] != dir || f.opened[1] != "/logs/commander.log" {
		t.Errorf("opened %v", f.opened)
	}
}

func TestUnknownCall(t *testing.T) {
	s := New((&fake{}).deps(writeFolder(t)))
	_, err := s.Handle(context.Background(), "nope", nil)
	if err == nil || err.Error() != `unknown call "nope"` {
		t.Errorf("err %v", err)
	}
}

func TestShowEvent(t *testing.T) {
	s := New((&fake{}).deps(writeFolder(t)))
	s.Show()
	select {
	case msg := <-s.Events():
		if msg.Event != "show" {
			t.Errorf("event %q", msg.Event)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}
