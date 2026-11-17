// SPDX-License-Identifier: GPL-2.0-or-later

package run

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/keyboard"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/light"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// quiet is how long after notifications are enabled no lighting frame is
// sent: the keyboard does not answer on the stream's interface for about 2 s.
const quiet = 2500 * time.Millisecond

// anyLit reports whether a profile has [light].
func anyLit(profiles []layout.Profile) bool {
	for _, p := range profiles {
		if p.Light != nil {
			return true
		}
	}
	return false
}

// flipsState returns the state a touch of b flips itself: its State, unless
// one of its chords is that state's toggle, which flips it when sent.
func flipsState(st *light.Stack, b layout.Button) string {
	if b.State == "" || st == nil {
		return ""
	}
	for _, d := range st.States() {
		if d.Name != b.State || d.Toggle == nil {
			continue
		}
		for _, s := range b.Action {
			if c, ok := s.(action.Chord); ok && sameChord(c, *d.Toggle) {
				return ""
			}
		}
	}
	return b.State
}

// sameChord reports whether a and b match the same key press: the same last
// key with the same other keys held.
func sameChord(a, b action.Chord) bool {
	if len(a.Keys) == 0 || len(b.Keys) == 0 || a.Keys[len(a.Keys)-1] != b.Keys[len(b.Keys)-1] {
		return false
	}
	set := func(ks []action.Key) map[action.Key]bool {
		m := map[action.Key]bool{}
		for _, k := range ks {
			m[k] = true
		}
		return m
	}
	x, y := set(a.Keys[:len(a.Keys)-1]), set(b.Keys[:len(b.Keys)-1])
	if len(x) != len(y) {
		return false
	}
	for k := range x {
		if !y[k] {
			return false
		}
	}
	return true
}

// keysStopped notes that the key channel closed. It is called once, as the
// channel is no longer read afterwards.
func (l *loop) keysStopped() {
	l.keys = nil
	l.logf("key input stopped; ripple and key toggles are off until run restarts")
}

// drainKeys passes the key events waiting on the channel to the engine,
// without waiting for more.
func (l *loop) drainKeys() {
	for l.keys != nil {
		select {
		case ev, ok := <-l.keys:
			if !ok {
				l.keysStopped()
				return
			}
			l.eng.Key(ev.Key, ev.Down, ev.At)
		default:
			return
		}
	}
}

// idle waits for d or until ctx is cancelled, passing key events to the
// engine meanwhile.
func (l *loop) idle(ctx context.Context, d time.Duration) {
	if l.keys == nil {
		sleep(ctx, d)
		return
	}
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			return
		case <-ctx.Done():
			return
		case ev, ok := <-l.keys: // never ready once l.keys is nil
			if !ok {
				l.keysStopped()
				continue
			}
			l.eng.Key(ev.Key, ev.Down, ev.At)
		}
	}
}

// noteStarts sets the states of the profiles whose program was not running
// at the last poll and is now (or is at the first poll) to their start
// values.
func (l *loop) noteStarts(running map[string]bool) {
	if l.eng == nil {
		return
	}
	for _, p := range l.switchers {
		if running[p.SwitchWhenRunning] && !l.wasRunning[p.SwitchWhenRunning] {
			l.eng.Start(p.Number)
		}
	}
	for _, p := range l.switchers {
		l.wasRunning[p.SwitchWhenRunning] = running[p.SwitchWhenRunning]
	}
}

// frameDue returns when the next frame is due; false when none is to be
// sent because there is no lighting, no keyboard or the active profile is
// not lit.
func (l *loop) frameDue() (time.Time, bool) {
	if l.eng == nil || l.kb == nil || !l.eng.Lit(l.active) {
		return time.Time{}, false
	}
	return l.nextFrame, true
}

// frame sends the active profile's lighting and schedules the next frame. A
// tick that is missed is skipped, not caught up on. After a missing
// acknowledgement the next frame waits as long as after enabling
// notifications, so that a keyboard that stopped answering does not hold up
// the loop every tick. Any other keyboard error, a stalled write included,
// closes the keyboard; the next poll opens it again.
func (l *loop) frame(ctx context.Context) {
	every := time.Second / time.Duration(l.eng.FPS())
	next := l.nextFrame.Add(every)
	if p := l.eng.Payload(l.now()); p != nil {
		missed, err := l.stream(p)
		if err != nil {
			l.logf("lighting the keyboard: %v; opening it again", err)
			l.drop(ctx)
			return
		}
		l.streamed, l.shown = true, l.active
		if missed {
			next = l.now().Add(quiet)
		}
	}
	if now := l.now(); !next.After(now) {
		next = now.Add(every)
	}
	l.nextFrame = next
}

// stream sends p. A missing acknowledgement is reported as missed, logged
// once per opening of the keyboard and otherwise ignored; any other error,
// keyboard.ErrStreamStalled included, is returned.
func (l *loop) stream(p *proto.LightPayload) (missed bool, err error) {
	err = l.kb.StreamLights(p)
	if err == nil || errors.Is(err, keyboard.ErrStreamStalled) || !errors.Is(err, device.ErrTimeout) {
		return false, err
	}
	if !l.ackLogged {
		l.ackLogged = true
		l.logf("lighting the keyboard: %v; waiting before the next frame", err)
	}
	return true, nil
}

// rest sends the rest frame of the profile last streamed, if a frame was
// streamed since the keyboard was opened or since the last rest frame.
func (l *loop) rest() error {
	if l.eng == nil || l.kb == nil || !l.streamed {
		return nil
	}
	l.streamed = false
	p := l.eng.Rest(l.shown)
	if p == nil {
		return nil
	}
	_, err := l.stream(p)
	return err
}

// activate follows a change of the active profile: the engine renders it
// from the next frame on, a rest frame is sent when it is not lit, and its
// brightness is read; when that read fails, the last level stays, unless
// the keyboard is gone: then it is closed. It reports false when the
// keyboard failed and was closed.
func (l *loop) activate(ctx context.Context) bool {
	if l.eng == nil {
		return true
	}
	l.eng.SetActive(l.active)
	if !l.eng.Lit(l.active) {
		if err := l.rest(); err != nil {
			l.logf("lighting the keyboard: %v; opening it again", err)
			l.drop(ctx)
			return false
		}
	}
	level, err := l.kb.ReadBrightness(l.active)
	switch {
	case errors.Is(err, device.ErrDisconnected) || errors.Is(err, device.ErrClosed):
		l.logf("reading the brightness: %v; opening the keyboard again", err)
		l.drop(ctx)
		return false
	case err != nil:
		l.logOnce(&l.brightMsg, fmt.Sprintf("reading the brightness: %v; keeping the last level", err))
		return true
	}
	l.brightMsg = ""
	l.eng.SetBrightness(level)
	return true
}

// sent tells the engine about a touch of the button at s that sends steps:
// every chord sent flips the states it toggles, and the button's own state
// flips unless one of those chords toggles it.
func (l *loop) sent(s spot, steps []action.Step) {
	if l.eng == nil {
		return
	}
	for _, st := range steps {
		if c, ok := st.(action.Chord); ok {
			l.eng.Sent(c)
		}
	}
	if name := l.flips[s]; name != "" {
		l.eng.Flip(s.profile, name)
	}
}
