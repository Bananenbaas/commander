// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"fmt"
	"image/color"
	"math"
	"time"
)

func init() {
	Register(Spec{
		Name: "wave",
		Help: "A colour gradient that travels across the keyboard.",
		Settings: []Setting{
			{Name: "colors", Kind: KindColors, Required: true, Help: "At least two colours, blended in a cycle."},
			{Name: "speed", Kind: KindNumber, Default: 0.5, Min: 0.05, Max: 5, Help: "Cycles per second."},
			{Name: "direction", Kind: KindChoice, Default: "right", Choices: []string{"right", "left", "up", "down"}, Help: "Direction of travel."},
			{Name: "length", Kind: KindNumber, Default: 14.0, Min: 2, Max: 40, Help: "Key units per cycle."},
		},
		New: func(v Values) (Effect, error) {
			cols := v["colors"].([]color.NRGBA)
			if len(cols) < 2 {
				return nil, fmt.Errorf("wave: colors: needs at least two colours")
			}
			return wave{cols, v["speed"].(float64), v["direction"].(string), v["length"].(float64)}, nil
		},
	})
}

type wave struct {
	colors    []color.NRGBA
	speed     float64
	direction string
	length    float64
}

func (w wave) Render(l *Layer, t time.Duration, _ *Input) {
	ts := t.Seconds()
	for i := range l {
		p, ok := Pos(i)
		if !ok {
			continue
		}
		var proj float64
		switch w.direction {
		case "left":
			proj = -p.X
		case "up":
			proj = -p.Y
		case "down":
			proj = p.Y
		default:
			proj = p.X
		}
		l[i] = waveColor(w.colors, proj, w.length, w.speed, ts)
	}
}

// waveColor returns the colour at projected position proj (key units) at time
// t seconds: the cyclic colour list blended linearly in RGB at phase
// frac(proj/length - speed*t).
func waveColor(cols []color.NRGBA, proj, length, speed, t float64) color.NRGBA {
	ph := proj/length - speed*t
	ph -= math.Floor(ph)
	pos := ph * float64(len(cols))
	i := int(pos)
	if i >= len(cols) {
		i = len(cols) - 1
	}
	f := pos - float64(i)
	a, b := cols[i], cols[(i+1)%len(cols)]
	mix := func(x, y uint8) uint8 {
		return uint8(math.Round(float64(x) + (float64(y)-float64(x))*f))
	}
	return color.NRGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 255}
}
