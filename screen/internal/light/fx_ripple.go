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
		Name: "ripple",
		Help: "A ring that spreads from every key you press and fades.",
		Settings: []Setting{
			{Name: "color", Kind: KindColor, Help: "Colour of the rings; white when neither color nor colors is given."},
			{Name: "colors", Kind: KindColors, Help: "Colours of the rings in turn, by press number. Not together with color."},
			{Name: "speed", Kind: KindNumber, Default: 12.0, Min: 2, Max: 60, Help: "Keys per second the ring spreads."},
			{Name: "width", Kind: KindNumber, Default: 1.5, Min: 0.5, Max: 6, Help: "Width of the ring in keys."},
			{Name: "life", Kind: KindDuration, Default: "1.2s", Min: 0.2, Max: 5, Help: "How long a ring lasts."},
		},
		New: func(v Values) (Effect, error) {
			single, hasOne := v["color"].(color.NRGBA)
			list, hasList := v["colors"].([]color.NRGBA)
			if hasOne && hasList {
				return nil, fmt.Errorf("ripple: color and colors are both given; use one")
			}
			switch {
			case hasList:
			case hasOne:
				list = []color.NRGBA{single}
			default:
				list = []color.NRGBA{{255, 255, 255, 255}}
			}
			return ripple{list, v["speed"].(float64), v["width"].(float64), v["life"].(time.Duration)}, nil
		},
	})
}

type ripple struct {
	colors []color.NRGBA
	speed  float64
	width  float64
	life   time.Duration
}

func (r ripple) Render(l *Layer, t time.Duration, in *Input) {
	if in == nil {
		return
	}
	for _, p := range in.Presses {
		age := t - p.At
		if age < 0 || age >= r.life {
			continue
		}
		origin, ok := Pos(p.LED)
		if !ok {
			continue
		}
		col := r.colors[((p.N%len(r.colors))+len(r.colors))%len(r.colors)]
		radius := r.speed * age.Seconds()
		fade := 1 - age.Seconds()/r.life.Seconds()
		for i := range l {
			q, ok := Pos(i)
			if !ok {
				continue
			}
			d := math.Hypot(q.X-origin.X, q.Y-origin.Y)
			k := math.Max(0, 1-math.Abs(d-radius)/r.width) * fade
			a := uint8(math.Round(255 * k))
			if a > l[i].A {
				l[i] = color.NRGBA{col.R, col.G, col.B, a}
			}
		}
	}
}
