// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"image/color"
	"time"
)

func init() {
	for _, s := range []struct {
		name, help string
		keys       bool
	}{
		{"static", "Every LED in one colour.", false},
		{"fixed", "The listed keys in one colour.", true},
	} {
		Register(Spec{
			Name:      s.name,
			Help:      s.help,
			NeedsKeys: s.keys,
			Settings:  []Setting{{Name: "color", Kind: KindColor, Required: true, Help: "The colour."}},
			New: func(v Values) (Effect, error) {
				return fillEffect{v["color"].(color.NRGBA)}, nil
			},
		})
	}
}

// fillEffect paints every mapped LED in one colour.
type fillEffect struct{ c color.NRGBA }

func (s fillEffect) Render(l *Layer, _ time.Duration, _ *Input) {
	for i := range l {
		if _, ok := Pos(i); ok {
			l[i] = s.c
		}
	}
}
