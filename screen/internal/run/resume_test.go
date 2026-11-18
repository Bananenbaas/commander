// SPDX-License-Identifier: GPL-2.0-or-later

package run

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/procs"
)

// A profile chosen on the keyboard by hand, which the keyboard is known to
// be on, is kept without any switch, and the next polls leave it alone.
func TestResumeKeepsTheActiveProfileWithoutSwitching(t *testing.T) {
	test(t, func(h *harness) {
		h.resume = &Resume{Profile: 4, Active: true}
		h.scripts = [][]item{tap(3)} // profile 4 has no buttons: nothing happens
		h.onPoll[3] = h.cancel
		got := statusRun(h)
		h.want("poll", "open", "notify", "poll", "poll", "poll", "close")
		want := []Status{{Profile: -1, Waiting: "keyboard"}, {Profile: 4}}
		if !slices.Equal(*got, want) {
			h.t.Errorf("reports\n got %v\nwant %v", *got, want)
		}
		h.wantLogged(1, "keyboard ready on profile 4")
	})
}

// Program starts and stops after a resumed connect switch as always.
func TestResumeThenProgramsSwitchAsBefore(t *testing.T) {
	test(t, func(h *harness) {
		h.resume = &Resume{Profile: 4, Active: true}
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[2] = func() { h.running[game] = false }
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify",
			"poll", "switch 2",
			"poll", "switch 0",
			"poll", "close")
	})
}

// A running program whose profile differs from the resumed one wins; the
// active profile is known, so a plain switch is enough.
func TestResumeLosesToARunningProgram(t *testing.T) {
	test(t, func(h *harness) {
		h.resume = &Resume{Profile: 4, Active: true}
		h.running[game] = true
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 2", "poll", "poll", "close")
	})
}

func TestResumeOnTheRunningProgramsProfile(t *testing.T) {
	test(t, func(h *harness) {
		h.resume = &Resume{Profile: 2, Active: true}
		h.running[game] = true
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "poll", "poll", "close")
	})
}

// The desktop profile, resumed while active, needs no switch either.
func TestResumeOnTheExpectedProfile(t *testing.T) {
	test(t, func(h *harness) {
		h.resume = &Resume{Profile: 0, Active: true}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "poll", "close")
	})
}

// When the keyboard may have changed profile since (an Apply switched
// profiles), the resumed profile is still chosen, through another profile
// first.
func TestResumeNotKnownActive(t *testing.T) {
	test(t, func(h *harness) {
		h.resume = &Resume{Profile: 4}
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "fresh 4", "poll", "poll", "close")
	})
}

// Only the first connect of Run resumes; after an unplug Run connects as
// always.
func TestResumeOnlyAtTheFirstConnect(t *testing.T) {
	test(t, func(h *harness) {
		h.resume = &Resume{Profile: 4, Active: true}
		h.onPoll[1] = func() { h.push(failure(fmt.Errorf("%w: reading input", device.ErrDisconnected))) }
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify",
			"poll", "close",
			"poll", "open", "notify", "fresh 0",
			"poll", "close")
	})
}

// A keyboard that is gone may come back on any profile: the resume is
// dropped.
func TestResumeDroppedWhenTheKeyboardIsGone(t *testing.T) {
	test(t, func(h *harness) {
		h.resume = &Resume{Profile: 4, Active: true}
		h.openErr = func(n int) error {
			if n == 0 {
				return device.ErrNotFound
			}
			return nil
		}
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want("poll", "open failed", "poll", "open", "notify", "fresh 0", "poll", "close")
	})
}

// A connect that fails before it switches leaves the profile alone: the
// resume is kept for the next one.
func TestResumeKeptAcrossAFailureBeforeTheSwitch(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		test(t, func(h *harness) {
			h.resume = &Resume{Profile: 4, Active: true}
			h.openErr = func(n int) error {
				if n == 0 {
					return errors.New("access denied")
				}
				return nil
			}
			h.onPoll[2] = h.cancel
			h.run(testConfig())
			h.want("poll", "open failed", "poll", "open", "notify", "poll", "close")
		})
	})
	t.Run("notifications", func(t *testing.T) {
		test(t, func(h *harness) {
			h.resume = &Resume{Profile: 4, Active: true}
			h.notifyErr = func(n int) error {
				if n == 0 {
					return semaphore
				}
				return nil
			}
			h.onPoll[2] = h.cancel
			h.run(testConfig())
			h.want("poll", "open", "notify", "close", "poll", "open", "notify", "poll", "close")
		})
	})
}

// Swarm II may change the profile: once it ran, the resume is dropped.
func TestResumeDroppedBySwarm(t *testing.T) {
	test(t, func(h *harness) {
		h.resume = &Resume{Profile: 4, Active: true}
		h.running[procs.Swarm[0]] = true
		h.onPoll[1] = func() { h.running[procs.Swarm[0]] = false }
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want("poll", "poll", "open", "notify", "fresh 0", "read 0", "read 2", "read 3", "poll", "close")
	})
}
