// SPDX-License-Identifier: GPL-2.0-or-later

package keys

import "encoding/binary"

// The struct input_event of 64-bit Linux: a 16-byte timestamp, then type,
// code and value. 32-bit Linux uses a smaller timestamp and is not supported.
const (
	evdevSize = 24
	evKey     = 1
)

// decodeEvdev reads an input_event. ok is false for a short buffer and for
// events that are not key events. value is 1 for a press, 0 for a release and
// 2 for an auto-repeat.
func decodeEvdev(b []byte) (code uint16, value int32, ok bool) {
	if len(b) < evdevSize || binary.LittleEndian.Uint16(b[16:]) != evKey {
		return 0, 0, false
	}
	return binary.LittleEndian.Uint16(b[18:]), int32(binary.LittleEndian.Uint32(b[20:])), true
}
