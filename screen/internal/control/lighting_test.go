// SPDX-License-Identifier: GPL-2.0-or-later

package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/store"
)

// lightSimple fits the simple view: a wave and a key state.
const lightSimple = `
profile = 0

[light]

[[light.layer]]
effect = "wave"
colors = ["#0040FF", "#00C0FF"]
speed = 1.5

[[light.layer]]
effect = "state"
name = "caps"
toggle = "CapsLock"
keys = ["CapsLock"]
color = "#FF0000"

[[button]]
page = 1
position = 0
title = "Caps"
state = "caps"
`

// lightAdvanced does not: the wave's length is changed, an integer, and a
// layer covers a group of LEDs.
const lightAdvanced = `
profile = 0

[light]
fps = 20

[[light.layer]]
effect = "wave"
colors = ["#0040FF", "#00C0FF"]
length = 20

[[light.layer]]
effect = "fixed"
keys = ["wrist"]
color = "#00FF00"

[stored_light]
color = "#112233"
brightness = 50

[[button]]
page = 1
position = 0
title = "Browser"
action = ["A"]
`

func lightDeps(t *testing.T, content string) (*Service, string, *counts) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "desk.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &counts{}
	d := (&fake{}).deps(dir)
	d.Reload = func() { c.reload++ }
	d.Apply = func() { c.apply++ }
	return newService(t, d), dir, c
}

func lightCall(t *testing.T, s *Service) map[string]any {
	t.Helper()
	return call(t, s, "deviceLight", `{"device":"kb7","profile":0}`)
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func savedFile(t *testing.T, dir string) *layout.File {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "desk.toml"))
	if err != nil {
		t.Fatal(err)
	}
	f, err := layout.DecodeFile(data)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLightingInfo(t *testing.T) {
	s, _, _ := lightDeps(t, lightSimple)
	m := call(t, s, "lightingInfo", "")
	if m["frameMs"] != float64(50) || m["frames"] != float64(80) {
		t.Errorf("clock: %v %v", m["frameMs"], m["frames"])
	}
	var wave map[string]any
	for _, e := range m["effects"].([]any) {
		if e := e.(map[string]any); e["name"] == "wave" {
			wave = e
		}
	}
	if wave == nil {
		t.Fatalf("no wave in %v", m["effects"])
	}
	if wave["needsKeys"] != false || wave["help"] == "" {
		t.Errorf("wave: %v", wave)
	}
	kinds := map[string]string{}
	for _, st := range wave["settings"].([]any) {
		st := st.(map[string]any)
		kinds[st["name"].(string)] = st["kind"].(string)
	}
	want := map[string]string{"colors": "colors", "speed": "number", "direction": "choice", "length": "number"}
	if !reflect.DeepEqual(kinds, want) {
		t.Errorf("wave settings %v, want %v", kinds, want)
	}
	speed := wave["settings"].([]any)[1].(map[string]any)
	if speed["name"] != "speed" || speed["default"] != 0.5 || speed["min"] != 0.05 || speed["max"] != float64(5) || speed["required"] != false {
		t.Errorf("speed: %v", speed)
	}
	leds := m["leds"].([]any)
	if len(leds) < 90 || len(leds) > 112 {
		t.Errorf("%d LEDs", len(leds))
	}
	first := leds[0].(map[string]any)
	if first["index"] != float64(0) || first["name"] != "Escape" || first["x"] != float64(0) || first["y"] != float64(0) {
		t.Errorf("first LED %v", first)
	}
	for _, l := range leds {
		if l.(map[string]any)["name"] == "" {
			t.Errorf("LED without a name: %v", l)
		}
	}
}

func TestDeviceLightSimple(t *testing.T) {
	s, _, _ := lightDeps(t, lightSimple)
	m := lightCall(t, s)
	if v, _ := m["version"].(string); len(v) != 16 {
		t.Errorf("version %q", m["version"])
	}
	simple, ok := m["simple"].(map[string]any)
	if !ok || m["reason"] != "" {
		t.Fatalf("simple %v reason %v", m["simple"], m["reason"])
	}
	if simple["base"] != "wave" || simple["speed"] != 1.5 || len(simple["states"].([]any)) != 1 {
		t.Errorf("simple: %v", simple)
	}
	if !reflect.DeepEqual(m["states"], []any{"caps"}) {
		t.Errorf("states %v", m["states"])
	}
	if !reflect.DeepEqual(m["stored"], map[string]any{"mode": "still", "color": "", "brightness": nil}) {
		t.Errorf("stored %v", m["stored"])
	}
	l := m["light"].(map[string]any)
	layers := l["layers"].([]any)
	if l["fps"] != float64(0) || len(layers) != 2 {
		t.Fatalf("light %v", l)
	}
	st := layers[1].(map[string]any)
	if st["effect"] != "state" || st["enabled"] != true || !reflect.DeepEqual(st["keys"], []any{"CapsLock"}) ||
		!reflect.DeepEqual(st["leds"], []any{float64(42)}) {
		t.Errorf("state layer %v", st)
	}
	if !reflect.DeepEqual(st["settings"], map[string]any{"name": "caps", "toggle": "CapsLock", "color": "#FF0000"}) {
		t.Errorf("state settings %v", st["settings"])
	}
	wave := layers[0].(map[string]any)
	if !reflect.DeepEqual(wave["keys"], []any{}) || !reflect.DeepEqual(wave["leds"], []any{}) {
		t.Errorf("wave keys %v leds %v", wave["keys"], wave["leds"])
	}
}

func TestDeviceLightAdvanced(t *testing.T) {
	s, _, _ := lightDeps(t, lightAdvanced)
	m := lightCall(t, s)
	if m["simple"] != nil || !strings.Contains(m["reason"].(string), "frame rate") {
		t.Errorf("simple %v reason %v", m["simple"], m["reason"])
	}
	if !reflect.DeepEqual(m["stored"], map[string]any{"mode": "color", "color": "#112233", "brightness": float64(50)}) {
		t.Errorf("stored %v", m["stored"])
	}
	fixed := m["light"].(map[string]any)["layers"].([]any)[1].(map[string]any)
	if leds := fixed["leds"].([]any); len(leds) != 10 || leds[0] != float64(96) {
		t.Errorf("wrist leds %v", leds)
	}

	// No [light]: light null, an empty simple view.
	s, _, _ = lightDeps(t, "profile = 0\n")
	m = lightCall(t, s)
	if m["light"] != nil || m["simple"].(map[string]any)["base"] != "" || !reflect.DeepEqual(m["states"], []any{}) {
		t.Errorf("no light: %v", m)
	}
}

func TestDeviceLightErrors(t *testing.T) {
	s, _, _ := lightDeps(t, lightSimple)
	for _, args := range []string{`{"device":"x","profile":0}`, `{"device":"kb7","profile":3}`, `[`} {
		if _, err := s.Handle(t.Context(), "deviceLight", json.RawMessage(args)); err == nil {
			t.Errorf("%s: no error", args)
		}
	}
}

func TestSaveDeviceLightSimple(t *testing.T) {
	s, dir, c := lightDeps(t, lightSimple)
	m := lightCall(t, s)
	simple := map[string]any{"base": "static", "colors": []string{"#ff0000"}, "states": m["simple"].(map[string]any)["states"]}
	args := map[string]any{"device": "kb7", "profile": 0, "version": m["version"], "simple": simple, "stored": m["stored"]}
	out := call(t, s, "saveDeviceLight", jsonOf(t, args))
	if out["ok"] != true || out["change"] != "apply" || c.apply != 1 || c.reload != 0 {
		t.Fatalf("answer %v %+v", out, *c)
	}
	// The fixture is hand-written, so it is backed up first.
	if bs, _ := out["backups"].([]any); len(bs) != 1 || !strings.HasPrefix(bs[0].(string), "backup/desk-") {
		t.Errorf("backups %v", out["backups"])
	}
	f := savedFile(t, dir)
	want := []map[string]any{
		{"effect": "static", "color": "#FF0000"},
		{"effect": "state", "name": "caps", "toggle": "CapsLock", "keys": []any{"CapsLock"}, "color": "#FF0000"},
	}
	if f.Light == nil || !reflect.DeepEqual(f.Light.Layers, want) || f.StoredLight != nil {
		t.Errorf("saved light %+v stored %+v", f.Light, f.StoredLight)
	}
	if again := lightCall(t, s); again["version"] != out["version"] {
		t.Errorf("version %v, answered %v", again["version"], out["version"])
	}
}

func TestSaveDeviceLightUnchangedAdvanced(t *testing.T) {
	s, dir, c := lightDeps(t, lightAdvanced)
	m := lightCall(t, s)
	args := map[string]any{"device": "kb7", "profile": 0, "version": m["version"], "light": m["light"], "stored": m["stored"]}
	out := call(t, s, "saveDeviceLight", jsonOf(t, args))
	if out["ok"] != true || out["change"] != "none" || c.apply+c.reload != 0 {
		t.Fatalf("answer %v %+v", out, *c)
	}
	f := savedFile(t, dir)
	if v := f.Light.Layers[0]["length"]; v != int64(20) {
		t.Errorf("length %#v, want int64 20", v)
	}
	if v := f.Light.Layers[1]["keys"]; !reflect.DeepEqual(v, []any{"wrist"}) {
		t.Errorf("keys %#v", v)
	}
	if f.Light.FPS != 20 || f.StoredLight == nil || f.StoredLight.Color != "#112233" {
		t.Errorf("light %+v stored %+v", f.Light, f.StoredLight)
	}
	if again := lightCall(t, s); again["version"] != out["version"] || again["version"] != m["version"] {
		t.Errorf("versions %v %v %v", m["version"], out["version"], again["version"])
	}
}

func TestSaveDeviceLightPickedKeys(t *testing.T) {
	s, dir, c := lightDeps(t, lightAdvanced)
	m := lightCall(t, s)
	layers := m["light"].(map[string]any)["layers"].([]any)
	fixed := layers[1].(map[string]any)
	fixed["leds"] = []int{14, 0}
	off := layers[0].(map[string]any)
	off["enabled"] = false
	off["settings"].(map[string]any)["length"] = 30
	args := map[string]any{"device": "kb7", "profile": 0, "version": m["version"], "light": m["light"], "stored": m["stored"]}
	out := call(t, s, "saveDeviceLight", jsonOf(t, args))
	// The profile has a stored colour: a change to [light] is a reload.
	if out["ok"] != true || out["change"] != "reload" || c.reload != 1 || c.apply != 0 {
		t.Fatalf("answer %v %+v", out, *c)
	}
	f := savedFile(t, dir)
	if v := f.Light.Layers[1]["keys"]; !reflect.DeepEqual(v, []any{"Escape", "Grave"}) {
		t.Errorf("keys %#v", v)
	}
	if f.Light.Layers[0]["enabled"] != false {
		t.Errorf("enabled %#v", f.Light.Layers[0]["enabled"])
	}
	if v := f.Light.Layers[0]["length"]; v != int64(30) && v != float64(30) {
		t.Errorf("length %#v", v)
	}

	// An LED that does not exist is an error at its layer.
	m = lightCall(t, s)
	m["light"].(map[string]any)["layers"].([]any)[1].(map[string]any)["leds"] = []int{54}
	args = map[string]any{"device": "kb7", "profile": 0, "version": m["version"], "light": m["light"]}
	out = call(t, s, "saveDeviceLight", jsonOf(t, args))
	errs, _ := out["errors"].([]any)
	if out["ok"] != false || len(errs) != 1 || errs[0].(map[string]any)["layer"] != float64(2) || errs[0].(map[string]any)["setting"] != "keys" {
		t.Errorf("unknown LED: %v", out)
	}
}

func TestSaveDeviceLightRemove(t *testing.T) {
	s, dir, c := lightDeps(t, lightAdvanced)
	m := lightCall(t, s)
	args := map[string]any{"device": "kb7", "profile": 0, "version": m["version"], "light": nil,
		"stored": map[string]any{"mode": "still"}}
	out := call(t, s, "saveDeviceLight", jsonOf(t, args))
	if out["ok"] != true || out["change"] != "apply" || c.apply != 1 {
		t.Fatalf("answer %v %+v", out, *c)
	}
	if f := savedFile(t, dir); f.Light != nil || f.StoredLight != nil {
		t.Errorf("light %+v stored %+v", f.Light, f.StoredLight)
	}
}

func TestSaveDeviceLightErrors(t *testing.T) {
	s, _, c := lightDeps(t, lightSimple)
	m := lightCall(t, s)
	v := m["version"]

	// A wave of one colour: the error names the layer and the setting.
	simple := map[string]any{"base": "wave", "colors": []string{"#FF0000"}}
	out := call(t, s, "saveDeviceLight", jsonOf(t, map[string]any{"device": "kb7", "profile": 0, "version": v, "simple": simple}))
	errs, _ := out["errors"].([]any)
	if out["ok"] != false || len(errs) != 1 {
		t.Fatalf("one colour wave: %v", out)
	}
	if e := errs[0].(map[string]any); e["layer"] != float64(1) || e["setting"] != "colors" || !strings.Contains(e["message"].(string), "two colours") {
		t.Errorf("error %v", e)
	}

	// Removing the state a button uses is a sentence.
	simple = map[string]any{"base": "static", "colors": []string{"#FF0000"}}
	out = call(t, s, "saveDeviceLight", jsonOf(t, map[string]any{"device": "kb7", "profile": 0, "version": v, "simple": simple}))
	if out["ok"] != false || !strings.Contains(out["error"].(string), "caps") {
		t.Errorf("state in use: %v", out)
	}

	// A stale version.
	out = call(t, s, "saveDeviceLight", jsonOf(t, map[string]any{"device": "kb7", "profile": 0, "version": "0000000000000000", "simple": m["simple"]}))
	if out["ok"] != false || out["changed"] != true || out["error"] != "This profile's lighting was changed outside Commander since you opened it." {
		t.Errorf("stale: %v", out)
	}
	if c.apply+c.reload != 0 {
		t.Errorf("refusals reloaded or applied: %+v", *c)
	}

	// Neither or both of simple and light is a bad call.
	for _, args := range []string{
		`{"device":"kb7","profile":0,"version":"x"}`,
		`{"device":"kb7","profile":0,"version":"x","simple":{},"light":null}`,
	} {
		if _, err := s.Handle(t.Context(), "saveDeviceLight", json.RawMessage(args)); err == nil {
			t.Errorf("%s: no error", args)
		}
	}
}

func TestSaveDeviceLightKeepsButtonsVersion(t *testing.T) {
	s, _, _ := lightDeps(t, lightSimple)
	before := profileCall(t, s)["version"]
	m := lightCall(t, s)
	simple := m["simple"].(map[string]any)
	simple["colors"] = []string{"#111111", "#222222"}
	out := call(t, s, "saveDeviceLight", jsonOf(t, map[string]any{"device": "kb7", "profile": 0, "version": m["version"], "simple": simple}))
	if out["ok"] != true {
		t.Fatalf("save: %v", out)
	}
	if after := profileCall(t, s)["version"]; after != before {
		t.Errorf("buttons version %v became %v", before, after)
	}
}

func TestSaveDeviceLightFailureAfterWriteReloads(t *testing.T) {
	s, _, c := lightDeps(t, lightSimple)
	m := lightCall(t, s)
	args := jsonOf(t, map[string]any{"device": "kb7", "profile": 0, "version": m["version"], "simple": m["simple"]})
	orig := saveProfile
	t.Cleanup(func() { saveProfile = orig })
	saveProfile = func(string, byte, map[string]string, store.Edit) (store.Result, error) {
		return store.Result{Written: true}, errors.New("disk full")
	}
	out := call(t, s, "saveDeviceLight", args)
	if out["ok"] != false || out["error"] != "disk full" || c.reload != 1 {
		t.Fatalf("after a partial write: %v %+v", out, *c)
	}
	saveProfile = func(string, byte, map[string]string, store.Edit) (store.Result, error) {
		return store.Result{}, errors.New("disk full")
	}
	call(t, s, "saveDeviceLight", args)
	if c.reload != 1 {
		t.Errorf("reload = %d after a failure that wrote nothing", c.reload)
	}
}

func TestLightFrames(t *testing.T) {
	s, _, _ := lightDeps(t, lightSimple)
	red := `{"light":{"layers":[{"effect":"static","settings":{"color":"#FF0000"}}]}}`
	m := call(t, s, "lightFrames", red)
	frames := m["frames"].([]any)
	if len(frames) != 80 {
		t.Fatalf("%d frames", len(frames))
	}
	b, err := base64.StdEncoding.DecodeString(frames[0].(string))
	if err != nil || len(b) != 112*3 {
		t.Fatalf("frame: %d bytes, %v", len(b), err)
	}
	if b[0] != 0xFF || b[1] != 0 || b[2] != 0 {
		t.Errorf("first LED % X", b[:3])
	}
	if b[54*3] != 0 { // slot 54 has no LED
		t.Errorf("unmapped LED % X", b[54*3:54*3+3])
	}
	if simple, ok := m["simple"].(map[string]any); !ok || simple["base"] != "static" {
		t.Errorf("simple %v", m["simple"])
	}

	// A draft that does not fit the simple view still draws.
	m = call(t, s, "lightFrames", `{"light":{"fps":20,"layers":[{"effect":"static","settings":{"color":"#FF0000"}}]}}`)
	if m["simple"] != nil || len(m["frames"].([]any)) != 80 {
		t.Errorf("advanced draft: simple %v, %d frames", m["simple"], len(m["frames"].([]any)))
	}

	// From the simple view.
	m = call(t, s, "lightFrames", `{"simple":{"base":"static","colors":["#00FF00"]}}`)
	b, _ = base64.StdEncoding.DecodeString(m["frames"].([]any)[0].(string))
	if len(b) != 336 || b[0] != 0 || b[1] != 0xFF || b[2] != 0 {
		t.Errorf("simple green: % X", b[:3])
	}
	// The stack Commander writes for a simple draft, as deviceLight gives it.
	m = call(t, s, "lightFrames", `{"simple":{"base":"breathing","colors":["#00FF00"],"period":1.2345,"highlight":{"keys":["W"],"color":"#FFFFFF"}}}`)
	lj, _ := m["light"].(map[string]any)
	layers, _ := lj["layers"].([]any)
	if len(layers) != 2 {
		t.Fatalf("light of a simple draft: %v", m["light"])
	}
	if st := layers[0].(map[string]any)["settings"].(map[string]any); st["period"] != "1.2345s" || st["colors"].([]any)[0] != "#00FF00" {
		t.Errorf("breathing settings %v", st)
	}
	if hl := layers[1].(map[string]any); hl["effect"] != "fixed" || len(hl["leds"].([]any)) != 1 {
		t.Errorf("highlight layer %v", hl)
	}

	// A broken draft is an answer with errors.
	m = call(t, s, "lightFrames", `{"light":{"layers":[{"effect":"static","settings":{"color":"#FF0000"}},{"effect":"wave","settings":{"colors":["#FF0000"]}}]}}`)
	errs, _ := m["errors"].([]any)
	if len(errs) != 1 || m["frames"] != nil {
		t.Fatalf("broken: %v", m)
	}
	if e := errs[0].(map[string]any); e["layer"] != float64(2) || e["setting"] != "colors" {
		t.Errorf("error %v", e)
	}
	m = call(t, s, "lightFrames", `{"light":{"layers":[{"effect":"static","enabled":false,"settings":{"color":"#FF0000"}}]}}`)
	errs, _ = m["errors"].([]any)
	if len(errs) != 1 || errs[0].(map[string]any)["layer"] != float64(0) {
		t.Errorf("all off: %v", m)
	}
}

// lightHandSimple fits the simple view but is written by hand: a whole
// speed and a setting given at its default, which the simple view's own
// [light] would not write that way.
const lightHandSimple = `
profile = 0

[light]

[[light.layer]]
effect = "wave"
colors = ["#0040FF", "#00C0FF"]
speed = 1
direction = "right"

[[light.layer]]
effect = "fixed"
enabled = true
keys = ["wrist"]
color = "#00FF00"
`

func TestSaveDeviceLightStoredOnly(t *testing.T) {
	s, dir, c := lightDeps(t, lightHandSimple)
	before := savedFile(t, dir).Light
	m := lightCall(t, s)
	if m["simple"] == nil {
		t.Fatalf("the fixture does not fit the simple view: %v", m["reason"])
	}
	var edits []store.Edit
	orig := saveProfile
	t.Cleanup(func() { saveProfile = orig })
	saveProfile = func(dir string, slot byte, bases map[string]string, e store.Edit) (store.Result, error) {
		edits = append(edits, e)
		return orig(dir, slot, bases, e)
	}
	stored := map[string]any{"mode": "color", "color": "#123456", "brightness": 40}
	out := call(t, s, "saveDeviceLight", jsonOf(t, map[string]any{"device": "kb7", "profile": 0, "version": m["version"], "stored": stored}))
	// A changed [stored_light] is an apply.
	if out["ok"] != true || out["change"] != "apply" || c.apply != 1 || c.reload != 0 {
		t.Fatalf("answer %v %+v", out, *c)
	}
	if len(edits) != 1 || edits[0].Light != nil || edits[0].StoredLight == nil {
		t.Fatalf("edits %+v, want [stored_light] only", edits)
	}
	f := savedFile(t, dir)
	if !reflect.DeepEqual(f.Light, before) {
		t.Errorf("[light] %+v, was %+v", f.Light, before)
	}
	if f.StoredLight == nil || f.StoredLight.Color != "#123456" {
		t.Errorf("stored %+v", f.StoredLight)
	}
	if again := lightCall(t, s); again["version"] != out["version"] {
		t.Errorf("version %v, answered %v", again["version"], out["version"])
	}

	// Neither the lighting nor the stored lighting is still an error.
	if _, err := s.Handle(context.Background(), "saveDeviceLight", json.RawMessage(jsonOf(t, map[string]any{"device": "kb7", "profile": 0, "version": out["version"]}))); err == nil {
		t.Error("a save of nothing went through")
	}
}

func TestSaveDeviceLightKeepsEnabledTrue(t *testing.T) {
	s, dir, _ := lightDeps(t, lightHandSimple)
	m := lightCall(t, s)
	args := map[string]any{"device": "kb7", "profile": 0, "version": m["version"], "light": m["light"]}
	out := call(t, s, "saveDeviceLight", jsonOf(t, args))
	if out["ok"] != true || out["change"] != "none" {
		t.Fatalf("answer %v", out)
	}
	f := savedFile(t, dir)
	if v, ok := f.Light.Layers[1]["enabled"]; !ok || v != true {
		t.Errorf("enabled %#v (given: %v), want an explicit true", v, ok)
	}
	if _, ok := f.Light.Layers[0]["enabled"]; ok {
		t.Errorf("enabled written for a layer that had none")
	}
}
