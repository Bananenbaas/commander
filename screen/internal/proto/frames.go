// SPDX-License-Identifier: GPL-2.0-or-later

// Package proto builds and parses the frames the KB7 speaks over its
// vendor-defined HID reports, as documented in docs/PROTOCOL.md.
package proto

// Selectors for the Select frame.
const (
	SelButtons byte = 0xB9
	SelTypes   byte = 0xB8
	SelNames   byte = 0xBA
)

// Sum16 returns the sum of all bytes in b, truncated to 16 bits.
func Sum16(b []byte) uint16 {
	var s uint16
	for _, v := range b {
		s += uint16(v)
	}
	return s
}

// Notifications builds the frame that switches touch notifications on or off.
func Notifications(on bool) []byte {
	var v byte
	if on {
		v = 1
	}
	return []byte{0x0e, 0x05, v, 0x00, 0x00}
}

// SwitchProfile builds the frame that makes profile p the active one.
func SwitchProfile(p byte) []byte {
	return []byte{0x06, 0x04, p, 0x05}
}

// Select builds the frame that selects which table of a profile is read.
func Select(profile, selector byte) []byte {
	return []byte{0x05, profile, selector, 0x00}
}

// WidgetType builds the frame that sets the widget type and value of the
// button at position, followed by the 16-bit byte sum of the first ten
// bytes, little-endian. The frame names no profile and the keyboard queues
// it, so it changes whichever profile is active when the keyboard gets to it;
// kb7screen writes whole 0x25 tables instead and does not send it.
func WidgetType(position, typ, value byte) []byte {
	f := []byte{0x35, 0x0c, 0x01, position, 0x01, 0x00, typ, 0x00, value, 0x00}
	s := Sum16(f)
	return append(f, byte(s), byte(s>>8))
}
