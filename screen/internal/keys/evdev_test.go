// SPDX-License-Identifier: GPL-2.0-or-later

package keys

import (
	"encoding/binary"
	"testing"
)

// event builds a 24-byte input_event of 64-bit Linux.
func event(typ, code uint16, value int32) []byte {
	b := make([]byte, evdevSize)
	binary.LittleEndian.PutUint16(b[16:], typ)
	binary.LittleEndian.PutUint16(b[18:], code)
	binary.LittleEndian.PutUint32(b[20:], uint32(value))
	return b
}

func TestDecodeEvdev(t *testing.T) {
	code, value, ok := decodeEvdev(event(1, 17, 1))
	if !ok || code != 17 || value != 1 {
		t.Errorf("W down: got %d %d %v", code, value, ok)
	}
	code, value, ok = decodeEvdev(event(1, 17, 0))
	if !ok || code != 17 || value != 0 {
		t.Errorf("W up: got %d %d %v", code, value, ok)
	}
	if _, _, ok = decodeEvdev(event(1, 17, 2)); !ok {
		t.Error("repeat must decode; the caller drops it")
	}
	if _, _, ok = decodeEvdev(event(0, 0, 0)); ok {
		t.Error("EV_SYN accepted")
	}
	if _, _, ok = decodeEvdev(event(1, 17, 1)[:20]); ok {
		t.Error("short buffer accepted")
	}
}
