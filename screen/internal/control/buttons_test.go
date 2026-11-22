// SPDX-License-Identifier: GPL-2.0-or-later

package control

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/picture"
	"github.com/Bananenbaas/commander/screen/internal/store"
)

type counts struct{ reload, apply int }

func buttonDeps(t *testing.T) (*Service, string, *counts) {
	t.Helper()
	dir := writeFolder(t)
	c := &counts{}
	d := (&fake{}).deps(dir)
	d.Reload = func() { c.reload++ }
	d.Apply = func() { c.apply++ }
	return newService(t, d), dir, c
}

func profileCall(t *testing.T, s *Service) map[string]any {
	t.Helper()
	return call(t, s, "deviceProfile", `{"device":"kb7","profile":0}`)
}

// saveArgs builds a saveDeviceProfile call from a deviceProfile answer, with
// edit applied to its buttons.
func saveArgs(t *testing.T, p map[string]any, version string, edit func(b []any)) string {
	t.Helper()
	b := p["buttons"].([]any)
	if edit != nil {
		edit(b)
	}
	out, err := json.Marshal(map[string]any{"device": "kb7", "profile": 0, "version": version, "buttons": b})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestDeviceProfile(t *testing.T) {
	s, _, _ := buttonDeps(t)
	m := profileCall(t, s)
	if m["device"] != "kb7" || m["profile"] != float64(0) || m["pictureSlots"] != float64(15) || m["picturesUsed"] != float64(2) {
		t.Errorf("head: %v", m)
	}
	if v, _ := m["version"].(string); len(v) != 16 {
		t.Errorf("version %q", m["version"])
	}
	if st, ok := m["states"].([]any); !ok || len(st) != 0 {
		t.Errorf("states %v", m["states"])
	}
	bs := m["buttons"].([]any)
	if len(bs) != 2 {
		t.Fatalf("buttons: %v", bs)
	}
	b := bs[0].(map[string]any)
	if b["title"] != "Browser" || b["symbol"] != "" || b["picture"] != "" || b["hold"] != false || b["confirmMs"] != float64(0) {
		t.Errorf("button: %v", b)
	}
	if !reflect.DeepEqual(b["steps"], []any{map[string]any{"keys": []any{"A"}}}) {
		t.Errorf("steps: %v", b["steps"])
	}
}

func TestDeviceProfileErrors(t *testing.T) {
	s, _, _ := buttonDeps(t)
	for _, args := range []string{`{"device":"mouse","profile":0}`, `{"device":"kb7","profile":3}`, `{"device":"kb7","profile":9}`} {
		if _, err := s.Handle(context.Background(), "deviceProfile", json.RawMessage(args)); err == nil {
			t.Errorf("%s: no error", args)
		}
	}
	if _, err := s.Handle(context.Background(), "deviceProfile", json.RawMessage(`{"device":"mouse","profile":0}`)); err == nil || !strings.Contains(err.Error(), "unknown device") {
		t.Errorf("device error: %v", err)
	}
}

func TestSaveTitleAppliesStepReloads(t *testing.T) {
	s, dir, c := buttonDeps(t)
	p := profileCall(t, s)
	v := p["version"].(string)

	m := call(t, s, "saveDeviceProfile", saveArgs(t, p, v, func(b []any) { b[0].(map[string]any)["title"] = "Web" }))
	if m["ok"] != true || m["change"] != "apply" || c.apply != 1 || c.reload != 0 {
		t.Fatalf("title: %v %+v", m, *c)
	}
	if m["version"] == v || m["migrated"] != false {
		t.Errorf("answer: %v", m)
	}
	if b, ok := m["backups"].([]any); !ok || len(b) != 1 {
		t.Errorf("backups (the fixture is hand-written): %v", m["backups"])
	}
	data, err := os.ReadFile(filepath.Join(dir, "desk.toml"))
	if err != nil || !strings.Contains(string(data), `title = "Web"`) {
		t.Errorf("file: %v\n%s", err, data)
	}

	p = profileCall(t, s)
	if p["version"] != m["version"] {
		t.Errorf("version after save %v, answer %v", p["version"], m["version"])
	}
	m = call(t, s, "saveDeviceProfile", saveArgs(t, p, p["version"].(string), func(b []any) {
		b[1].(map[string]any)["steps"] = []any{map[string]any{"keys": []any{"LCtrl", "C"}}}
	}))
	if m["ok"] != true || m["change"] != "reload" || c.apply != 1 || c.reload != 1 {
		t.Fatalf("step: %v %+v", m, *c)
	}
}

func TestSaveStale(t *testing.T) {
	s, dir, c := buttonDeps(t)
	p := profileCall(t, s)
	f := filepath.Join(dir, "desk.toml")
	data, _ := os.ReadFile(f)
	if err := os.WriteFile(f, append(data, "\n# edited\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	m := call(t, s, "saveDeviceProfile", saveArgs(t, p, p["version"].(string), nil))
	if m["ok"] != false || m["changed"] != true || m["error"] != "This profile was changed outside Commander since you opened it." {
		t.Errorf("answer: %v", m)
	}
	if c.apply+c.reload != 0 {
		t.Errorf("called %+v", *c)
	}
}

func TestSaveBadColor(t *testing.T) {
	s, dir, c := buttonDeps(t)
	p := profileCall(t, s)
	before, _ := os.ReadFile(filepath.Join(dir, "desk.toml"))
	m := call(t, s, "saveDeviceProfile", saveArgs(t, p, p["version"].(string), func(b []any) { b[1].(map[string]any)["color"] = "red" }))
	errs, _ := m["errors"].([]any)
	if m["ok"] != false || len(errs) != 1 {
		t.Fatalf("answer: %v", m)
	}
	e := errs[0].(map[string]any)
	if e["field"] != "color" || e["page"] != float64(1) || e["position"] != float64(1) || e["step"] != float64(-1) || e["message"] == "" {
		t.Errorf("error: %v", e)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "desk.toml"))
	if !bytes.Equal(before, after) || c.apply+c.reload != 0 {
		t.Error("a refused save changed something")
	}
}

func TestSaveBadStep(t *testing.T) {
	s, _, _ := buttonDeps(t)
	p := profileCall(t, s)
	m := call(t, s, "saveDeviceProfile", saveArgs(t, p, p["version"].(string), func(b []any) {
		b[0].(map[string]any)["steps"] = []any{
			map[string]any{"keys": []any{"A"}},
			map[string]any{"waitMs": 0},
		}
	}))
	errs, _ := m["errors"].([]any)
	if m["ok"] != false || len(errs) != 1 {
		t.Fatalf("answer: %v", m)
	}
	e := errs[0].(map[string]any)
	if e["field"] != "action" || e["step"] != float64(1) || e["page"] != float64(1) || e["position"] != float64(0) {
		t.Errorf("error: %v", e)
	}
}

func TestSaveWithoutCallbacks(t *testing.T) {
	dir := writeFolder(t)
	s := newService(t, (&fake{}).deps(dir))
	p := profileCall(t, s)
	m := call(t, s, "saveDeviceProfile", saveArgs(t, p, p["version"].(string), func(b []any) { b[0].(map[string]any)["title"] = "Web" }))
	if m["ok"] != true {
		t.Errorf("answer: %v", m)
	}
}

func TestRenderButton(t *testing.T) {
	s, dir, _ := buttonDeps(t)
	m := call(t, s, "renderButton", `{"button":{"page":1,"position":0,"title":"Gear","label":"","color":"#FF0000","symbol":"","picture":"","steps":[],"hold":false,"confirmMs":0,"state":""}}`)
	img := decodeDataURL(t, m["png"])
	if b := img.Bounds(); b.Dx() != picture.W || b.Dy() != picture.H || picture.W != 142 || picture.H != 148 {
		t.Errorf("size %v", b)
	}

	// A picture of the folder, and one that is missing.
	if err := os.MkdirAll(filepath.Join(dir, "pictures"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := pngFile(t, 8, 8)
	if err := os.WriteFile(filepath.Join(dir, "pictures", "x.png"), src, 0o600); err != nil {
		t.Fatal(err)
	}
	m = call(t, s, "renderButton", `{"button":{"page":1,"position":0,"title":"","label":"","color":"","symbol":"","picture":"pictures/x.png","steps":[],"hold":false,"confirmMs":0,"state":""}}`)
	if b := decodeDataURL(t, m["png"]).Bounds(); b.Dx() != 142 || b.Dy() != 148 {
		t.Errorf("picture size %v", b)
	}
	_, err := s.Handle(context.Background(), "renderButton", json.RawMessage(`{"button":{"page":1,"position":0,"title":"","label":"","color":"","symbol":"symbols/none.png","picture":"","steps":[],"hold":false,"confirmMs":0,"state":""}}`))
	if err == nil || !strings.Contains(err.Error(), "symbol") {
		t.Errorf("missing symbol: %v", err)
	}
	_, err = s.Handle(context.Background(), "renderButton", json.RawMessage(`{"button":{"page":1,"position":0,"title":"","label":"","color":"nope","symbol":"","picture":"","steps":[],"hold":false,"confirmMs":0,"state":""}}`))
	if err == nil || !strings.Contains(err.Error(), "color") {
		t.Errorf("bad color: %v", err)
	}
}

func decodeDataURL(t *testing.T, v any) image.Image {
	t.Helper()
	str, _ := v.(string)
	rest, ok := strings.CutPrefix(str, "data:image/png;base64,")
	if !ok {
		t.Fatalf("not a PNG data URL: %.40q", str)
	}
	raw, err := base64.StdEncoding.DecodeString(rest)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestImportPictureCall(t *testing.T) {
	s, dir, _ := buttonDeps(t)
	src := filepath.Join(t.TempDir(), "Landing.png")
	if err := os.WriteFile(src, pngFile(t, 4, 4), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"path": src, "kind": "picture"})
	m := call(t, s, "importPicture", string(args))
	if m["path"] != "pictures/landing.png" {
		t.Fatalf("answer: %v", m)
	}
	if m = call(t, s, "importPicture", string(args)); m["path"] != "pictures/landing.png" {
		t.Errorf("same bytes: %v", m)
	}
	if e, _ := os.ReadDir(filepath.Join(dir, "pictures")); len(e) != 1 {
		t.Errorf("pictures/ holds %d files", len(e))
	}
	txt := filepath.Join(t.TempDir(), "a.png")
	if err := os.WriteFile(txt, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	args, _ = json.Marshal(map[string]string{"path": txt, "kind": "picture"})
	if _, err := s.Handle(context.Background(), "importPicture", args); err == nil {
		t.Error("a text file was imported")
	}
}

func TestButtonRoundTrip(t *testing.T) {
	fb := layout.FileButton{
		Page: 2, Position: 5, Title: "T", Label: "L", Color: "#00AAFF80", Symbol: "symbols/s.png",
		Action: []string{"LCtrl+A", "wait 600ms", `run "C:\Program Files\x.exe" -a`, "open https://example.com"},
		State:  "caps", Confirm: "hold 700ms",
	}
	b, err := fromFile(fb)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Steps) != 4 || b.ConfirmMs != 700 || b.Hold {
		t.Fatalf("fromFile: %+v", b)
	}
	if !reflect.DeepEqual(b.Steps[0].Keys, []string{"LCtrl", "A"}) || *b.Steps[1].WaitMs != 600 ||
		*b.Steps[2].Run != `C:\Program Files\x.exe` || !reflect.DeepEqual(b.Steps[2].Args, []string{"-a"}) ||
		*b.Steps[3].Open != "https://example.com" {
		t.Errorf("steps: %+v", b.Steps)
	}
	back, err := toFile(b)
	if err != nil || !reflect.DeepEqual(back, fb) {
		t.Errorf("toFile: %+v, %v\nwant %+v", back, err, fb)
	}

	hold, err := fromFile(layout.FileButton{Page: 1, Action: []string{"LShift"}, Mode: "hold", Color: "#aabbcc"})
	if err != nil || !hold.Hold || hold.Color != "#AABBCC" {
		t.Errorf("hold: %+v, %v", hold, err)
	}
	if fb, err = toFile(hold); err != nil || fb.Mode != "hold" || fb.Confirm != "" {
		t.Errorf("hold back: %+v, %v", fb, err)
	}

	// The window gets lists, never null.
	empty, _ := fromFile(layout.FileButton{Page: 1, Action: nil, State: "x"})
	raw, _ := json.Marshal(empty)
	if !strings.Contains(string(raw), `"steps":[]`) {
		t.Errorf("json: %s", raw)
	}
	run := ""
	raw, _ = json.Marshal(stepJSON{Run: &run})
	if string(raw) != `{"args":[],"run":""}` {
		t.Errorf("run step: %s", raw)
	}
}

func TestToFileBadStep(t *testing.T) {
	_, err := toFile(buttonJSON{Steps: []stepJSON{{Keys: []string{"Nope"}}}})
	fe, ok := err.(*layout.FieldError)
	if !ok || fe.Field != "action" || fe.Step != 0 {
		t.Errorf("error: %v", err)
	}
	if _, err = toFile(buttonJSON{Steps: []stepJSON{{}}}); err == nil {
		t.Error("an empty step was accepted")
	}
}

func pngFile(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const richProfile = `
profile = 0

[light]
fps = 30

[[light.layer]]
effect = "state"
name = "gear"
toggle = "N"
start = "on"
keys = ["N"]
color = "#30E070"

[[button]]
page = 1
position = 0
title = "Gear"
label = "GEAR"
color = "#30e070"
symbol = "symbols/star.png"
action = ["N", "wait 200ms"]
state = "gear"
confirm = "hold 700ms"

[[button]]
page = 1
position = 1
title = "Boost"
action = ["LShift"]
mode = "hold"

[[button]]
page = 1
position = 2
title = "Lights"
state = "gear"
`

func TestSaveUnchanged(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "symbols"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "symbols", "star.png"), pngFile(t, 8, 8), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rich.toml"), []byte(richProfile), 0o600); err != nil {
		t.Fatal(err)
	}
	c := &counts{}
	d := (&fake{}).deps(dir)
	d.Reload = func() { c.reload++ }
	d.Apply = func() { c.apply++ }
	s := newService(t, d)

	p := profileCall(t, s)
	if !reflect.DeepEqual(p["states"], []any{"gear"}) {
		t.Errorf("states: %v", p["states"])
	}
	m := call(t, s, "saveDeviceProfile", saveArgs(t, p, p["version"].(string), nil))
	if m["ok"] != true || m["change"] != "none" || c.apply+c.reload != 0 {
		t.Fatalf("answer: %v %+v", m, *c)
	}
	// Saved once, the same buttons read back the same.
	again := profileCall(t, s)
	if !reflect.DeepEqual(again["buttons"], p["buttons"]) || again["version"] != m["version"] {
		t.Errorf("buttons changed by a save:\n%v\n%v", p["buttons"], again["buttons"])
	}
}

func TestSaveFailureAfterWriteReloads(t *testing.T) {
	s, _, c := buttonDeps(t)
	p := profileCall(t, s)
	v := p["version"].(string)
	orig := saveButtons
	t.Cleanup(func() { saveButtons = orig })

	// Some files were written, then the save failed: the configuration is
	// stale, so it is reloaded, and the error is still answered.
	saveButtons = func(string, byte, string, []layout.FileButton) (store.Result, error) {
		return store.Result{Written: true}, errors.New("disk full")
	}
	m := call(t, s, "saveDeviceProfile", saveArgs(t, p, v, nil))
	if m["ok"] != false || m["error"] != "disk full" || c.reload != 1 {
		t.Fatalf("after a partial write: %v reload=%d", m, c.reload)
	}

	// A failure before any write, and refusals, do not reload.
	saveButtons = func(string, byte, string, []layout.FileButton) (store.Result, error) {
		return store.Result{}, errors.New("disk full")
	}
	call(t, s, "saveDeviceProfile", saveArgs(t, p, v, nil))
	saveButtons = func(string, byte, string, []layout.FileButton) (store.Result, error) {
		return store.Result{}, store.ErrChanged
	}
	call(t, s, "saveDeviceProfile", saveArgs(t, p, v, nil))
	if c.reload != 1 {
		t.Errorf("reload = %d, want it unchanged by failures that wrote nothing", c.reload)
	}
}
