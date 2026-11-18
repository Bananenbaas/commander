// SPDX-License-Identifier: GPL-2.0-or-later

package run

import (
	"fmt"
	"slices"
	"testing"

	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
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

// A switch the keyboard does not confirm counts as well, also when the
// keyboard stays open; a confirmed switch ends it.
func TestReplugCountsUnconfirmedSwitches(t *testing.T) {
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
		h.onPoll[3] = func() { h.running[game] = true }
		h.onPoll[4] = h.cancel
		got := statusRun(h)
		h.want("poll", "open", "notify", "close",
			"poll", "open", "notify", "close",
			"poll", "open", "notify", "fresh 0",
			"poll", "switch 2",
			"poll", "close")
		wantStatuses(h, got, Status{Profile: -1, Waiting: "keyboard"}, Status{Profile: -1, Waiting: WaitReplug}, Status{Profile: 2})
		h.wantLogged(1, replugMsg)
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
