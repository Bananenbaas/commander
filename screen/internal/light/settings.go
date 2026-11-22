// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"fmt"
	"image/color"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/action"
)

// Kind is the type of a setting; it tells a configuration editor what to draw.
type Kind int

// Setting kinds. The comment gives the configuration form and the Go type the
// value has in Values.
const (
	KindColor    Kind = iota + 1 // "#RRGGBB" → color.NRGBA (A 255)
	KindColors                   // ["#RRGGBB", …], not empty → []color.NRGBA
	KindNumber                   // integer or float → float64
	KindChoice                   // one of Choices, any case → string as in Choices
	KindDuration                 // Go duration such as "4s" or "600ms" → time.Duration
	KindChord                    // chord in the action syntax such as "LAlt+N" → action.Chord
	KindName                     // non-empty text → string
)

var kindNames = map[Kind]string{
	KindColor: "colour", KindColors: "colour list", KindNumber: "number",
	KindChoice: "choice", KindDuration: "duration", KindChord: "chord", KindName: "name",
}

func (k Kind) String() string {
	if n, ok := kindNames[k]; ok {
		return n
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}

// Setting declares one setting of an effect.
type Setting struct {
	Name string
	Kind Kind
	// Default is used when the setting is absent, written in configuration
	// form: a string for colours, choices, durations, chords and names, a
	// []string for colour lists, a float64 for numbers. Nil means no default:
	// the setting is then absent from Values unless Required.
	Default any
	// Min and Max bound a number, or a duration in seconds. Both zero means
	// unbounded.
	Min, Max float64
	Choices  []string // the allowed values of a KindChoice
	Required bool     // the configuration must give it; then Default is nil
	Help     string
}

// Values holds an effect's validated settings by name, with the Go types
// listed at the Kind constants.
type Values map[string]any

// parseValues checks raw configuration values against s and fills defaults.
// Errors start with the effect's name.
func parseValues(s Spec, raw map[string]any) (Values, error) {
	declared := map[string]bool{}
	for _, st := range s.Settings {
		declared[st.Name] = true
	}
	names := make([]string, 0, len(raw))
	for n := range raw {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !declared[n] {
			return nil, fmt.Errorf("%s: unknown setting %q", s.Name, n)
		}
	}
	v := Values{}
	for _, st := range s.Settings {
		r, given := raw[st.Name]
		switch {
		case given:
		case st.Required:
			return nil, fmt.Errorf("%s: %s: required but not given", s.Name, st.Name)
		case st.Default != nil:
			r = st.Default
		default:
			continue
		}
		val, err := st.parse(r)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.Name, err)
		}
		v[st.Name] = val
	}
	return v, nil
}

// parse turns one raw value into the Go type of the setting's kind. Errors
// start with the setting's name.
func (st Setting) parse(r any) (any, error) {
	v, err := st.convert(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", st.Name, err)
	}
	return v, nil
}

func (st Setting) convert(r any) (any, error) {
	switch st.Kind {
	case KindColor:
		return parseColor(r)
	case KindColors:
		return parseColors(r)
	case KindNumber:
		return st.parseNumber(r)
	case KindChoice:
		s, ok := r.(string)
		if ok {
			for _, c := range st.Choices {
				if strings.EqualFold(s, c) {
					return c, nil
				}
			}
		}
		return nil, fmt.Errorf("%s is not one of %s", show(r), strings.Join(st.Choices, ", "))
	case KindDuration:
		return st.parseDuration(r)
	case KindChord:
		s, ok := r.(string)
		if !ok {
			return nil, fmt.Errorf("%s is not a key chord such as \"LAlt+N\"", show(r))
		}
		step, err := action.ParseStep(s)
		if err != nil {
			return nil, fmt.Errorf("%s is not a key chord: %w", show(r), err)
		}
		c, ok := step.(action.Chord)
		if !ok {
			return nil, fmt.Errorf("%s is not a key chord such as \"LAlt+N\"", show(r))
		}
		return c, nil
	case KindName:
		s, ok := r.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("%s is not a non-empty name", show(r))
		}
		return strings.TrimSpace(s), nil
	}
	return nil, fmt.Errorf("unknown kind %v", st.Kind)
}

func parseColor(r any) (color.NRGBA, error) {
	s, ok := r.(string)
	if ok && len(s) == 7 && s[0] == '#' {
		if n, err := strconv.ParseUint(s[1:], 16, 32); err == nil {
			return color.NRGBA{uint8(n >> 16), uint8(n >> 8), uint8(n), 255}, nil
		}
	}
	return color.NRGBA{}, fmt.Errorf("%s is not a colour such as \"#FF8000\"", show(r))
}

func parseColors(r any) ([]color.NRGBA, error) {
	var items []any
	switch l := r.(type) {
	case []any:
		items = l
	case []string:
		for _, s := range l {
			items = append(items, s)
		}
	default:
		return nil, fmt.Errorf("%s is not a list of colours such as [\"#FF8000\", \"#0040FF\"]", show(r))
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("the list of colours is empty")
	}
	out := make([]color.NRGBA, len(items))
	for i, it := range items {
		c, err := parseColor(it)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i+1, err)
		}
		out[i] = c
	}
	return out, nil
}

func (st Setting) parseNumber(r any) (float64, error) {
	var f float64
	switch n := r.(type) {
	case int64:
		f = float64(n)
	case int:
		f = float64(n)
	case float64:
		f = n
	default:
		return 0, fmt.Errorf("%s is not a number", show(r))
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, fmt.Errorf("%v is not a finite number", f)
	}
	if st.Min != 0 || st.Max != 0 {
		if f < st.Min {
			return 0, fmt.Errorf("%v is below the minimum %v", f, st.Min)
		}
		if f > st.Max {
			return 0, fmt.Errorf("%v is above the maximum %v", f, st.Max)
		}
	}
	return f, nil
}

func (st Setting) parseDuration(r any) (time.Duration, error) {
	s, ok := r.(string)
	if !ok {
		return 0, fmt.Errorf("%s is not a duration such as \"4s\" or \"600ms\"", show(r))
	}
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%s is not a duration such as \"4s\" or \"600ms\"", show(r))
	}
	if d < 0 {
		return 0, fmt.Errorf("%s is negative", show(r))
	}
	if st.Min != 0 || st.Max != 0 {
		if sec := d.Seconds(); sec < st.Min {
			return 0, fmt.Errorf("%s is below the minimum %vs", show(r), st.Min)
		} else if sec > st.Max {
			return 0, fmt.Errorf("%s is above the maximum %vs", show(r), st.Max)
		}
	}
	return d, nil
}

// show formats a raw value for an error message: strings quoted, the rest as
// Go prints them.
func show(r any) string {
	if s, ok := r.(string); ok {
		return strconv.Quote(s)
	}
	return fmt.Sprint(r)
}
