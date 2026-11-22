// SPDX-License-Identifier: GPL-2.0-or-later

package run

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/device"
	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/procs"
	"github.com/Bananenbaas/commander/screen/internal/proto"
)

// semaphore is how Windows reports a control write the keyboard does not
// take.
var semaphore = fmt.Errorf("%w: the semaphore timeout period has expired", device.ErrTimeout)

const replugMsg = "the keyboard does not answer; unplug it and plug it back in"

func wantStatuses(h *harness, got *[]Status, want ...Status) {
	h.t.Helper()
	if !slices.Equal(*got, want) {
		h.t.Errorf("reports\n got %v\nwant %v", *got, want)
	}
}

// Three failed connects in a row ask for a replug, once; Run keeps trying,
// and a connect that works ends it.
func TestReplugAfterThreeFailures(t *testing.T) {
	test(t, func(h *harness) {
		h.notifyErr = func(n int) error {
			if n < 4 {
				return semaphore
			}
			return nil
		}
		h.onPoll[5] = h.cancel
		got := statusRun(h)
		h.want("poll", "open", "notify", "close",
			"poll", "open", "notify", "close",
			"poll", "open", "notify", "close",
			"poll", "open", "notify", "close",
			"poll", "open", "notify", "fresh 0",
			"poll", "close")
		wantStatuses(h, got, Status{Profile: -1, Waiting: "keyboard"}, Status{Profile: -1, Waiting: WaitReplug}, Status{Profile: 0})
		h.wantLogged(1, replugMsg)
	})
}

// Switches the keyboard does not confirm count; once the keyboard answers
// again, here with a touch, the replug status ends.
func TestReplugAfterUnconfirmedSwitchesEndsWhenTheKeyboardAnswers(t *testing.T) {
	test(t, func(h *harness) {
		h.switchErr = func(int) error { return unconfirmed }
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[2] = func() { h.running[game] = false }
		h.onPoll[3] = func() { h.push(touch(9, false)) }
		h.onPoll[4] = h.cancel
		got := statusRun(h)
		h.want("poll", "open", "notify", "fresh 0",
			"poll", "switch 2",
			"poll", "switch 0",
			"poll", "poll", "close")
		wantStatuses(h, got, Status{Profile: -1, Waiting: "keyboard"},
			Status{Profile: 0, Unconfirmed: true}, Status{Profile: 2, Unconfirmed: true},
			Status{Profile: -1, Waiting: WaitReplug}, Status{Profile: 0, Unconfirmed: true})
		h.wantLogged(1, replugMsg)
	})
}

// Notifications that are enabled at last show the keyboard answers: two
// timeouts before and an unconfirmed switch after do not ask for a replug.
func TestEnabledNotificationsEndTheFailures(t *testing.T) {
	test(t, func(h *harness) {
		h.notifyErr = func(n int) error {
			if n < 2 {
				return semaphore
			}
			return nil
		}
		h.switchErr = func(n int) error {
			if n == 0 {
				return unconfirmed
			}
			return nil
		}
		h.onPoll[4] = h.cancel
		got := statusRun(h)
		h.want("poll", "open", "notify", "close",
			"poll", "open", "notify", "close",
			"poll", "open", "notify", "fresh 0",
			"poll", "poll", "close")
		wantStatuses(h, got, Status{Profile: -1, Waiting: "keyboard"}, Status{Profile: 0, Unconfirmed: true})
		h.wantLogged(0, replugMsg)
	})
}

// Swarm II taking the keyboard is a new start as well.
func TestReplugCountRestartsWhenSwarmTakesTheKeyboard(t *testing.T) {
	test(t, func(h *harness) {
		h.notifyErr = func(n int) error {
			if n < 3 {
				return semaphore
			}
			return nil
		}
		h.onPoll[2] = func() { h.running[procs.Swarm[0]] = true }
		h.onPoll[3] = func() { h.running[procs.Swarm[0]] = false }
		h.onPoll[5] = h.cancel
		got := statusRun(h)
		wantStatuses(h, got, Status{Profile: -1, Waiting: "keyboard"}, Status{Profile: -1, Waiting: "Swarm II"},
			Status{Profile: -1, Waiting: "keyboard"}, Status{Profile: 0})
		h.wantLogged(0, replugMsg)
	})
}

// A profile change reported by the keyboard makes the active profile known.
func TestProfileChangeConfirmsTheActiveProfile(t *testing.T) {
	test(t, func(h *harness) {
		h.switchErr = func(int) error { return unconfirmed }
		h.scripts = [][]item{{event(proto.ProfileChanged{Profile: 2})}}
		h.onPoll[1] = h.cancel
		got := statusRun(h)
		wantStatuses(h, got, Status{Profile: -1, Waiting: "keyboard"}, Status{Profile: 0, Unconfirmed: true}, Status{Profile: 2})
	})
}

// A keyboard that is gone is a new start: the failures before do not count.
func TestReplugCountRestartsWhenTheKeyboardIsGone(t *testing.T) {
	test(t, func(h *harness) {
		h.openErr = func(n int) error {
			if n == 2 {
				return device.ErrNotFound
			}
			return nil
		}
		h.notifyErr = func(n int) error {
			if n < 4 {
				return semaphore
			}
			return nil
		}
		h.onPoll[6] = h.cancel
		got := statusRun(h)
		wantStatuses(h, got, Status{Profile: -1, Waiting: "keyboard"}, Status{Profile: 0})
		h.wantLogged(0, replugMsg)
	})
}

// After a replug was asked for and the keyboard answered again, a new run of
// failures asks again.
func TestReplugAskedAgainAfterRecovery(t *testing.T) {
	test(t, func(h *harness) {
		h.notifyErr = func(n int) error {
			if n < 3 || (n >= 4 && n < 7) {
				return semaphore
			}
			return nil
		}
		// The fourth connect works; then the keyboard is lost and fails three
		// times more.
		h.onPoll[4] = func() { h.push(failure(fmt.Errorf("%w: reading input", device.ErrDisconnected))) }
		h.onPoll[9] = h.cancel
		got := statusRun(h)
		wantStatuses(h, got,
			Status{Profile: -1, Waiting: "keyboard"}, Status{Profile: -1, Waiting: WaitReplug}, Status{Profile: 0},
			Status{Profile: -1, Waiting: "keyboard"}, Status{Profile: -1, Waiting: WaitReplug}, Status{Profile: 0})
		h.wantLogged(2, replugMsg)
	})
}

// traceLog is a Trace that notes, in the harness's messages, every request
// to write the trace, and keeps the markers.
type traceLog struct {
	h        *harness
	marks    []string
	answered int
}

func (t *traceLog) Dump(reason string) { t.h.logf("trace dump: %s", reason) }
func (t *traceLog) Mark(text string)   { t.marks = append(t.marks, text) }
func (t *traceLog) Answered()          { t.answered++ }

// tracedRun runs testConfig with a traceLog and returns it.
func tracedRun(h *harness) *traceLog {
	tr := &traceLog{h: h}
	d := h.deps()
	d.Trace = tr
	if err := Run(h.ctx, testConfig(), d); err != nil {
		h.t.Fatalf("Run: %v", err)
	}
	return tr
}

// wantDumpsAfterReplug checks that the trace is asked for n times, each
// right after the replug advice.
func (h *harness) wantDumpsAfterReplug(n int) {
	h.t.Helper()
	h.mu.Lock()
	msgs := slices.Clone(h.msgs)
	h.mu.Unlock()
	dumps := 0
	for i, m := range msgs {
		if !strings.HasPrefix(m, "trace dump: ") {
			continue
		}
		dumps++
		if i == 0 || msgs[i-1] != replugMsg {
			h.t.Errorf("trace asked for after %q, want after the replug advice", msgs[max(i-1, 0)])
		}
		if want := "trace dump: the keyboard does not answer: 3 failed connects or switches in a row"; m != want {
			h.t.Errorf("got %q, want %q", m, want)
		}
	}
	if dumps != n {
		h.t.Errorf("trace asked for %d times, want %d; messages: %q", dumps, n, msgs)
	}
}

// The trace is written with the replug advice, once however long the
// keyboard keeps failing.
func TestReplugWritesTheTrace(t *testing.T) {
	test(t, func(h *harness) {
		h.notifyErr = func(n int) error {
			if n < 6 {
				return semaphore
			}
			return nil
		}
		h.onPoll[7] = h.cancel
		tracedRun(h)
		h.wantLogged(1, replugMsg)
		h.wantDumpsAfterReplug(1)
	})
}

// After the keyboard answered, a new run of failures writes it again.
func TestReplugWritesTheTraceOncePerEpisode(t *testing.T) {
	test(t, func(h *harness) {
		h.notifyErr = func(n int) error {
			if n < 3 || (n >= 4 && n < 7) {
				return semaphore
			}
			return nil
		}
		h.onPoll[4] = func() { h.push(failure(fmt.Errorf("%w: reading input", device.ErrDisconnected))) }
		h.onPoll[9] = h.cancel
		tracedRun(h)
		h.wantDumpsAfterReplug(2)
	})
}

// A program of switch_when_running seen ending is marked in the trace, and
// so is the switch that follows, with the time in between.
func TestProgramEndIsMarked(t *testing.T) {
	test(t, func(h *harness) {
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[2] = func() { h.running[game] = false }
		h.onPoll[3] = h.cancel
		tr := tracedRun(h)
		want := []string{"program game.exe ended", "switching to profile 0, 0ms after game.exe was seen ending"}
		if !slices.Equal(tr.marks, want) {
			t.Errorf("marks %q, want %q", tr.marks, want)
		}
	})
}

// A game without a kb7 profile cannot cause a switch, so its end is not marked.
func TestProgramEndWithoutProfileIsNotMarked(t *testing.T) {
	test(t, func(h *harness) {
		cfg := &layout.Config{
			Profiles: []layout.Profile{{Number: 0}},
			Games:    []layout.Game{{Name: "Game", Program: game}},
			Desktop:  &layout.Game{Name: "Desktop", KB7: kb7(0)},
		}
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[2] = func() { h.running[game] = false }
		h.onPoll[3] = h.cancel
		tr := &traceLog{h: h}
		d := h.deps()
		d.Trace = tr
		if err := Run(h.ctx, cfg, d); err != nil {
			h.t.Fatalf("Run: %v", err)
		}
		if len(tr.marks) != 0 {
			t.Errorf("marks %q, want none", tr.marks)
		}
	})
}

// Only a switch the keyboard confirmed ends the trace's episode.
func TestConfirmedSwitchesEndTheTraceEpisode(t *testing.T) {
	test(t, func(h *harness) {
		h.switchErr = func(n int) error {
			if n == 2 {
				return unconfirmed
			}
			return nil
		}
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[2] = func() { h.running[game] = false }
		h.onPoll[3] = h.cancel
		tr := tracedRun(h)
		h.want("poll", "open", "notify", "fresh 0",
			"poll", "switch 2",
			"poll", "switch 0",
			"poll", "close")
		if tr.answered != 2 {
			t.Errorf("episode ended %d times, want 2: by the first two switches, not the unconfirmed one", tr.answered)
		}
	})
}

// Enabled notifications and an unconfirmed switch do not end the episode.
func TestFailedSwitchDoesNotEndTheTraceEpisode(t *testing.T) {
	test(t, func(h *harness) {
		h.switchErr = func(int) error { return unconfirmed }
		h.onPoll[1] = h.cancel
		tr := tracedRun(h)
		if tr.answered != 0 {
			t.Errorf("episode ended %d times, want 0", tr.answered)
		}
	})
}
