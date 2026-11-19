// SPDX-License-Identifier: GPL-2.0-or-later

package run

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/audio"
	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/keyboard"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// Session is the system volume or one program's volume.
type Session = audio.Session

// Audio is the computer's sound, shown on the keyboard's volume mixer page.
type Audio interface {
	// Sessions returns the system volume and the programs with a sound
	// session; Active tells those that play sound.
	Sessions() (system Session, programs []Session, err error)
	// SetSystemLevel sets the system volume, 0 to 100.
	SetSystemLevel(level int) error
	// SetProgramLevel sets the volume of the program with the given ID.
	SetProgramLevel(id string, level int) error
	// Changes signals that the sessions are to be read again. It is never
	// closed.
	Changes() <-chan struct{}
}

// mixerKeyboard is what the keyboard needs for the mixer page; the keyboard
// package's *KB provides it.
type mixerKeyboard interface {
	WriteMixerList(b []byte) error
	SendFrame(f [64]byte) error
}

// maxPrograms is the most programs the mixer page shows: the level frames
// carry four.
const maxPrograms = proto.MaxMixerRows - 1

// mixBackoff is how long the mixer waits after a write the keyboard refused
// before it writes again.
const mixBackoff = 5 * time.Second

// mixSettle is how long the level frames wait after a list (report 0x28): the
// keyboard ignores the frames sent right after it. Swarm II waits about 580 ms
// (capture 45-mixer-new-app-1: the 0x28 write at 6.038 s, the first a3 03 0e
// frame at 6.615 s).
const mixSettle = 600 * time.Millisecond

// Kinds of mixer errors, each logged once until that kind works again.
const (
	mixRead = iota
	mixSet
	mixWrite
	mixKinds
)

// mixer is the state of the volume mixer page, used by Run's goroutine only.
type mixer struct {
	audio   Audio           // nil: no mixer
	changes <-chan struct{} // nil when there is none, or once it is closed
	dirty   bool            // the sessions are to be read
	after   time.Time       // nothing is sent before
	first   map[string]int  // when each program was first seen in this run
	reads   int             // Sessions calls that succeeded
	shown   []string        // the IDs of the programs on the page, row 1 first
	list    []string        // the names of the list written; nil: none since the keyboard was opened or switched
	sys     *[64]byte       // the system frame sent last; nil: none
	muted   bool            // the system mute sent last
	levels  []int           // the program levels sent last; nil: none
	logged  [mixKinds]bool  // the kind of error was logged
}

// resetMixer makes the next mixer step send the list and the levels again,
// not before after. It is called when the keyboard is ready on a profile.
func (l *loop) resetMixer() {
	m := &l.mix
	m.list, m.sys, m.levels = nil, nil, nil
	m.dirty = m.audio != nil
}

// drainChanges notes a change signal of the audio, without waiting.
func (l *loop) drainChanges() {
	m := &l.mix
	for m.changes != nil {
		select {
		case _, ok := <-m.changes:
			if !ok {
				m.changes = nil
				return
			}
			m.dirty = true
		default:
			return
		}
	}
}

// mixLog logs an error of a kind unless one was logged since that kind last
// worked.
func (l *loop) mixLog(kind int, format string, args ...any) {
	if !l.mix.logged[kind] {
		l.mix.logged[kind] = true
		l.logf(format, args...)
	}
}

// mixerKB is the open keyboard's mixer, if there is a mixer and the keyboard
// is ready for it.
func (l *loop) mixerKB() (mixerKeyboard, bool) {
	if l.mix.audio == nil || l.kb == nil || l.paused {
		return nil, false
	}
	mk, ok := l.kb.(mixerKeyboard)
	return mk, ok
}

// mixStep reads the sessions when they changed and sends what differs from
// what the keyboard shows: the list (report 0x28) when the names changed, or
// else the level frames that changed; after a list, all the frames follow in
// a later step, once mixSettle has passed. An error of the audio pauses the mixer
// until the next change signal, reconnect or profile switch. A write the
// keyboard refused is tried again, all of it, once mixBackoff has passed; a
// stalled frame or a keyboard that is gone closes the keyboard, as for
// lighting.
func (l *loop) mixStep(ctx context.Context) {
	m := &l.mix
	mk, ok := l.mixerKB()
	if !ok || !m.dirty || l.now().Before(m.after) {
		return
	}
	m.dirty = false
	sys, progs, err := m.audio.Sessions()
	if err != nil {
		l.mixLog(mixRead, "reading the volumes: %v; the mixer page waits for the next change", err)
		return
	}
	m.logged[mixRead] = false
	progs = l.pickPrograms(progs)
	if err := l.sendMixer(mk, sys, progs); err != nil {
		m.list, m.sys, m.levels = nil, nil, nil // all again next time
		if errors.Is(err, keyboard.ErrStreamStalled) || errors.Is(err, device.ErrDisconnected) || errors.Is(err, device.ErrClosed) {
			l.logf("writing the mixer page: %v; opening the keyboard again", err)
			l.drop(ctx)
			return
		}
		m.dirty, m.after = true, l.now().Add(mixBackoff)
		l.mixLog(mixWrite, "writing the mixer page: %v; trying again in %v", err, mixBackoff)
		return
	}
	m.logged[mixWrite] = false
}

// mixDue tells how long until the mixer has something to send, when it waits
// for its time to come: the frames after a list, or a write after a refused
// one.
func (l *loop) mixDue() (time.Duration, bool) {
	if _, ok := l.mixerKB(); !ok || !l.mix.dirty {
		return 0, false
	}
	until := l.mix.after.Sub(l.now())
	return until, until > 0
}

// pickPrograms returns at most maxPrograms of progs. A shown program that
// still plays keeps its place; the other places go to the programs that play
// first, then to those first seen earliest in this run, then by ID. The
// programs picked are shown in the order they were first seen, those first
// seen together by ID, so that a program pausing or playing again does not
// move the rows.
func (l *loop) pickPrograms(progs []Session) []Session {
	m := &l.mix
	if m.first == nil {
		m.first = map[string]int{}
	}
	m.reads++
	for _, p := range progs {
		if _, ok := m.first[p.ID]; !ok {
			m.first[p.ID] = m.reads
		}
	}
	seen := func(a, b Session) int {
		return cmp.Or(cmp.Compare(m.first[a.ID], m.first[b.ID]), cmp.Compare(a.ID, b.ID))
	}
	var picked, rest []Session
	for _, p := range progs {
		if p.Active && slices.Contains(m.shown, p.ID) {
			picked = append(picked, p)
		} else {
			rest = append(rest, p)
		}
	}
	slices.SortStableFunc(rest, func(a, b Session) int {
		if a.Active != b.Active {
			if a.Active {
				return -1
			}
			return 1
		}
		return seen(a, b)
	})
	picked = append(picked, rest[:min(len(rest), max(maxPrograms-len(picked), 0))]...)
	slices.SortStableFunc(picked, seen)
	return picked
}

// sendMixer sends the list when its names changed and leaves all the level
// frames to the step after mixSettle. Otherwise it sends the program frames
// and the system frame that changed, in the order Swarm II sends them.
func (l *loop) sendMixer(mk mixerKeyboard, sys Session, progs []Session) error {
	m := &l.mix
	names := make([]string, len(progs))
	levels := make([]int, len(progs))
	ids := make([]string, len(progs))
	for i, p := range progs {
		names[i], levels[i], ids[i] = p.Name, p.Level, p.ID
	}
	if m.list == nil || !slices.Equal(m.list, names) {
		rows := []proto.MixerRow{{Name: proto.SystemRow, Level: sys.Level}}
		for _, p := range progs {
			rows = append(rows, proto.MixerRow{Name: p.Name, Level: p.Level})
		}
		b, err := proto.MixerList(rows)
		if err != nil {
			return err
		}
		m.list, m.sys, m.levels = nil, nil, nil
		if err := mk.WriteMixerList(b); err != nil {
			return err
		}
		m.list, m.shown = names, ids
		m.dirty, m.after = true, l.now().Add(mixSettle)
		return nil
	}
	m.shown = ids
	frames := proto.ProgramLevelFrames(levels)
	var old [][64]byte
	if m.levels != nil {
		old = proto.ProgramLevelFrames(m.levels)
	}
	for i, f := range frames {
		if old != nil && old[i] == f {
			continue
		}
		if err := mk.SendFrame(f); err != nil {
			m.levels = nil
			return err
		}
	}
	m.levels = levels
	f := proto.SystemLevelFrame(sys.Level, sys.Muted)
	if m.sys == nil || *m.sys != f {
		if err := mk.SendFrame(f); err != nil {
			return err
		}
		m.sys, m.muted = &f, sys.Muted
	}
	return nil
}

// systemSlide sets the system volume to the system slider's level. The
// keyboard already shows the level, so it counts as sent (with the mute sent
// last): a drag is not echoed back.
func (l *loop) systemSlide(ev proto.SystemSlide) {
	m := &l.mix
	if _, ok := l.mixerKB(); !ok {
		return
	}
	v := min(max(ev.Level, 0), 100)
	if err := m.audio.SetSystemLevel(v); err != nil {
		l.mixLog(mixSet, "setting the volume: %v", err)
		return
	}
	m.logged[mixSet] = false
	if m.sys != nil {
		f := proto.SystemLevelFrame(v, m.muted)
		m.sys = &f
	}
}

// programSlide sets the volume of the program shown on the slider's row;
// rows without a program are ignored. The keyboard already shows the level,
// so it counts as sent.
func (l *loop) programSlide(ev proto.ProgramSlide) {
	m := &l.mix
	if _, ok := l.mixerKB(); !ok || m.list == nil || ev.Row < 1 || ev.Row > len(m.shown) {
		return
	}
	v := min(max(ev.Level, 0), 100)
	if err := m.audio.SetProgramLevel(m.shown[ev.Row-1], v); err != nil {
		l.mixLog(mixSet, "setting the volume: %v", err)
		return
	}
	m.logged[mixSet] = false
	if ev.Row <= len(m.levels) {
		m.levels[ev.Row-1] = v
	}
}

// redrawMain is the Redraw view of a profile's main page.
const redrawMain = 0x20

// mixRedraw sends the levels again, as Swarm II does when the keyboard has
// drawn a new view: the list stays.
func (l *loop) mixRedraw() {
	m := &l.mix
	if m.audio == nil {
		return
	}
	m.sys, m.levels = nil, nil
	m.dirty = true
}
