// SPDX-License-Identifier: GPL-2.0-or-later

// Package keys reports the key presses of the KB7 itself, so that lighting can
// react to them. It only listens: nothing is stored, and presses of other
// keyboards are ignored.
package keys

import (
	"errors"
	"strings"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
)

// Event is one press or release of a key on the KB7. A held key repeats its
// down event.
type Event struct {
	Key  action.Key
	Down bool
	At   time.Time
}

// ErrUnsupported is returned by Listen on systems where the KB7's key presses
// cannot be read.
var ErrUnsupported = errors.New("reading the KB7's key presses is not supported on this system")

// The Raw Input flags of a keyboard event.
const (
	riKeyBreak = 0x01
	riKeyE0    = 0x02
	riKeyE1    = 0x04
)

// isKB7 reports whether a HID device name belongs to the KB7 (Turtle Beach,
// vendor 10F5, product 5038).
func isKB7(deviceName string) bool {
	return strings.Contains(strings.ToLower(deviceName), "vid_10f5&pid_5038")
}

// decodeWindows turns the make code and flags of a Raw Input keyboard event
// into a key and whether it went down. ok is false for keys that are not in
// the key table and for the E1 sequence of Pause.
func decodeWindows(makeCode, flags uint16) (key action.Key, down, ok bool) {
	if flags&riKeyE1 != 0 {
		return 0, false, false
	}
	code := makeCode
	if flags&riKeyE0 != 0 {
		code |= 0xE000
	}
	key, ok = action.KeyFromWindowsScan(code)
	return key, flags&riKeyBreak == 0, ok
}
