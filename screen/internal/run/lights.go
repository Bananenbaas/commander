// SPDX-License-Identifier: GPL-2.0-or-later

package run

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/action"
	"github.com/Bananenbaas/commander/screen/internal/device"
	"github.com/Bananenbaas/commander/screen/internal/keyboard"
	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/light"
	"github.com/Bananenbaas/commander/screen/internal/proto"
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
// values, and marks in the trace a program that was running at the last poll
// and is not now.
func (l *loop) noteStarts(running map[string]bool) {
	for _, p := range l.switchers {
		n := p.SwitchWhenRunning
		if l.eng != nil && running[n] && !l.wasRunning[n] {
			l.eng.Start(p.Number)
		}
		if !running[n] && l.wasRunning[n] {
			l.wasRunning[n] = false // marked once, also when two profiles share it
			l.ended, l.endedAt = n, l.now()
			l.trace.Mark(fmt.Sprintf("program %s ended", n))
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

// rest sends the frame the keyboard is to keep once the stream stops, if a
// frame was streamed since the keyboard was opened or since the last such
// frame: the active profile's stored lighting, read first when it was not
// read since the keyboard was opened (see restFrame). It is needed because
// the keyboard keeps the last frame streamed, also after a profile switch
// has loaded the new profile's stored lighting.
func (l *loop) rest() error {
	if l.eng == nil || l.kb == nil || !l.streamed {
		return nil
	}
	if _, ok := l.stored[l.active]; !ok {
		if err := l.readLights(); err != nil { // the keyboard is gone
			return fmt.Errorf("reading the stored lighting of profile %d: %w", l.active, err)
		}
	}
	return l.sendRest()
}

// sendRest sends restFrame, if a frame was streamed since the keyboard was
// opened or since the last such frame.
func (l *loop) sendRest() error {
	if !l.streamed {
		return nil
	}
	l.streamed = false
	p := l.restFrame()
	if p == nil {
		return nil
	}
	_, err := l.stream(p)
	return err
}

// restFrame is the active profile's stored lighting (report 0x11, kept
// unscaled) scaled by the brightness. When it has not been read since the
// keyboard was opened, it is the rest frame of the profile last streamed
// (see light.Engine.Rest).
func (l *loop) restFrame() *proto.LightPayload {
	if p := l.stored[l.active]; p != nil {
		return l.eng.Scaled(p)
	}
	return l.eng.Rest(l.shown)
}

// readLights reads the active profile's report 0x11: its brightness becomes
// the engine's, and its stored lighting is kept for restFrame. A failed read
// leaves both as they were and is logged once; it is returned only when the
// keyboard is gone.
func (l *loop) readLights() error {
	stored, level, err := l.kb.ReadLights(l.active)
	switch {
	case errors.Is(err, device.ErrDisconnected) || errors.Is(err, device.ErrClosed):
		return err
	case err != nil:
		colours := "using the last lit profile's colours"
		if l.stored[l.active] != nil {
			colours = "the stored lighting read before"
		}
		l.logOnce(&l.brightMsg, fmt.Sprintf("reading the stored lighting of profile %d: %v; keeping the last brightness and %s",
			l.active, err, colours))
		return nil
	}
	l.brightMsg = ""
	l.eng.SetBrightness(level)
	l.stored[l.active] = stored
	return nil
}

// activate follows a change of the active profile: the engine renders it
// from the next frame on, its brightness and stored lighting are read (one
// selector read), and when it is not lit the stored lighting is sent (see
// rest). When the read fails, the last brightness stays and so does the
// stored lighting read earlier since the keyboard was opened; without one
// the rest frame of the profile last streamed is sent instead. When the
// keyboard is gone it is closed. It reports false when the keyboard failed
// and was closed. The mixer page is sent again from scratch (see resetMixer).
func (l *loop) activate(ctx context.Context) bool {
	l.resetMixer()
	if l.eng == nil {
		return true
	}
	l.eng.SetActive(l.active)
	if err := l.readLights(); err != nil {
		l.logf("reading the stored lighting of profile %d: %v; opening the keyboard again", l.active, err)
		l.drop(ctx)
		return false
	}
	if !l.eng.Lit(l.active) {
		if err := l.sendRest(); err != nil {
			l.logf("lighting the keyboard: %v; opening it again", err)
			l.drop(ctx)
			return false
		}
	}
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
