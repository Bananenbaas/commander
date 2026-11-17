// SPDX-License-Identifier: GPL-2.0-or-later

package run

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/procs"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

const confirmPos = 12 // a tap of A that needs the finger to stay down

// confirmConfig is testConfig with a tap button of A at position 12 of
// profile 0 that fires after d.
func confirmConfig(d time.Duration) *layout.Config {
	cfg := testConfig()
	for i := range cfg.Profiles {
		if cfg.Profiles[i].Number == 0 {
			b := tapButton(confirmPos, chord(action.A))
			b.Confirm = d
			cfg.Profiles[i].Buttons = append(cfg.Profiles[i].Buttons, b)
		}
	}
	return cfg
}

func (h *harness) wantFiredOnce() {
	h.t.Helper()
	h.want("poll", "open", "notify", "fresh 0", "down A", "up A", "poll", "close")
	if len(h.downAt) != 1 || h.downAt[0] < time.Second || h.downAt[0] > time.Second+readTimeout {
		h.t.Errorf("fired at %v, want once from 1s to %v", h.downAt, time.Second+readTimeout)
	}
}

func TestConfirmReleasedTooSoonDoesNothing(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(confirmPos, true), idleFor(500 * time.Millisecond), touch(confirmPos, false)}}
		h.onPoll[1] = h.cancel
		h.run(confirmConfig(time.Second))
		h.want("poll", "open", "notify", "fresh 0", "poll", "close")
	})
}

func TestConfirmFiresOnceWhileHeld(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{
			touch(confirmPos, true), idleFor(600 * time.Millisecond),
			touch(confirmPos, true), // auto-repeat: does not restart the wait
			idleFor(900 * time.Millisecond),
			touch(confirmPos, true), // after it fired: no second action
			idleFor(100 * time.Millisecond),
			touch(confirmPos, false),
		}}
		h.onPoll[1] = h.cancel
		h.run(confirmConfig(time.Second))
		h.wantFiredOnce()
	})
}

func TestConfirmFiresWithoutOtherInput(t *testing.T) {
	test(t, func(h *harness) {
		// No release at all: the loop's own wake-ups find the deadline.
		h.scripts = [][]item{{touch(confirmPos, true)}}
		h.onPoll[1] = h.cancel
		h.run(confirmConfig(time.Second))
		h.wantFiredOnce()
		if h.maxWait > readTimeout {
			h.t.Errorf("NextEvent waited up to %v", h.maxWait)
		}
	})
}

func TestConfirmIsCancelled(t *testing.T) {
	for _, c := range []struct {
		name    string
		input   []item
		confirm time.Duration
		setup   func(h *harness)
		want    []string // programs launched
	}{
		{"profile change", script(touch(confirmPos, true), idleFor(500*time.Millisecond),
			event(proto.ProfileChanged{Profile: 0}), idleFor(time.Second)), time.Second, nil, nil},
		{"touch elsewhere", script(touch(confirmPos, true), idleFor(300*time.Millisecond),
			touch(7, true), idleFor(time.Second)), time.Second, nil, []string{"launch tool"}},
		{"keyboard error", script(touch(confirmPos, true), idleFor(500*time.Millisecond),
			failure(device.ErrDisconnected)), time.Second, nil, nil},
		{"swarm", script(touch(confirmPos, true), idleFor(3500*time.Millisecond)), 3 * time.Second,
			func(h *harness) { h.onPoll[1] = func() { h.running[procs.Swarm[0]] = true } }, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			test(t, func(h *harness) {
				h.scripts = [][]item{c.input}
				h.onPoll[1] = h.cancel
				if c.setup != nil {
					c.setup(h)
					h.onPoll[2] = h.cancel
				}
				h.run(confirmConfig(c.confirm))
				var launched []string
				for _, s := range h.log {
					if strings.HasPrefix(s, "down ") {
						h.t.Errorf("a key was pressed: %q", h.log)
					}
					if strings.HasPrefix(s, "launch ") {
						launched = append(launched, s)
					}
				}
				if !slices.Equal(launched, c.want) {
					h.t.Errorf("launched %q, want %q", launched, c.want)
				}
			})
		})
	}
}

func TestConfirmFlipsTheStateWhenConfirmed(t *testing.T) {
	for _, hold := range []time.Duration{500 * time.Millisecond, 1500 * time.Millisecond} {
		test(t, func(h *harness) {
			cfg := litConfig(t)
			for pi := range cfg.Profiles {
				for bi := range cfg.Profiles[pi].Buttons {
					if b := &cfg.Profiles[pi].Buttons[bi]; b.Position == 11 {
						b.Confirm = time.Second
					}
				}
			}
			h.lit(cfg)
			h.scripts = [][]item{{touch(11, true), idleFor(hold), touch(11, false)}}
			h.onPoll[2] = h.cancel
			h.run(cfg)
			wantOn := hold < time.Second // gear flips off only once confirmed
			if got := h.gearAt(3900 * time.Millisecond); got != wantOn {
				h.t.Errorf("held %v: gear on = %v, want %v", hold, got, wantOn)
			}
		})
	}
}
