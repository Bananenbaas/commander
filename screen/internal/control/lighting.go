// SPDX-License-Identifier: GPL-2.0-or-later

package control

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/light"
	"github.com/Bananenbaas/commander/screen/internal/lightedit"
	"github.com/Bananenbaas/commander/screen/internal/store"
)

// lightChangedText is the answer when the lighting on disk is not the one
// the window opened.
const lightChangedText = "This profile's lighting was changed outside Commander since you opened it."

// kindNames are the setting kinds as the window names them.
var kindNames = map[light.Kind]string{
	light.KindColor: "color", light.KindColors: "colors", light.KindNumber: "number",
	light.KindChoice: "choice", light.KindDuration: "duration", light.KindChord: "chord", light.KindName: "name",
}

type settingJSON struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Default  any      `json:"default"`
	Min      float64  `json:"min"`
	Max      float64  `json:"max"`
	Choices  []string `json:"choices"`
	Required bool     `json:"required"`
	Help     string   `json:"help"`
}

type effectJSON struct {
	Name      string        `json:"name"`
	Help      string        `json:"help"`
	NeedsKeys bool          `json:"needsKeys"`
	Settings  []settingJSON `json:"settings"`
}

type ledJSON struct {
	Index int     `json:"index"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Name  string  `json:"name"`
}

type lightingInfoOut struct {
	Effects []effectJSON `json:"effects"`
	LEDs    []ledJSON    `json:"leds"`
	FrameMs int64        `json:"frameMs"`
	Frames  int          `json:"frames"`
}

func lightingInfo() lightingInfoOut {
	out := lightingInfoOut{Effects: []effectJSON{}, LEDs: []ledJSON{},
		FrameMs: light.FrameStep.Milliseconds(), Frames: light.FrameCount}
	for _, sp := range light.Specs() {
		e := effectJSON{Name: sp.Name, Help: sp.Help, NeedsKeys: sp.NeedsKeys, Settings: []settingJSON{}}
		for _, st := range sp.Settings {
			choices := append([]string{}, st.Choices...)
			e.Settings = append(e.Settings, settingJSON{
				Name: st.Name, Kind: kindNames[st.Kind], Default: st.Default, Min: st.Min, Max: st.Max,
				Choices: choices, Required: st.Required, Help: st.Help,
			})
		}
		out.Effects = append(out.Effects, e)
	}
	for _, l := range light.LEDs() {
		out.LEDs = append(out.LEDs, ledJSON{Index: l.Index, X: l.X, Y: l.Y, Name: l.Name})
	}
	return out
}

// layerJSON is one lighting layer as the window sees it. Settings are in
// configuration form, as the file holds them. LEDs is what Keys resolves to;
// the window sends back the LEDs it picked on the picture.
type layerJSON struct {
	Effect   string         `json:"effect"`
	Enabled  *bool          `json:"enabled"` // nil = on
	Keys     []string       `json:"keys"`
	LEDs     []int          `json:"leds"` // nil = not edited
	Settings map[string]any `json:"settings"`
}

// lightJSON is the [light] table as the window sees it.
type lightJSON struct {
	FPS    int64       `json:"fps"`
	Layers []layerJSON `json:"layers"`
}

// toLightJSON turns a [light] table into the window's form.
func toLightJSON(l *layout.FileLight) *lightJSON {
	if l == nil {
		return nil
	}
	out := &lightJSON{FPS: l.FPS, Layers: []layerJSON{}}
	for _, m := range l.Layers {
		on := true
		lj := layerJSON{Enabled: &on, Keys: []string{}, LEDs: []int{}, Settings: map[string]any{}}
		for k, v := range m {
			switch k {
			case "effect":
				lj.Effect, _ = v.(string)
			case "enabled":
				if b, ok := v.(bool); ok {
					on = b
				} else {
					lj.Settings[k] = v // not valid; the window shows it as it is
				}
			case "keys":
				if list, ok := v.([]any); ok {
					for _, x := range list {
						if s, ok := x.(string); ok {
							lj.Keys = append(lj.Keys, s)
						}
					}
				}
			default:
				lj.Settings[k] = v
			}
		}
		if leds, err := light.ResolveLEDs(lj.Keys); err == nil {
			lj.LEDs = leds
		}
		out.Layers = append(out.Layers, lj)
	}
	return out
}

// ledNames maps every LED to the name that picks it alone.
var ledNames = func() map[int]string {
	m := map[int]string{}
	for _, l := range light.LEDs() {
		m[l.Index] = l.Name
	}
	return m
}()

// lightErrorJSON is an error at a layer (1-based, as the file counts them;
// 0 for the lighting as a whole) and a setting ("" for the layer as a whole).
type lightErrorJSON struct {
	Layer   int    `json:"layer"`
	Setting string `json:"setting"`
	Message string `json:"message"`
}

// fromLightJSON turns the window's [light] into the file's. old is the
// table in the file: a number the window sends that equals an integer the
// same layer has there stays an integer. A layer whose LEDs differ from what
// its keys resolve to is written with one name per LED.
func fromLightJSON(lj *lightJSON, old *layout.FileLight) (*layout.FileLight, []lightErrorJSON) {
	if lj == nil {
		return nil, nil
	}
	out := &layout.FileLight{FPS: lj.FPS, Layers: []map[string]any{}}
	var bad []lightErrorJSON
	for i, layer := range lj.Layers {
		var before map[string]any
		if old != nil && i < len(old.Layers) && old.Layers[i]["effect"] == layer.Effect {
			before = old.Layers[i]
		}
		m := map[string]any{"effect": layer.Effect}
		if layer.Enabled != nil && !*layer.Enabled {
			m["enabled"] = false
		}
		keys, err := pickedKeys(layer)
		if err != nil {
			bad = append(bad, lightErrorJSON{Layer: i + 1, Setting: "keys", Message: err.Error()})
			continue
		}
		if len(keys) > 0 {
			m["keys"] = keys
		}
		for k, v := range layer.Settings {
			if v == nil || k == "effect" || k == "keys" {
				continue
			}
			if f, isFloat := v.(float64); isFloat {
				if n, wasInt := before[k].(int64); wasInt && float64(n) == f {
					v = n
				}
			}
			m[k] = v
		}
		out.Layers = append(out.Layers, m)
	}
	return out, bad
}

// pickedKeys returns the keys a layer is written with: its keys when the
// LEDs picked on the picture are what they resolve to, else one name per
// picked LED.
func pickedKeys(layer layerJSON) ([]string, error) {
	if layer.LEDs == nil {
		return layer.Keys, nil
	}
	picked := append([]int(nil), layer.LEDs...)
	sort.Ints(picked)
	picked = compactInts(picked)
	if now, err := light.ResolveLEDs(layer.Keys); err == nil && equalInts(now, picked) {
		return layer.Keys, nil
	}
	names := make([]string, 0, len(picked))
	for _, i := range picked {
		n, ok := ledNames[i]
		if !ok {
			return nil, fmt.Errorf("there is no LED %d", i)
		}
		names = append(names, n)
	}
	return names, nil
}

func compactInts(s []int) []int {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// lightDraft is the lighting part of a call: exactly one of simple and
// light, where a light of null is no lighting.
type lightDraft struct {
	Simple json.RawMessage `json:"simple"`
	Light  json.RawMessage `json:"light"`
}

// fileLight turns the draft into a [light] table; old is the file's, for
// fromLightJSON.
func (d lightDraft) fileLight(old *layout.FileLight) (*layout.FileLight, []lightErrorJSON, error) {
	if (d.Simple == nil) == (d.Light == nil) {
		return nil, nil, errors.New("give either simple or light")
	}
	if d.Simple != nil {
		var s lightedit.Simple
		if err := json.Unmarshal(d.Simple, &s); err != nil {
			return nil, nil, fmt.Errorf("bad arguments: %w", err)
		}
		return lightedit.ToFile(s), nil, nil
	}
	var lj *lightJSON
	if err := json.Unmarshal(d.Light, &lj); err != nil {
		return nil, nil, fmt.Errorf("bad arguments: %w", err)
	}
	fl, bad := fromLightJSON(lj, old)
	return fl, bad, nil
}

type deviceLightOut struct {
	Version string            `json:"version"`
	Light   *lightJSON        `json:"light"`
	Simple  *lightedit.Simple `json:"simple"`
	Reason  string            `json:"reason"`
	Stored  lightedit.Stored  `json:"stored"`
	States  []string          `json:"states"`
}

func (s *Service) deviceLight(args json.RawMessage) (any, error) {
	var a struct {
		Device  string `json:"device"`
		Profile int    `json:"profile"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("deviceLight: bad arguments: %w", err)
	}
	slot, err := kb7Slot(a.Device, a.Profile)
	if err != nil {
		return nil, fmt.Errorf("deviceLight: %w", err)
	}
	_, f, err := layoutOf(s.d.Dir, slot)
	if err != nil {
		return nil, fmt.Errorf("deviceLight: %w", err)
	}
	out := deviceLightOut{
		Version: store.LightVersion(f), Light: toLightJSON(f.Light),
		Stored: lightedit.StoredFromFile(f.StoredLight), States: []string{},
	}
	if simple, ok, reason := lightedit.FromFile(f.Light); ok {
		out.Simple = &simple
	} else {
		out.Reason = reason
	}
	if cfg, err := s.d.Load(s.d.Dir); err == nil {
		for _, p := range cfg.Profiles {
			if p.Number == slot && p.Light != nil {
				for _, d := range p.Light.States() {
					out.States = append(out.States, d.Name)
				}
			}
		}
	}
	return out, nil
}

func (s *Service) saveDeviceLight(args json.RawMessage) (any, error) {
	var a struct {
		lightDraft
		Device  string            `json:"device"`
		Profile int               `json:"profile"`
		Version string            `json:"version"`
		Stored  *lightedit.Stored `json:"stored"` // nil = keep [stored_light]
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("saveDeviceLight: bad arguments: %w", err)
	}
	slot, err := kb7Slot(a.Device, a.Profile)
	if err != nil {
		return nil, fmt.Errorf("saveDeviceLight: %w", err)
	}
	var old *layout.FileLight
	if _, f, err := layoutOf(s.d.Dir, slot); err == nil {
		old = f.Light
	}
	fl, bad, err := a.fileLight(old)
	if err != nil {
		return nil, fmt.Errorf("saveDeviceLight: %w", err)
	}
	if len(bad) > 0 {
		return map[string]any{"ok": false, "errors": bad}, nil
	}
	edit := store.Edit{Light: &fl}
	if a.Stored != nil {
		st := lightedit.StoredToFile(*a.Stored)
		edit.StoredLight = &st
	}

	res, err := saveProfile(s.d.Dir, slot, map[string]string{"light": a.Version}, edit)
	return s.saveAnswer("saveDeviceLight", lightChangedText, res, err, func(err error) map[string]any {
		var le *store.LightError
		var be *store.ButtonError
		switch {
		case errors.As(err, &le):
			return map[string]any{"ok": false, "errors": []lightErrorJSON{{le.Layer, le.Setting, le.Err.Error()}}}
		case errors.As(err, &be):
			return map[string]any{"ok": false, "error": fmt.Sprintf(
				"The button at page %d position %d does not work with this lighting: %v.", be.Page, be.Position, be.Err)}
		}
		return nil
	}), nil
}

// frameBytes is one preview frame: R, G, B per LED index, 0 for slots
// without an LED.
const frameBytes = light.NumLEDs * 3

func (s *Service) lightFrames(args json.RawMessage) (any, error) {
	var d lightDraft
	if err := json.Unmarshal(args, &d); err != nil {
		return nil, fmt.Errorf("lightFrames: bad arguments: %w", err)
	}
	fl, bad, err := d.fileLight(nil)
	if err != nil {
		return nil, fmt.Errorf("lightFrames: %w", err)
	}
	if len(bad) > 0 {
		return map[string]any{"errors": bad}, nil
	}
	stack, err := layout.BuildLight(fl)
	if err != nil {
		e := lightErrorJSON{Message: err.Error()}
		var le *light.LayerError
		if errors.As(err, &le) {
			e = lightErrorJSON{Layer: le.Layer, Setting: le.Setting, Message: le.Err.Error()}
		}
		return map[string]any{"errors": []lightErrorJSON{e}}, nil
	}
	// The simple view of the draft as the file would hold it.
	var simple *lightedit.Simple
	data, err := (&layout.File{Light: fl}).Encode()
	if err != nil {
		return nil, fmt.Errorf("lightFrames: %w", err)
	}
	f, err := layout.DecodeFile(data)
	if err != nil {
		return nil, fmt.Errorf("lightFrames: %w", err)
	}
	if sv, ok, _ := lightedit.FromFile(f.Light); ok {
		simple = &sv
	}
	frames := []string{}
	if stack != nil {
		for _, pl := range light.Frames(stack) {
			var buf bytes.Buffer
			buf.Grow(frameBytes)
			for i := 0; i < light.NumLEDs; i++ {
				if _, mapped := light.Pos(i); !mapped {
					buf.Write([]byte{0, 0, 0})
					continue
				}
				base := i/16*48 + i%16
				buf.Write([]byte{pl[base], pl[base+16], pl[base+32]})
			}
			frames = append(frames, base64.StdEncoding.EncodeToString(buf.Bytes()))
		}
	}
	return map[string]any{"frames": frames, "simple": simple}, nil
}

// saveProfile is store.SaveProfile; tests replace it.
var saveProfile = store.SaveProfile
