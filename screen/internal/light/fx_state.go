// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"image/color"
	"math"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
)

func init() {
	Register(Spec{
		Name:      "state",
		Help:      "Lights its keys while a named on/off state is on.",
		NeedsKeys: true,
		Settings: []Setting{
			{Name: "name", Kind: KindName, Required: true, Help: "Name of the state."},
			{Name: "toggle", Kind: KindChord, Help: "Chord that flips the state; without it only a screen button does."},
			{Name: "start", Kind: KindChoice, Default: "off", Choices: []string{"on", "off"}, Help: "Value when the profile starts."},
			{Name: "color", Kind: KindColor, Required: true, Help: "Colour while on."},
			{Name: "blink", Kind: KindNumber, Default: 0.0, Min: 0, Max: 10, Help: "Blinks per second while on; 0 for steady."},
		},
		New: func(v Values) (Effect, error) {
			s := stateEffect{
				def:   StateDef{Name: v["name"].(string), Start: v["start"].(string) == "on"},
				color: v["color"].(color.NRGBA),
				blink: v["blink"].(float64),
			}
			if c, ok := v["toggle"].(action.Chord); ok {
				s.def.Toggle = &c
			}
			return s, nil
		},
	})
}

type stateEffect struct {
	def   StateDef
	color color.NRGBA
	blink float64
}

// State declares the state for the stack.
func (s stateEffect) State() StateDef { return s.def }

func (s stateEffect) Render(l *Layer, t time.Duration, in *Input) {
	if in == nil || !in.States[s.def.Name] {
		return
	}
	if s.blink > 0 {
		x := t.Seconds() * s.blink
		if x-math.Floor(x) >= 0.5 {
			return
		}
	}
	for i := range l {
		if _, ok := Pos(i); ok {
			l[i] = s.color
		}
	}
}
