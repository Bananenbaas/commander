package proto

import (
	"errors"
	"fmt"
)

const (
	len29     = 584
	entries29 = 35
	entryBase = 7
	entrySize = 16
	labelOff  = 4
	labelSize = 12
	maxLabel  = labelSize - 1
	len25     = 164
	entries25 = 38
	typeBase  = 10
	typeSize  = 4
	keyApp    = 0x01
)

const (
	// KindApp is the kind byte of a 0x29 entry that makes a position an
	// application button.
	KindApp = 0x0b
	// TypeApp is the widget type of an application button in the 0x25 table
	// and report 0x35. The touch report 0x33 carries the touched position's
	// widget type as its kind, so an application button's touch has kind
	// TypeApp.
	TypeApp = 0x05
)

// Entry29 is one button position of the 0x29 table.
type Entry29 struct {
	Mod, Key, Kind, Slot byte
	Label                string
}

// AppEntry returns the entry that makes a position an application button
// showing the picture in the given slot, with the given label.
func AppEntry(slot byte, label string) Entry29 {
	return Entry29{Mod: 0x00, Key: keyApp, Kind: KindApp, Slot: slot, Label: label}
}

// Table29 is the 584-byte button table of one profile. Bytes it does not
// interpret are kept exactly as read.
type Table29 struct{ b [len29]byte }

func checkSum(b []byte, id, hdr byte, what string) error {
	n := len(b) - 2
	if b[0] != id {
		return fmt.Errorf("%s: report id 0x%02x, want 0x%02x", what, b[0], id)
	}
	if b[1] != hdr {
		return fmt.Errorf("%s: size byte 0x%02x, want 0x%02x", what, b[1], hdr)
	}
	s := Sum16(b[:n])
	if b[n] != byte(s) || b[n+1] != byte(s>>8) {
		return fmt.Errorf("%s: bad checksum", what)
	}
	return nil
}

func fillSum(b []byte) {
	n := len(b) - 2
	s := Sum16(b[:n])
	b[n], b[n+1] = byte(s), byte(s>>8)
}

// ParseTable29 validates and copies a 0x29 block.
func ParseTable29(b []byte) (*Table29, error) {
	if len(b) != len29 {
		return nil, fmt.Errorf("table 0x29: %d bytes, want %d", len(b), len29)
	}
	if err := checkSum(b, 0x29, 0x48, "table 0x29"); err != nil {
		return nil, err
	}
	if b[2] != 0x02 {
		return nil, fmt.Errorf("table 0x29: third byte 0x%02x, want 0x02", b[2])
	}
	t := &Table29{}
	copy(t.b[:], b)
	return t, nil
}

// Profile returns the profile number (0-4) the table belongs to.
func (t *Table29) Profile() byte { return t.b[3] }

// Entry returns position pos (0-34). Out-of-range positions return the zero
// Entry29.
func (t *Table29) Entry(pos int) Entry29 {
	if pos < 0 || pos >= entries29 {
		return Entry29{}
	}
	o := entryBase + entrySize*pos
	lab := t.b[o+labelOff : o+labelOff+labelSize]
	n := 0
	for n < len(lab) && lab[n] != 0 {
		n++
	}
	return Entry29{t.b[o], t.b[o+1], t.b[o+2], t.b[o+3], string(lab[:n])}
}

// SetEntry writes position pos (0-34). The label may hold at most 11 printable
// ASCII characters.
func (t *Table29) SetEntry(pos int, e Entry29) error {
	if pos < 0 || pos >= entries29 {
		return fmt.Errorf("table 0x29: position %d outside 0-%d", pos, entries29-1)
	}
	if len(e.Label) > maxLabel {
		return fmt.Errorf("table 0x29: label %q longer than %d bytes", e.Label, maxLabel)
	}
	for i := 0; i < len(e.Label); i++ {
		if c := e.Label[i]; c < 0x20 || c > 0x7e {
			return fmt.Errorf("table 0x29: label %q has a byte outside printable ASCII", e.Label)
		}
	}
	o := entryBase + entrySize*pos
	t.b[o], t.b[o+1], t.b[o+2], t.b[o+3] = e.Mod, e.Key, e.Kind, e.Slot
	lab := t.b[o+labelOff : o+labelOff+labelSize]
	clear(lab)
	copy(lab, e.Label)
	return nil
}

// Bytes returns a copy of the block with the checksum recomputed.
func (t *Table29) Bytes() []byte {
	out := append([]byte(nil), t.b[:]...)
	fillSum(out)
	return out
}

// TypeEntry is one position of the 0x25 type table.
type TypeEntry struct{ Type, Value byte }

// AppType is the type of a position that holds an application button.
var AppType = TypeEntry{TypeApp, 0x06}

// Table25 is the 164-byte type table of one profile. Bytes it does not
// interpret are kept exactly as read.
type Table25 struct{ b [len25]byte }

// ParseTable25 validates and copies a 0x25 block.
func ParseTable25(b []byte) (*Table25, error) {
	if len(b) != len25 {
		return nil, fmt.Errorf("table 0x25: %d bytes, want %d", len(b), len25)
	}
	if err := checkSum(b, 0x25, 0xa4, "table 0x25"); err != nil {
		return nil, err
	}
	t := &Table25{}
	copy(t.b[:], b)
	return t, nil
}

// Profile returns the profile number (0-4) the table belongs to.
func (t *Table25) Profile() byte { return t.b[2] }

// Type returns position pos (0-37). Out-of-range positions return the zero
// TypeEntry.
func (t *Table25) Type(pos int) TypeEntry {
	if pos < 0 || pos >= entries25 {
		return TypeEntry{}
	}
	o := typeBase + typeSize*pos
	return TypeEntry{t.b[o], t.b[o+2]}
}

// SetType writes position pos (0-37).
func (t *Table25) SetType(pos int, te TypeEntry) error {
	if pos < 0 || pos >= entries25 {
		return errors.New("table 0x25: position outside 0-37")
	}
	o := typeBase + typeSize*pos
	t.b[o], t.b[o+1], t.b[o+2], t.b[o+3] = te.Type, 0, te.Value, 0
	return nil
}

// Bytes returns a copy of the block with the checksum recomputed.
func (t *Table25) Bytes() []byte {
	out := append([]byte(nil), t.b[:]...)
	fillSum(out)
	return out
}
