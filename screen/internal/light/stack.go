// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"errors"
	"fmt"
	"image/color"
	"strings"
	"time"
)

// Frame rate bounds; 0 in the configuration means DefaultFPS.
const (
	DefaultFPS = 30
	MinFPS     = 10
	MaxFPS     = 30
)

// LayerConfig is one layer as the configuration gives it. Settings holds the
// raw values as the TOML decoder produces them: string, int64, float64, []any.
type LayerConfig struct {
	Effect   string
	Keys     []string // where the layer shows; empty means everywhere
	Settings map[string]any
}

// Config is a profile's lighting: layers bottom first.
type Config struct {
	FPS    int
	Layers []LayerConfig
}

type stackLayer struct {
	effect Effect
	mask   *[NumLEDs]bool // nil means every LED
}

// Stack is a built lighting configuration, ready to render.
type Stack struct {
	fps    int
	layers []stackLayer
	states []StateDef
}

// Build checks a configuration and makes its effects. Errors name the layer
// (counted from 1), the effect and the setting.
func Build(c Config) (*Stack, error) {
	s := &Stack{fps: c.FPS}
	if s.fps == 0 {
		s.fps = DefaultFPS
	}
	if s.fps < MinFPS || s.fps > MaxFPS {
		return nil, fmt.Errorf("fps: %d is not from %d to %d", c.FPS, MinFPS, MaxFPS)
	}
	if len(c.Layers) == 0 {
		return nil, fmt.Errorf("no layer given")
	}
	stateLayer := map[string]int{}
	for i, lc := range c.Layers {
		n := i + 1
		sl, err := buildLayer(lc)
		if err != nil {
			return nil, newLayerError(n, err)
		}
		if st, ok := sl.effect.(interface{ State() StateDef }); ok {
			d := st.State()
			if prev, dup := stateLayer[d.Name]; dup {
				return nil, &LayerError{Layer: n, Setting: "name",
					Err: fmt.Errorf("state %q is already declared by layer %d", d.Name, prev)}
			}
			stateLayer[d.Name] = n
			s.states = append(s.states, d)
		}
		s.layers = append(s.layers, sl)
	}
	return s, nil
}

// LayerError is a Build error tied to one layer (1-based, bottom first)
// and, when it concerns one, a setting. Error is the text Build has always
// returned, such as "layer 2: wave: colors: needs at least two colours".
type LayerError struct {
	Layer   int
	Setting string // "" when the error is not about one setting ("effect" and "keys" count as settings here)
	Err     error
}

func (e *LayerError) Error() string { return fmt.Sprintf("layer %d: %v", e.Layer, e.Err) }
func (e *LayerError) Unwrap() error { return e.Err }

// settingError marks an error from buildLayer as concerning one setting.
type settingError struct {
	setting string
	err     error
}

func (e *settingError) Error() string { return e.err.Error() }
func (e *settingError) Unwrap() error { return e.err }

func forSetting(setting string, err error) error {
	return &settingError{setting: setting, err: err}
}

func newLayerError(n int, err error) *LayerError {
	le := &LayerError{Layer: n, Err: err}
	var se *settingError
	if errors.As(err, &se) {
		le.Setting = se.setting
	}
	return le
}

// newSetting finds the setting a New error is about: the error's text reads
// "[effect: ]setting: ..." when it is about one.
func newSetting(spec Spec, err error) string {
	text := strings.TrimPrefix(err.Error(), spec.Name+": ")
	first, _, ok := strings.Cut(text, ": ")
	if !ok {
		return ""
	}
	for _, st := range spec.Settings {
		if st.Name == first {
			return first
		}
	}
	return ""
}

func buildLayer(lc LayerConfig) (stackLayer, error) {
	spec, ok := registry[strings.ToLower(lc.Effect)]
	if !ok {
		return stackLayer{}, forSetting("effect", fmt.Errorf("unknown effect %q", lc.Effect))
	}
	var sl stackLayer
	if len(lc.Keys) > 0 {
		leds, err := ResolveLEDs(lc.Keys)
		if err != nil {
			return stackLayer{}, forSetting("keys", fmt.Errorf("%s: keys: %w", spec.Name, err))
		}
		sl.mask = new([NumLEDs]bool)
		for _, l := range leds {
			sl.mask[l] = true
		}
	} else if spec.NeedsKeys {
		return stackLayer{}, forSetting("keys", fmt.Errorf("%s: keys: required but not given", spec.Name))
	}
	v, err := parseValues(spec, lc.Settings)
	if err != nil {
		return stackLayer{}, err
	}
	sl.effect, err = spec.New(v)
	if err != nil {
		return stackLayer{}, forSetting(newSetting(spec, err), fmt.Errorf("%s: %w", spec.Name, err))
	}
	if sl.effect == nil {
		return stackLayer{}, fmt.Errorf("%s: made no effect", spec.Name)
	}
	return sl, nil
}

// FPS is the frame rate to render at.
func (s *Stack) FPS() int { return s.fps }

// States returns the states the layers declare, in layer order.
func (s *Stack) States() []StateDef {
	return append([]StateDef(nil), s.states...)
}

// Render composes the layers bottom to top over black at time t: each layer
// goes over what is below with straight alpha, and a layer's keys limit where
// it shows. The result is opaque.
func (s *Stack) Render(t time.Duration, in *Input) [NumLEDs]color.NRGBA {
	var out [NumLEDs]color.NRGBA
	for i := range out {
		out[i].A = 255
	}
	for _, sl := range s.layers {
		var l Layer
		sl.effect.Render(&l, t, in)
		for i, c := range l {
			if c.A == 0 || (sl.mask != nil && !sl.mask[i]) {
				continue
			}
			o := &out[i]
			o.R = over(c.R, o.R, c.A)
			o.G = over(c.G, o.G, c.A)
			o.B = over(c.B, o.B, c.A)
		}
	}
	return out
}

// over mixes top over below with alpha a, rounded to the nearest value.
func over(top, below, a uint8) uint8 {
	x := int(top)*int(a) + int(below)*(255-int(a))
	return uint8((x + 127) / 255)
}
