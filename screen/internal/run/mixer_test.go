// SPDX-License-Identifier: GPL-2.0-or-later

package run

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/action"
	"github.com/Bananenbaas/commander/screen/internal/keyboard"
	"github.com/Bananenbaas/commander/screen/internal/proto"
)

var _ mixerKeyboard = (*keyboard.KB)(nil)

// WriteMixerList records the list's names in the log: "list
// SystemVolume|Steam". Every row must carry the full-scale value 100.
func (k *fakeKB) WriteMixerList(b []byte) error {
	k.enter("WriteMixerList")
	defer k.leave()
	if err := k.mixFails(); err != nil {
		return err
	}
	if len(b) != proto.Len28 || b[0] != 0x28 {
		k.h.t.Errorf("mixer list % x...", b[:min(len(b), 5)])
	}
	var rows []string
	for i := 0; i < int(b[3]); i++ {
		r := b[5+43*i : 5+43*(i+1)]
		if r[42] != 100 {
			k.h.t.Errorf("mixer list row %d: full scale %d, want 100", i, r[42])
		}
		rows = append(rows, string(bytes.TrimRight(r[1:42], "\x00")))
	}
	k.h.record("list " + strings.Join(rows, "|"))
	k.h.mixAt = append(k.h.mixAt, time.Since(k.h.start))
	return nil
}

// SendFrame records the level frame in the log: "system 50", "system 50
// muted", "programs 1-2: 42 100", "programs 3-4: 0 0".
func (k *fakeKB) SendFrame(f [64]byte) error {
	k.enter("SendFrame")
	defer k.leave()
	if err := k.mixFails(); err != nil {
		return err
	}
	switch {
	case f[0] == 0xa3 && f[4] == 0x00 && f[5] == 0x0d:
		s := fmt.Sprintf("system %d", f[6])
		if f[8] == 0 {
			s += " muted"
		}
		k.h.record(s)
	case f[0] == 0xa3 && f[4] == 0x03 && f[5] == 0x0e:
		k.h.record(fmt.Sprintf("programs 1-2: %d %d", f[7], f[10]))
	case f[0] == 0xa3 && f[4] == 0x06 && f[5] == 0x0e:
		k.h.record(fmt.Sprintf("programs 3-4: %d %d", f[7], f[10]))
	default:
		k.h.t.Errorf("frame % x", f[:12])
	}
	k.h.mixAt = append(k.h.mixAt, time.Since(k.h.start))
	return nil
}

func (k *fakeKB) mixFails() error {
	n := k.h.mixes
	k.h.mixes++
	if k.h.mixErr != nil {
		if err := k.h.mixErr(n); err != nil {
			k.h.record("mixer failed")
			return err
		}
	}
	return nil
}

// fakeAudio is the audio of the tests. Its setters record in the harness's
// log: "set system 95", "set b 30".
type fakeAudio struct {
	h  *harness
	ch chan struct{}

	mu    sync.Mutex
	sys   Session
	progs []Session
	err   error
	reads int
}

func newAudio(h *harness, sys int, progs ...Session) *fakeAudio {
	a := &fakeAudio{h: h, ch: make(chan struct{}, 1), sys: Session{ID: "sys", Name: "Speakers", Level: sys}, progs: progs}
	h.audio = a
	return a
}

func prog(id, name string, level int) Session { return Session{ID: id, Name: name, Level: level} }

func (a *fakeAudio) Sessions() (Session, []Session, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reads++
	if a.err != nil {
		return Session{}, nil, a.err
	}
	return a.sys, slices.Clone(a.progs), nil
}

func (a *fakeAudio) SetSystemLevel(v int) error {
	a.h.record(fmt.Sprintf("set system %d", v))
	return nil
}

func (a *fakeAudio) SetProgramLevel(id string, v int) error {
	a.h.record(fmt.Sprintf("set %s %d", id, v))
	return nil
}

func (a *fakeAudio) Changes() <-chan struct{} { return a.ch }

// change changes the sessions and signals it.
func (a *fakeAudio) change(f func(a *fakeAudio)) {
	a.mu.Lock()
	f(a)
	a.mu.Unlock()
	select {
	case a.ch <- struct{}{}:
	default:
	}
}

// startLog is the log up to the second poll, which comes before the mixer
// starts 2.5 s after notifications were enabled.
var startLog = []string{"poll", "open", "notify", "fresh 0", "poll"}

func logOf(parts ...[]string) []string { return slices.Concat(parts...) }

func TestMixerSendsListAndLevels(t *testing.T) {
	test(t, func(h *harness) {
		a := newAudio(h, 50, prog("a", "Steam", 42), prog("b", "Opera", 100))
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{
			"list SystemVolume|Steam|Opera", "programs 1-2: 42 100", "programs 3-4: 0 0", "system 50",
			"poll", "poll", "close",
		})...)
		if a.reads != 2 {
			h.t.Errorf("%d reads of the sessions, want 2: one for the list, one for the levels", a.reads)
		}
	})
}

// The level frames wait 600 ms after a list, as Swarm II's do: the keyboard
// ignores those sent right after it.
func TestMixerLevelsWaitAfterList(t *testing.T) {
	test(t, func(h *harness) {
		a := newAudio(h, 50, prog("a", "Steam", 42))
		h.onPoll[2] = func() {
			a.change(func(a *fakeAudio) { a.progs = append(a.progs, prog("b", "Opera", 100)) })
		}
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		sent := func(list string, levels string) []string {
			return []string{list, "programs 1-2: " + levels, "programs 3-4: 0 0", "system 50"}
		}
		h.want(logOf(startLog, sent("list SystemVolume|Steam", "42 0"),
			[]string{"poll"}, sent("list SystemVolume|Steam|Opera", "42 100"),
			[]string{"poll", "close"})...)
		lists := []time.Duration{h.notified(0) + quietGap, h.pollTimes[2]}
		for i, list := range lists {
			at := h.mixAt[4*i : 4*i+4]
			if at[0] != list {
				t.Errorf("list %d at %v, want %v", i, at[0], list)
			}
			for j, f := range at[1:] {
				if f != list+600*time.Millisecond {
					t.Errorf("list %d: frame %d at %v, want %v", i, j, f, list+600*time.Millisecond)
				}
			}
		}
	})
}

// A level that changes without a new list is sent at once.
func TestMixerLevelChangeSentAtOnce(t *testing.T) {
	test(t, func(h *harness) {
		a := newAudio(h, 50, prog("a", "Steam", 42))
		h.onPoll[2] = func() { a.change(func(a *fakeAudio) { a.progs[0].Level = 60 }) }
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{
			"list SystemVolume|Steam", "programs 1-2: 42 0", "programs 3-4: 0 0", "system 50",
			"poll", "programs 1-2: 60 0",
			"poll", "close",
		})...)
		if got := h.mixAt[len(h.mixAt)-1]; got != h.pollTimes[2] {
			t.Errorf("level frame at %v, want %v", got, h.pollTimes[2])
		}
	})
}

func TestMixerSendsOnlyChangedLevels(t *testing.T) {
	test(t, func(h *harness) {
		a := newAudio(h, 50, prog("a", "Steam", 42), prog("b", "Opera", 100))
		h.onPoll[2] = func() { a.change(func(a *fakeAudio) { a.progs[0].Level = 60 }) }
		h.onPoll[3] = func() { a.change(func(a *fakeAudio) { a.sys.Level, a.sys.Muted = 70, true }) }
		h.onPoll[4] = func() { a.change(func(*fakeAudio) {}) } // nothing changed
		h.onPoll[5] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{
			"list SystemVolume|Steam|Opera", "programs 1-2: 42 100", "programs 3-4: 0 0", "system 50",
			"poll", "programs 1-2: 60 100",
			"poll", "system 70 muted",
			"poll", "poll", "close",
		})...)
	})
}

func TestMixerNewProgramSendsNewList(t *testing.T) {
	test(t, func(h *harness) {
		a := newAudio(h, 50, prog("a", "Steam", 42))
		h.onPoll[2] = func() {
			a.change(func(a *fakeAudio) { a.progs = append(a.progs, prog("c", "Spotify · Premium", 80)) })
		}
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{
			"list SystemVolume|Steam", "programs 1-2: 42 0", "programs 3-4: 0 0", "system 50",
			"poll", "list SystemVolume|Steam|Spotify ? Premium", "programs 1-2: 42 80", "programs 3-4: 0 0", "system 50",
			"poll", "close",
		})...)
	})
}

func TestMixerShowsFourPrograms(t *testing.T) {
	test(t, func(h *harness) {
		a := newAudio(h, 50, prog("e", "E", 5), prog("d", "D", 4), prog("c", "C", 3), prog("b", "B", 2), prog("a", "A", 1))
		// A program seen later does not push one out, however its ID sorts.
		h.onPoll[2] = func() { a.change(func(a *fakeAudio) { a.progs = append(a.progs, prog("0", "Zero", 9)) }) }
		// When a shown program stops, the next one in order moves up.
		h.onPoll[3] = func() { a.change(func(a *fakeAudio) { a.progs = slices.Delete(a.progs, 4, 5) }) }
		h.onPoll[4] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{
			"list SystemVolume|A|B|C|D", "programs 1-2: 1 2", "programs 3-4: 3 4", "system 50",
			"poll",
			"poll", "list SystemVolume|B|C|D|E", "programs 1-2: 2 3", "programs 3-4: 4 5", "system 50",
			"poll", "close",
		})...)
	})
}

func playing(id, name string, level int) Session {
	return Session{ID: id, Name: name, Level: level, Active: true}
}

// Idle sessions do not keep a player started later off the page: it takes
// the row of an idle program.
func TestMixerActiveNewcomerReplacesIdle(t *testing.T) {
	test(t, func(h *harness) {
		a := newAudio(h, 50, prog("a", "A", 1), prog("b", "B", 2), prog("c", "C", 3), prog("d", "D", 4))
		h.onPoll[2] = func() { a.change(func(a *fakeAudio) { a.progs = append(a.progs, playing("z", "Z", 9)) }) }
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{
			"list SystemVolume|A|B|C|D", "programs 1-2: 1 2", "programs 3-4: 3 4", "system 50",
			"poll", "list SystemVolume|A|B|C|Z", "programs 1-2: 1 2", "programs 3-4: 3 9", "system 50",
			"poll", "close",
		})...)
	})
}

// Four playing programs keep their rows when a fifth starts to play; one
// that goes idle may then make room for it.
func TestMixerPlayingProgramsStay(t *testing.T) {
	test(t, func(h *harness) {
		a := newAudio(h, 50, playing("a", "A", 1), playing("b", "B", 2), playing("c", "C", 3), playing("d", "D", 4))
		h.onPoll[2] = func() { a.change(func(a *fakeAudio) { a.progs = append(a.progs, playing("0", "Zero", 9)) }) }
		h.onPoll[3] = func() { a.change(func(a *fakeAudio) { a.progs[1].Active = false }) }
		h.onPoll[4] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{
			"list SystemVolume|A|B|C|D", "programs 1-2: 1 2", "programs 3-4: 3 4", "system 50",
			"poll",
			"poll", "list SystemVolume|A|C|D|Zero", "programs 1-2: 1 3", "programs 3-4: 4 9", "system 50",
			"poll", "close",
		})...)
	})
}

func TestMixerSlides(t *testing.T) {
	test(t, func(h *harness) {
		a := newAudio(h, 50, prog("a", "Steam", 42), prog("b", "Opera", 100))
		h.scripts = [][]item{{
			idleFor(3500 * time.Millisecond),
			event(proto.ProgramSlide{Row: 2, Level: 30}),
			event(proto.ProgramSlide{Row: 3, Level: 10}), // no program on row 3
			event(proto.ProgramSlide{Row: 0, Level: 10}),
			event(proto.SystemSlide{Level: 95}),
		}}
		// The audio reports the slides: both levels are on the keyboard
		// already, so nothing is echoed back mid-drag.
		h.onPoll[2] = func() {
			a.change(func(a *fakeAudio) { a.progs[1].Level, a.sys.Level = 30, 95 })
		}
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{
			"list SystemVolume|Steam|Opera", "programs 1-2: 42 100", "programs 3-4: 0 0", "system 50",
			"set b 30", "set system 95",
			"poll",
			"poll", "close",
		})...)
	})
}

func TestMixerAudioErrorLoggedOnce(t *testing.T) {
	test(t, func(h *harness) {
		a := newAudio(h, 50, prog("a", "Steam", 42))
		a.err = errors.New("no output device")
		h.scripts = [][]item{script(idleFor(3*time.Second), tap(3))}
		h.onPoll[2] = func() { a.change(func(*fakeAudio) {}) }
		h.onPoll[3] = func() { a.change(func(a *fakeAudio) { a.err = nil }) }
		h.onPoll[4] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{
			"down A", "up A",
			"poll",
			"poll", "list SystemVolume|Steam", "programs 1-2: 42 0", "programs 3-4: 0 0", "system 50",
			"poll", "close",
		})...)
		h.wantLogged(1, "no output device")
		if a.reads != 4 {
			h.t.Errorf("%d reads, want 4: one at the start, one per change and one for the levels after the list", a.reads)
		}
	})
}

// A refused write waits 5 s, whatever changes meanwhile, and is then tried
// again without a new change.
func TestMixerWriteErrorBacksOff(t *testing.T) {
	test(t, func(h *harness) {
		a := newAudio(h, 50, prog("a", "Steam", 42))
		h.mixErr = func(n int) error {
			if n < 1 {
				return errors.New("keyboard: status 05 01 01 00 after the mixer list, want 05 01 00 00")
			}
			return nil
		}
		h.onPoll[2] = func() { a.change(func(*fakeAudio) {}) }
		h.onPoll[3] = func() { a.change(func(*fakeAudio) {}) }
		h.onPoll[5] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{
			"mixer failed", // 2.5 s
			"poll", "poll", // 4 s and 6 s: no write before 7.5 s
			"list SystemVolume|Steam",
			"poll", // 8 s; the frames follow at 8.1 s
			"programs 1-2: 42 0", "programs 3-4: 0 0", "system 50",
			"poll", "close",
		})...)
		h.wantLogged(1, "05 01 01 00")
	})
}

// A stalled level frame closes the keyboard, as a stalled lighting frame
// does; the page is sent again once it is open.
func TestMixerStalledFrameDropsKeyboard(t *testing.T) {
	test(t, func(h *harness) {
		newAudio(h, 50, prog("a", "Steam", 42))
		h.mixErr = func(n int) error {
			if n == 1 {
				return fmt.Errorf("keyboard: a3 frame: %w", keyboard.ErrStreamStalled)
			}
			return nil
		}
		h.onPoll[4] = h.cancel
		h.run(testConfig())
		sent := []string{"list SystemVolume|Steam", "programs 1-2: 42 0", "programs 3-4: 0 0", "system 50"}
		h.want(logOf(startLog, []string{"list SystemVolume|Steam", "mixer failed", "close",
			"poll", "open", "notify", "fresh 0", "poll"}, sent, []string{"poll", "close"})...)
		h.wantLogged(1, "did not take")
	})
}

func TestMixerAgainAfterSwitchAndReconnect(t *testing.T) {
	test(t, func(h *harness) {
		newAudio(h, 50, prog("a", "Steam", 42))
		h.scripts = [][]item{{idleFor(3500 * time.Millisecond), failure(errors.New("gone"))}}
		h.onPoll[4] = func() { h.running[game] = true }
		h.onPoll[6] = h.cancel
		h.run(testConfig())
		sent := []string{"list SystemVolume|Steam", "programs 1-2: 42 0", "programs 3-4: 0 0", "system 50"}
		h.want(logOf(startLog, sent, []string{"close", "poll", "open", "notify", "fresh 0", "poll"}, sent,
			[]string{"poll", "switch 2"}, sent, []string{"poll", "poll", "close"})...)
	})
}

func TestMixerRedrawSendsLevelsAgain(t *testing.T) {
	test(t, func(h *harness) {
		newAudio(h, 50, prog("a", "Steam", 42))
		h.scripts = [][]item{{idleFor(3500 * time.Millisecond), event(proto.Redraw{View: 0x20})}}
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{
			"list SystemVolume|Steam", "programs 1-2: 42 0", "programs 3-4: 0 0", "system 50",
			"programs 1-2: 42 0", "programs 3-4: 0 0", "system 50",
			"poll", "close",
		})...)
	})
}

func TestNoMixerWithoutAudio(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{
			idleFor(3 * time.Second),
			event(proto.ProgramSlide{Row: 1, Level: 30}),
			event(proto.SystemSlide{Level: 95}),
			event(proto.Redraw{View: 0x20}),
			touch(3, true), touch(3, false),
		}}
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want(logOf(startLog, []string{"down A", "up A", "poll", "close"})...)
		if h.mixes != 0 {
			h.t.Errorf("%d mixer writes", h.mixes)
		}
	})
}

// The slides are not touches: nothing on page 1 acts on them.
func TestSlidesAreNotTouches(t *testing.T) {
	test(t, func(h *harness) {
		cfg := testConfig()
		cfg.Profiles[1].Buttons = append(cfg.Profiles[1].Buttons, tapButton(0, chord(action.B)))
		h.scripts = [][]item{{event(proto.SystemSlide{Level: 0})}}
		h.onPoll[1] = h.cancel
		h.run(cfg)
		h.want("poll", "open", "notify", "fresh 0", "poll", "close")
	})
}
