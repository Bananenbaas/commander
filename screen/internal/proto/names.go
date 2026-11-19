// SPDX-License-Identifier: GPL-2.0-or-later

package proto

import "fmt"

const (
	// Len16 is the length of report 0x16 as GetFeature returns it and as it
	// is written: the report id, 0x68, five name fields and a 16-bit sum.
	Len16 = 104

	nameSlots = 5
	nameSize  = 20 // bytes of one name field
	nameBase  = 2  // where the first name field starts

	// MaxNameLen is the longest profile name: the field keeps a NUL after it.
	MaxNameLen = nameSize - 1
)

// Table16 is report 0x16, the names of the five profiles: `16 68`, five
// 20-byte fields from offset 2 and a 16-bit little-endian sum of the first
// 102 bytes. Bytes after a name's NUL, and every field not renamed, are kept
// as read.
type Table16 struct{ b [Len16]byte }

// ParseTable16 parses report 0x16 as GetFeature returns it, report ID first.
func ParseTable16(b []byte) (*Table16, error) {
	if len(b) != Len16 {
		return nil, fmt.Errorf("report 0x16: %d bytes, want %d", len(b), Len16)
	}
	if b[0] != 0x16 || b[1] != 0x68 {
		return nil, fmt.Errorf("report 0x16: starts %02x %02x, want 16 68", b[0], b[1])
	}
	s := Sum16(b[:Len16-2])
	if b[Len16-2] != byte(s) || b[Len16-1] != byte(s>>8) {
		return nil, fmt.Errorf("report 0x16: bad checksum")
	}
	t := new(Table16)
	copy(t.b[:], b)
	return t, nil
}

func (t *Table16) field(slot int) []byte {
	return t.b[nameBase+slot*nameSize : nameBase+(slot+1)*nameSize]
}

// Name is the name of profile slot, up to its first NUL; empty for a slot
// outside 0 to 4.
func (t *Table16) Name(slot int) string {
	if slot < 0 || slot >= nameSlots {
		return ""
	}
	f := t.field(slot)
	n := 0
	for n < len(f) && f[n] != 0 {
		n++
	}
	return string(f[:n])
}

// CheckName reports why name cannot be a profile name: it must be 1 to 19
// printable ASCII characters.
func CheckName(name string) error {
	if len(name) < 1 || len(name) > MaxNameLen {
		return fmt.Errorf("a profile name is 1 to %d characters, got %d", MaxNameLen, len(name))
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c < 0x20 || c > 0x7e {
			return fmt.Errorf("a profile name is printable ASCII, got byte 0x%02x", c)
		}
	}
	return nil
}

// SetName gives profile slot the name, zero-filling the rest of that field
// only.
func (t *Table16) SetName(slot int, name string) error {
	if slot < 0 || slot >= nameSlots {
		return fmt.Errorf("report 0x16: slot %d is not from 0 to %d", slot, nameSlots-1)
	}
	if err := CheckName(name); err != nil {
		return err
	}
	f := t.field(slot)
	clear(f)
	copy(f, name)
	return nil
}

// Bytes returns the 104-byte report to write, with its sum recomputed.
func (t *Table16) Bytes() []byte {
	out := make([]byte, Len16)
	copy(out, t.b[:])
	s := Sum16(out[:Len16-2])
	out[Len16-2], out[Len16-1] = byte(s), byte(s>>8)
	return out
}
