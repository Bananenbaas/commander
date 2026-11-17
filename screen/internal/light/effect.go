// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
)

// Layer holds one colour per LED with straight (not premultiplied) alpha.
// Alpha 0 is transparent: the layers below show through.
type Layer [NumLEDs]color.NRGBA

// Press is one key press on the KB7.
type Press struct {
	LED int           // LED index of the key
	At  time.Duration // time of the press since the start
	N   int           // number of presses before this one since the start
}

// Input is what an effect may react to besides time.
type Input struct {
	Presses []Press         // recent presses, oldest first
	States  map[string]bool // current value of each state of the profile
}

// Effect draws one layer. Render gets a transparent layer and must depend only
// on t (time since the start) and in, so that it can be tested without a
// keyboard.
type Effect interface {
	Render(l *Layer, t time.Duration, in *Input)
}

// StateDef declares a state. An effect that implements
// interface{ State() StateDef } declares one for its stack.
type StateDef struct {
	Name   string
	Toggle *action.Chord // nil when only a screen button flips it
	Start  bool          // value when the profile's program starts
	Hold   time.Duration // when > 0 the toggle key must be held this long; the flip comes on its key-up
}

// Spec describes an effect: its settings and how to make it.
type Spec struct {
	Name      string
	Help      string
	Settings  []Setting
	NeedsKeys bool // the layer must say which keys it covers
	// New makes the effect from validated values: every setting given or
	// with a default is present, with the Go type its Kind documents.
	New func(v Values) (Effect, error)
}

var registry = map[string]Spec{}

// reserved names are layer-level keys in the configuration, not settings.
var reserved = map[string]bool{"effect": true, "keys": true}

// Register adds an effect. It panics on mistakes in the Spec, such as a name
// that is already taken or a default that does not parse, since those are
// programming errors.
func Register(s Spec) {
	if s.Name == "" || s.Name != strings.ToLower(s.Name) {
		panic(fmt.Sprintf("light: effect name %q must be non-empty lower case", s.Name))
	}
	if _, dup := registry[s.Name]; dup {
		panic(fmt.Sprintf("light: effect %q registered twice", s.Name))
	}
	if s.New == nil {
		panic(fmt.Sprintf("light: effect %q has no New", s.Name))
	}
	seen := map[string]bool{}
	for _, st := range s.Settings {
		switch {
		case st.Name == "" || reserved[st.Name]:
			panic(fmt.Sprintf("light: effect %q: setting name %q is not allowed", s.Name, st.Name))
		case seen[st.Name]:
			panic(fmt.Sprintf("light: effect %q: setting %q declared twice", s.Name, st.Name))
		case st.Kind < KindColor || st.Kind > KindName:
			panic(fmt.Sprintf("light: effect %q: setting %q has no valid kind", s.Name, st.Name))
		case st.Kind == KindChoice && len(st.Choices) == 0:
			panic(fmt.Sprintf("light: effect %q: choice %q has no choices", s.Name, st.Name))
		case st.Required && st.Default != nil:
			panic(fmt.Sprintf("light: effect %q: setting %q is required and has a default", s.Name, st.Name))
		case (st.Kind == KindNumber || st.Kind == KindDuration) && st.Max < st.Min:
			// Also catches a minimum given without a maximum; 0/0 is unbounded.
			panic(fmt.Sprintf("light: effect %q: setting %q has maximum %v below minimum %v",
				s.Name, st.Name, st.Max, st.Min))
		}
		seen[st.Name] = true
		if st.Default != nil {
			if _, err := st.parse(st.Default); err != nil {
				panic(fmt.Sprintf("light: effect %q: default of %s", s.Name, err))
			}
		}
	}
	registry[s.Name] = s
}

// Specs returns every registered effect, sorted by name.
func Specs() []Spec {
	out := make([]Spec, 0, len(registry))
	for _, s := range registry {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
