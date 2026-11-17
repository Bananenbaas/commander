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

// maxRepeatGap is the longest gap between two down events of a key that still
// makes the second an auto-repeat. Windows repeats about every 30 ms after an
// initial delay of up to 1 s. A down after a longer gap is a new press: the
// key-up in between was lost, as happens to keys pressed during Alt+Tab.
const maxRepeatGap = 1100 * time.Millisecond

// modifiers are the keys a toggle chord may hold before its last key.
var modifiers = map[action.Key]bool{
	action.LCtrl: true, action.RCtrl: true, action.LShift: true, action.RShift: true,
	action.LAlt: true, action.RAlt: true, action.LWin: true, action.RWin: true,
}

// toggle is a state's toggle chord taken apart for matching.
type toggle struct {
	name    string
	trigger action.Key          // last key of the chord
	mods    map[action.Key]bool // the keys before it
	hold    time.Duration       // 0 flips on key-down, else on a key-up after this long
}

// pendingHold is a press of a hold toggle's trigger that may still flip it.
type pendingHold struct {
	profile byte
	name    string
	trigger action.Key
	downAt  time.Time
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
	toggles    map[byte][]toggle        // per lit profile, the states with a toggle
	down       map[action.Key]time.Time // keys held, with the time of their last down event
	pending    []pendingHold            // hold toggles whose key is down
	presses    []Press                  // oldest first
	count      int                      // presses with an LED since the start
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
		toggles:    map[byte][]toggle{},
		down:       map[action.Key]time.Time{},
		brightness: 100,
	}
	for p, s := range stacks {
		if s == nil {
			continue
		}
		e.stacks[p] = s
		e.states[p] = map[string]bool{}
		e.Start(p)
		for _, d := range s.states {
			if d.Toggle == nil || len(d.Toggle.Keys) == 0 {
				continue
			}
			n := len(d.Toggle.Keys) - 1
			e.toggles[p] = append(e.toggles[p], toggle{d.Name, d.Toggle.Keys[n], keySet(d.Toggle.Keys[:n]), d.Hold})
		}
	}
	return e
}

// Lit reports whether profile p has lighting.
func (e *Engine) Lit(p byte) bool { return e.stacks[p] != nil }

// SetActive makes p the active profile. States keep their values.
func (e *Engine) SetActive(p byte) { e.active = p }

// Key takes a key event of the KB7. A down for a key that is already down,
// within maxRepeatGap of its previous down event, is an auto-repeat and
// ignored. A fresh down adds a press when the key has an LED, and flips each
// state of the active profile whose toggle ends in k while exactly the
// toggle's other keys are the modifiers held.
func (e *Engine) Key(k action.Key, down bool, at time.Time) {
	if !down {
		delete(e.down, k)
		e.release(k, at)
		return
	}
	last, held := e.down[k]
	e.down[k] = at
	if held && at.Sub(last) <= maxRepeatGap {
		return
	}
	if led, ok := LEDOfKey(k); ok {
		e.presses = append(e.presses, Press{LED: led, At: at.Sub(e.start), N: e.count})
		e.count++
		e.prune(at)
	}
	mods := map[action.Key]bool{}
	for d := range e.down {
		if d != k && modifiers[d] {
			mods[d] = true
		}
	}
	e.flipToggles(k, mods, false)
	e.holdToggles(k, mods, at)
}

// holdToggles records a fresh down of k for each hold toggle of the active
// profile it triggers. A pending press of the same toggle, whose key-up was
// lost, is replaced.
func (e *Engine) holdToggles(k action.Key, mods map[action.Key]bool, at time.Time) {
	for _, tg := range e.toggles[e.active] {
		if tg.hold <= 0 || tg.trigger != k || !sameSet(tg.mods, mods) {
			continue
		}
		e.dropPending(e.active, tg.name)
		e.pending = append(e.pending, pendingHold{e.active, tg.name, k, at})
	}
}

func (e *Engine) dropPending(p byte, name string) {
	for i, h := range e.pending {
		if h.profile == p && h.name == name {
			e.pending = append(e.pending[:i], e.pending[i+1:]...)
			return
		}
	}
}

// release flips the hold toggles whose trigger k went up after being held
// long enough, and forgets the presses of k.
func (e *Engine) release(k action.Key, at time.Time) {
	kept := e.pending[:0]
	for _, h := range e.pending {
		if h.trigger != k {
			kept = append(kept, h)
			continue
		}
		for _, tg := range e.toggles[h.profile] {
			if tg.name == h.name && at.Sub(h.downAt) >= tg.hold {
				e.states[h.profile][h.name] = !e.states[h.profile][h.name]
			}
		}
	}
	e.pending = kept
}

// Sent takes a chord that a screen button sent to the game: it flips the
// states of the active profile whose toggle is that chord. It adds no press.
func (e *Engine) Sent(c action.Chord) {
	if len(c.Keys) == 0 {
		return
	}
	n := len(c.Keys) - 1
	e.flipToggles(c.Keys[n], keySet(c.Keys[:n]), true)
}

// flipToggles flips each state of the active profile whose toggle has last
// key k and exactly the keys in mods before it. Hold toggles flip only when
// tap is true (a screen button sends a tap).
func (e *Engine) flipToggles(k action.Key, mods map[action.Key]bool, tap bool) {
	st := e.states[e.active]
	for _, tg := range e.toggles[e.active] {
		if (tap || tg.hold <= 0) && tg.trigger == k && sameSet(tg.mods, mods) {
			st[tg.name] = !st[tg.name]
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

// Scaled returns a copy of p with every colour scaled by the brightness, as
// the payloads the engine renders are.
func (e *Engine) Scaled(p *proto.LightPayload) *proto.LightPayload {
	out := new(proto.LightPayload)
	for i, v := range p {
		out[i] = e.scale(v)
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
