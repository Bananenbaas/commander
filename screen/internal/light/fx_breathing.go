// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"image/color"
	"math"
	"time"
)

func init() {
	Register(Spec{
		Name: "breathing",
		Help: "All LEDs fade between a dim level and full brightness; the colour changes at each minimum.",
		Settings: []Setting{
			{Name: "colors", Kind: KindColors, Default: []string{"#FFFFFF"}, Help: "Colours used one per breath."},
			{Name: "period", Kind: KindDuration, Default: "4s", Min: 0.5, Max: 60, Help: "Length of one breath."},
			{Name: "low", Kind: KindNumber, Default: 0.1, Min: 0, Max: 1, Help: "Brightness at the minimum, 0 to 1."},
		},
		New: func(v Values) (Effect, error) {
			return breathing{
				colors: v["colors"].([]color.NRGBA),
				period: v["period"].(time.Duration),
				low:    v["low"].(float64),
			}, nil
		},
	})
}

type breathing struct {
	colors []color.NRGBA
	period time.Duration
	low    float64
}

func (b breathing) Render(l *Layer, t time.Duration, _ *Input) {
	if t < 0 {
		t = 0
	}
	n := int64(t / b.period)
	c := b.colors[n%int64(len(b.colors))]
	frac := float64(t%b.period) / float64(b.period)
	k := b.low + (1-b.low)*(1-math.Cos(2*math.Pi*frac))/2
	px := color.NRGBA{
		R: uint8(math.Round(float64(c.R) * k)),
		G: uint8(math.Round(float64(c.G) * k)),
		B: uint8(math.Round(float64(c.B) * k)),
		A: 255,
	}
	for i := range l {
		if _, ok := Pos(i); ok {
			l[i] = px
		}
	}
}
