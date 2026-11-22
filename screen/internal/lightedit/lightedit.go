// SPDX-License-Identifier: GPL-2.0-or-later

// Package lightedit translates between a layout's [light] and [stored_light]
// tables and the simple view of the window's Lighting tab: a base effect,
// an optional ripple, optional highlighted keys and any number of key states.
// A stack that does not fit that view comes with a one-line reason, so the
// window can open it in the advanced view instead.
package lightedit

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/light"
)

// Simple is the simple view of a lighting stack.
type Simple struct {
	Base      string     `json:"base"`      // "static", "wave", "breathing", "off", or "" for none
	Colors    []string   `json:"colors"`    // static: one; wave: two or more; breathing: one or more; off: none
	Speed     float64    `json:"speed"`     // wave: cycles per second
	Period    float64    `json:"period"`    // breathing: seconds per breath
	Direction string     `json:"direction"` // wave
	Ripple    *Ripple    `json:"ripple"`    // nil = no ripple
	Highlight *Highlight `json:"highlight"` // nil = no highlighted keys
	States    []State    `json:"states"`
}

// Ripple is a ripple when a key is pressed.
type Ripple struct {
	Colors []string `json:"colors"` // one colour is written as color
}

// Highlight is a set of keys in one colour.
type Highlight struct {
	Keys  []string `json:"keys"`
	Color string   `json:"color"`
}

// State is a key state: keys that light while a named on/off state is on.
type State struct {
	Name   string   `json:"name"`
	Toggle string   `json:"toggle"` // chord in the action syntax, "" = only a screen button flips it
	Keys   []string `json:"keys"`
	Color  string   `json:"color"`
}

// Stored is what the keyboard shows when Commander is not running.
type Stored struct {
	Mode       string `json:"mode"`       // "still" or "color"
	Color      string `json:"color"`      // mode color
	Brightness *int   `json:"brightness"` // nil = keep the keyboard's
}

// The slider for a breath's length goes from slow to fast over this range,
// in seconds.
const (
	minPeriod = 1.0
	maxPeriod = 10.0
)

const off = "#000000" // Off is a static layer in black

// Places in the simple view's pattern, bottom first.
const (
	placeBase = iota
	placeRipple
	placeHighlight
	placeState
)

var place = map[string]int{
	"static": placeBase, "wave": placeBase, "breathing": placeBase,
	"ripple": placeRipple, "fixed": placeHighlight, "state": placeState,
}

// placeName names a place in a sentence about the order of the layers.
var placeName = map[int]string{
	placeBase: "the base effect", placeRipple: "the ripple",
	placeHighlight: "the highlighted keys", placeState: "a key state",
}

// twice names two layers in one place.
var twice = map[int]string{
	placeBase: "two base effects", placeRipple: "two ripples",
	placeHighlight: "two sets of highlighted keys",
}

// noun names an effect in a sentence.
var noun = map[string]string{
	"static": "one colour", "wave": "wave", "breathing": "breathing",
	"ripple": "ripple", "fixed": "highlighted keys", "state": "key state",
}

// controls lists, per effect, the settings the simple view has a control for.
var controls = map[string]map[string]bool{
	"static":    {"color": true},
	"wave":      {"colors": true, "speed": true, "direction": true},
	"breathing": {"colors": true, "period": true},
	"ripple":    {"color": true, "colors": true},
	"fixed":     {"color": true},
	"state":     {"name": true, "toggle": true, "color": true},
}

// words are plain words for settings; "effect.setting" wins over "setting".
var words = map[string]string{
	"color": "colour", "colors": "colours", "period": "speed",
	"low": "lowest brightness", "mode": "colour mode", "width": "ring width",
	"life": "ring lifetime", "ripple.speed": "spreading speed",
	"hold": "hold time", "start": "starting state", "blink": "blinking",
	"toggle": "key",
}

// specs holds the lighting engine's effects by name.
var specs = func() map[string]light.Spec {
	m := map[string]light.Spec{}
	for _, s := range light.Specs() {
		m[s.Name] = s
	}
	return m
}()

// FromFile returns the simple view of l, or ok false and a one-line reason
// in gamer words (e.g. "This lighting has two base effects, so it opens in
// Advanced.", "The wave's length is changed, which the simple view has no
// control for."). A nil l is a Simple with Base "" and ok true. Colours come
// out upper case; settings left out of the file come out at their default.
func FromFile(l *layout.FileLight) (s Simple, ok bool, reason string) {
	s = Simple{Colors: []string{}, States: []State{}}
	if l == nil {
		return s, true, ""
	}
	if l.FPS != 0 && l.FPS != light.DefaultFPS {
		return Simple{}, false, "The frame rate is changed, which the simple view has no control for."
	}
	last := -1
	for _, m := range l.Layers {
		if reason := readLayer(&s, m, &last); reason != "" {
			return Simple{}, false, reason
		}
	}
	return s, true, ""
}

// readLayer adds one layer to s; last is the place of the layer below. It
// returns why the layer does not fit, or "".
func readLayer(s *Simple, m map[string]any, last *int) string {
	if v, given := m["enabled"]; given {
		on, isBool := v.(bool)
		if !isBool {
			return "One of the layers has an on/off setting that is not true or false, so it opens in Advanced."
		}
		if !on {
			return "One of the layers is switched off, so it opens in Advanced."
		}
	}
	e, _ := m["effect"].(string)
	e = strings.ToLower(strings.TrimSpace(e))
	if e == "" {
		return "One of the layers has no effect, so it opens in Advanced."
	}
	p, known := place[e]
	if _, registered := specs[e]; !known || !registered {
		return "This lighting uses the " + e + " effect, which the simple view has no control for."
	}
	switch {
	case p < *last:
		return capital(placeName[*last]) + " sits below " + placeName[p] + ", so it opens in Advanced."
	case p == *last && p != placeState:
		return "This lighting has " + twice[p] + ", so it opens in Advanced."
	}
	*last = p
	if reason := checkOthers(e, m); reason != "" {
		return reason
	}
	keys, keysOK := stringList(m["keys"])
	if !keysOK {
		return invalid(e, "keys")
	}
	if len(keys) > 0 && (p == placeBase || p == placeRipple) {
		return "The " + noun[e] + " covers only some keys, which the simple view has no control for."
	}
	switch e {
	case "static":
		c, ok := colorOf(m["color"])
		if !ok {
			return invalid(e, "color")
		}
		if c == off {
			s.Base = "off"
		} else {
			s.Base, s.Colors = e, []string{c}
		}
	case "wave":
		cs, ok := colorsOf(m["colors"])
		if !ok || len(cs) < 2 {
			return invalid(e, "colors")
		}
		speed, ok := numberOf(setting(m, e, "speed"))
		if !ok {
			return invalid(e, "speed")
		}
		dir, ok := choiceOf(e, "direction", setting(m, e, "direction"))
		if !ok {
			return invalid(e, "direction")
		}
		s.Base, s.Colors, s.Speed, s.Direction = e, cs, speed, dir
	case "breathing":
		cs, ok := colorsOf(setting(m, e, "colors"))
		if !ok {
			return invalid(e, "colors")
		}
		d, ok := durationOf(setting(m, e, "period"))
		if !ok {
			return invalid(e, "period")
		}
		sec := d.Seconds()
		if sec < minPeriod || sec > maxPeriod {
			return "The breathing's speed is outside what the simple view's slider covers, so it opens in Advanced."
		}
		s.Base, s.Colors, s.Period = e, cs, sec
	case "ripple":
		one, hasOne := m["color"]
		list, hasList := m["colors"]
		r := &Ripple{Colors: []string{"#FFFFFF"}}
		switch {
		case hasOne && hasList:
			return "The ripple has both one colour and a list of colours, so it opens in Advanced."
		case hasOne:
			c, ok := colorOf(one)
			if !ok {
				return invalid(e, "color")
			}
			r.Colors = []string{c}
		case hasList:
			cs, ok := colorsOf(list)
			if !ok {
				return invalid(e, "colors")
			}
			r.Colors = cs
		}
		s.Ripple = r
	case "fixed":
		c, ok := colorOf(m["color"])
		if !ok {
			return invalid(e, "color")
		}
		if len(keys) == 0 {
			return invalid(e, "keys")
		}
		s.Highlight = &Highlight{Keys: keys, Color: c}
	case "state":
		name, _ := m["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			return invalid(e, "name")
		}
		toggle := ""
		if v, given := m["toggle"]; given {
			t, isString := v.(string)
			if !isString {
				return invalid(e, "toggle")
			}
			toggle = strings.TrimSpace(t)
		}
		c, ok := colorOf(m["color"])
		if !ok {
			return invalid(e, "color")
		}
		if len(keys) == 0 {
			return invalid(e, "keys")
		}
		s.States = append(s.States, State{Name: name, Toggle: toggle, Keys: keys, Color: c})
	}
	return ""
}

// checkOthers makes sure every setting of the layer that the simple view has
// no control for is absent or at its default.
func checkOthers(e string, m map[string]any) string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if k == "effect" || k == "keys" || k == "enabled" || controls[e][k] {
			continue
		}
		st, found := settingOf(e, k)
		if !found {
			return "The " + noun[e] + " has a setting the simple view does not know (" + k + "), so it opens in Advanced."
		}
		if st.Default != nil && equalsDefault(st, m[k]) {
			continue
		}
		verb := "changed"
		if st.Default == nil {
			verb = "set"
		}
		return "The " + possessive(e) + " " + word(e, k) + " is " + verb + ", which the simple view has no control for."
	}
	return ""
}

// ToFile builds the [light] table for s, bottom first in the pattern order;
// settings at their default are left out, and so are a zero Speed or Period
// and an empty Direction. Colours are written upper case. A Simple with
// nothing in it returns nil (no [light]).
func ToFile(s Simple) *layout.FileLight {
	var layers []map[string]any
	switch s.Base {
	case "off":
		layers = append(layers, map[string]any{"effect": "static", "color": off})
	case "static":
		m := map[string]any{"effect": "static"}
		if len(s.Colors) > 0 {
			m["color"] = upper(s.Colors[0])
		}
		layers = append(layers, m)
	case "wave":
		m := map[string]any{"effect": "wave", "colors": uppers(s.Colors)}
		if s.Speed != 0 {
			m["speed"] = s.Speed
		}
		if s.Direction != "" {
			m["direction"] = strings.ToLower(s.Direction)
		}
		layers = append(layers, dropDefaults(m))
	case "breathing":
		m := map[string]any{"effect": "breathing"}
		if len(s.Colors) > 0 {
			m["colors"] = uppers(s.Colors)
		}
		if s.Period != 0 {
			m["period"] = strconv.FormatFloat(s.Period, 'f', -1, 64) + "s"
		}
		layers = append(layers, dropDefaults(m))
	}
	if s.Ripple != nil {
		m := map[string]any{"effect": "ripple"}
		switch len(s.Ripple.Colors) {
		case 0:
		case 1:
			m["color"] = upper(s.Ripple.Colors[0])
		default:
			m["colors"] = uppers(s.Ripple.Colors)
		}
		layers = append(layers, m)
	}
	if s.Highlight != nil {
		layers = append(layers, map[string]any{
			"effect": "fixed", "keys": append([]string(nil), s.Highlight.Keys...), "color": upper(s.Highlight.Color),
		})
	}
	for _, st := range s.States {
		m := map[string]any{"effect": "state", "name": st.Name}
		if st.Toggle != "" {
			m["toggle"] = st.Toggle
		}
		if len(st.Keys) > 0 {
			m["keys"] = append([]string(nil), st.Keys...)
		}
		m["color"] = upper(st.Color)
		layers = append(layers, m)
	}
	if len(layers) == 0 {
		return nil
	}
	return &layout.FileLight{Layers: layers}
}

// dropDefaults removes the settings of a layer that equal their default.
func dropDefaults(m map[string]any) map[string]any {
	e := m["effect"].(string)
	for k, v := range m {
		if st, found := settingOf(e, k); found && st.Default != nil && equalsDefault(st, v) {
			delete(m, k)
		}
	}
	return m
}

// StoredFromFile returns the view of a [stored_light] table; nil is a still
// of the lighting at the keyboard's brightness.
func StoredFromFile(st *layout.FileStored) Stored {
	s := Stored{Mode: "still"}
	if st == nil {
		return s
	}
	if st.Color != "" {
		s.Mode, s.Color = "color", upper(st.Color)
	}
	if st.Brightness != nil {
		b := int(*st.Brightness)
		s.Brightness = &b
	}
	return s
}

// StoredToFile builds the [stored_light] table for s: nil when the mode is
// still and no brightness is given.
func StoredToFile(s Stored) *layout.FileStored {
	st := &layout.FileStored{}
	if s.Mode == "color" {
		st.Color = upper(s.Color)
	}
	if s.Brightness != nil {
		b := int64(*s.Brightness)
		st.Brightness = &b
	}
	if st.Color == "" && st.Brightness == nil {
		return nil
	}
	return st
}

// settingOf finds a setting of an effect.
func settingOf(e, name string) (light.Setting, bool) {
	for _, st := range specs[e].Settings {
		if st.Name == name {
			return st, true
		}
	}
	return light.Setting{}, false
}

// setting returns the layer's value of a setting, or its default when the
// layer leaves it out.
func setting(m map[string]any, e, name string) any {
	if v, given := m[name]; given {
		return v
	}
	st, _ := settingOf(e, name)
	return st.Default
}

// equalsDefault reports whether v is the setting's default in configuration
// form: numbers by value, durations by length, the rest regardless of case.
func equalsDefault(st light.Setting, v any) bool {
	switch st.Kind {
	case light.KindNumber:
		a, ok1 := numberOf(v)
		b, ok2 := numberOf(st.Default)
		return ok1 && ok2 && a == b
	case light.KindDuration:
		a, ok1 := durationOf(v)
		b, ok2 := durationOf(st.Default)
		return ok1 && ok2 && a == b
	case light.KindColors:
		a, ok1 := colorsOf(v)
		b, ok2 := colorsOf(st.Default)
		if !ok1 || !ok2 || len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	a, ok1 := v.(string)
	b, ok2 := st.Default.(string)
	return ok1 && ok2 && strings.EqualFold(strings.TrimSpace(a), b)
}

func numberOf(v any) (float64, bool) {
	var f float64
	switch n := v.(type) {
	case int64:
		f = float64(n)
	case int:
		f = float64(n)
	case float64:
		f = n
	default:
		return 0, false
	}
	return f, !math.IsNaN(f) && !math.IsInf(f, 0)
}

func durationOf(v any) (time.Duration, bool) {
	s, ok := v.(string)
	if !ok {
		return 0, false
	}
	d, err := time.ParseDuration(strings.TrimSpace(s))
	return d, err == nil && d >= 0
}

// choiceOf returns a choice as the effect's Spec spells it.
func choiceOf(e, name string, v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	st, _ := settingOf(e, name)
	for _, c := range st.Choices {
		if strings.EqualFold(strings.TrimSpace(s), c) {
			return c, true
		}
	}
	return "", false
}

// colorOf returns a "#RRGGBB" colour upper case.
func colorOf(v any) (string, bool) {
	s, ok := v.(string)
	if !ok || len(s) != 7 || s[0] != '#' {
		return "", false
	}
	if _, err := strconv.ParseUint(s[1:], 16, 32); err != nil {
		return "", false
	}
	return strings.ToUpper(s), true
}

// colorsOf returns a non-empty list of colours upper case.
func colorsOf(v any) ([]string, bool) {
	items, ok := stringList(v)
	if !ok || len(items) == 0 {
		return nil, false
	}
	out := make([]string, len(items))
	for i, it := range items {
		c, ok := colorOf(it)
		if !ok {
			return nil, false
		}
		out[i] = c
	}
	return out, true
}

// stringList reads a list of strings as decoded from TOML ([]any) or built
// in Go ([]string); nil is an empty list.
func stringList(v any) ([]string, bool) {
	switch l := v.(type) {
	case nil:
		return nil, true
	case []string:
		return append([]string(nil), l...), true
	case []any:
		out := make([]string, len(l))
		for i, x := range l {
			s, ok := x.(string)
			if !ok {
				return nil, false
			}
			out[i] = s
		}
		return out, true
	}
	return nil, false
}

func upper(c string) string { return strings.ToUpper(strings.TrimSpace(c)) }

func uppers(cs []string) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = upper(c)
	}
	return out
}

func invalid(e, name string) string {
	w, verb := word(e, name), "is"
	if strings.HasSuffix(w, "s") {
		verb = "are"
	}
	return "The " + possessive(e) + " " + w + " " + verb + " not valid, so it opens in Advanced."
}

func word(e, name string) string {
	if w, ok := words[e+"."+name]; ok {
		return w
	}
	if w, ok := words[name]; ok {
		return w
	}
	return name
}

func possessive(e string) string {
	n := noun[e]
	if strings.HasSuffix(n, "s") {
		return n + "'"
	}
	return n + "'s"
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
