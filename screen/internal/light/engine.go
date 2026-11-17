// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// Limits on the presses an Engine keeps for the effects.
const (
	MaxPressAge = 5 * time.Second
	MaxPresses  = 32
)

// modifiers are the keys a toggle chord may hold before its last key.
var modifiers = map[action.Key]bool{
	action.LCtrl: true, action.RCtrl: true, action.LShift: true, action.RShift: true,
	action.LAlt: true, action.RAlt: true, action.LWin: true, action.RWin: true,
}

// Engine keeps what the lighting of the KB7 depends on besides the
// configuration: the active profile, the states of each profile, recent key
// presses, the keys held and the brightness. It turns them into payloads.
//
// An Engine is not safe for concurrent use: run drives it from one goroutine.
type Engine struct {
	stacks     map[byte]*Stack
	start      time.Time
	active     byte
	states     map[byte]map[string]bool // per lit profile, every declared state
	down       map[action.Key]bool
	presses    []Press // oldest first
	count      int     // presses with an LED since the start
	brightness int
}

// NewEngine makes an engine for the lit profiles in stacks; a profile without
// a stack is not lit. Times passed to the engine count from start. Every state
// begins at its start value, and the brightness at 100 %.
func NewEngine(stacks map[byte]*Stack, start time.Time) *Engine {
	e := &Engine{
		stacks:     map[byte]*Stack{},
		start:      start,
		states:     map[byte]map[string]bool{},
		down:       map[action.Key]bool{},
		brightness: 100,
	}
	for p, s := range stacks {
		if s == nil {
			continue
		}
		e.stacks[p] = s
		e.states[p] = map[string]bool{}
		e.Start(p)
	}
	return e
}

// Lit reports whether profile p has lighting.
func (e *Engine) Lit(p byte) bool { return e.stacks[p] != nil }

// SetActive makes p the active profile. States keep their values.
func (e *Engine) SetActive(p byte) { e.active = p }

// Key takes a key event of the KB7. A down for a key that is already down is
// an auto-repeat and ignored. A fresh down adds a press when the key has an
// LED, and flips each state of the active profile whose toggle ends in k while
// exactly the toggle's other keys are the modifiers held.
func (e *Engine) Key(k action.Key, down bool, at time.Time) {
	if !down {
		delete(e.down, k)
		return
	}
	if e.down[k] {
		return
	}
	e.down[k] = true
	if led, ok := LEDOfKey(k); ok {
		e.presses = append(e.presses, Press{LED: led, At: at.Sub(e.start), N: e.count})
		e.count++
		e.prune(at)
	}
	held := map[action.Key]bool{}
	for d := range e.down {
		if d != k && modifiers[d] {
			held[d] = true
		}
	}
	e.flipToggles(k, held)
}

// Sent takes a chord that a screen button sent to the game: it flips the
// states of the active profile whose toggle is that chord. It adds no press.
func (e *Engine) Sent(c action.Chord) {
	if len(c.Keys) == 0 {
		return
	}
	n := len(c.Keys) - 1
	e.flipToggles(c.Keys[n], keySet(c.Keys[:n]))
}

// flipToggles flips each state of the active profile whose toggle has last
// key k and exactly the keys in mods before it.
func (e *Engine) flipToggles(k action.Key, mods map[action.Key]bool) {
	s := e.stacks[e.active]
	if s == nil {
		return
	}
	for _, d := range s.states {
		if d.Toggle == nil || len(d.Toggle.Keys) == 0 {
			continue
		}
		n := len(d.Toggle.Keys) - 1
		if d.Toggle.Keys[n] == k && sameSet(keySet(d.Toggle.Keys[:n]), mods) {
			e.states[e.active][d.Name] = !e.states[e.active][d.Name]
		}
	}
}

func keySet(keys []action.Key) map[action.Key]bool {
	m := make(map[action.Key]bool, len(keys))
	for _, k := range keys {
		m[k] = true
	}
	return m
}

func sameSet(a, b map[action.Key]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// Flip flips state name of profile p. It reports false when the profile
// declares no such state.
func (e *Engine) Flip(p byte, name string) bool {
	st := e.states[p]
	v, ok := st[name]
	if !ok {
		return false
	}
	st[name] = !v
	return true
}

// Start sets the states of profile p to their start values. It does nothing
// for a profile that is not lit.
func (e *Engine) Start(p byte) {
	s := e.stacks[p]
	if s == nil {
		return
	}
	for _, d := range s.states {
		e.states[p][d.Name] = d.Start
	}
}

// SetBrightness sets the brightness in percent, clamped to 0–100.
func (e *Engine) SetBrightness(pct int) {
	e.brightness = min(max(pct, 0), 100)
}

// FPS is the frame rate of the active profile, DefaultFPS when it is not lit.
func (e *Engine) FPS() int {
	if s := e.stacks[e.active]; s != nil {
		return s.FPS()
	}
	return DefaultFPS
}

// Payload renders the active profile at time at. It is nil when the active
// profile is not lit.
func (e *Engine) Payload(at time.Time) *proto.LightPayload {
	s := e.stacks[e.active]
	if s == nil {
		return nil
	}
	e.prune(at)
	return e.payload(s, at.Sub(e.start), &Input{Presses: e.presses, States: e.states[e.active]})
}

// Rest renders profile p as it should stay when the stream stops: at t = 0,
// without presses and with every state off. It is nil when p is not lit.
func (e *Engine) Rest(p byte) *proto.LightPayload {
	s := e.stacks[p]
	if s == nil {
		return nil
	}
	return e.payload(s, 0, &Input{States: map[string]bool{}})
}

func (e *Engine) payload(s *Stack, t time.Duration, in *Input) *proto.LightPayload {
	out := new(proto.LightPayload)
	for i, c := range s.Render(t, in) {
		proto.SetLED(out, i, e.scale(c.R), e.scale(c.G), e.scale(c.B))
	}
	return out
}

func (e *Engine) scale(v uint8) byte { return byte(int(v) * e.brightness / 100) }

// prune drops presses older than MaxPressAge at time at and all but the
// newest MaxPresses.
func (e *Engine) prune(at time.Time) {
	now := at.Sub(e.start)
	i := 0
	for i < len(e.presses) && now-e.presses[i].At > MaxPressAge {
		i++
	}
	if n := len(e.presses) - MaxPresses; n > i {
		i = n
	}
	if i > 0 {
		e.presses = append(e.presses[:0], e.presses[i:]...)
	}
}
