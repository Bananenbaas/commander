// SPDX-License-Identifier: GPL-2.0-or-later

package proto

// Touch is a touch or release on a widget, from input report 0x33.
type Touch struct {
	Position, Kind byte
	Down           bool
}

// ProfileChanged is the notification sent when the active profile changes.
type ProfileChanged struct{ Profile byte }

// Redraw is the notification asking for a view to be redrawn. Position is
// only set when View is 0x01 (a single widget).
type Redraw struct{ View, Position byte }

// AppTap is a press or release of an application button.
type AppTap struct {
	ID   uint16
	Down bool
}

// ParseReport decodes an input report. It returns false for reports that are
// not understood or too short to read.
func ParseReport(b []byte) (any, bool) {
	if len(b) == 0 {
		return nil, false
	}
	switch b[0] {
	case 0x33:
		if len(b) < 8 {
			return nil, false
		}
		return Touch{Position: b[2], Kind: b[3], Down: b[7] == 1}, true
	case 0x03:
		if len(b) < 4 || b[1] != 0x00 {
			return nil, false
		}
		switch b[2] {
		case 0x01:
			return ProfileChanged{Profile: b[3]}, true
		case 0x31:
			r := Redraw{View: b[3]}
			if r.View == 0x01 {
				if len(b) < 5 {
					return nil, false
				}
				r.Position = b[4]
			}
			return r, true
		case 0xf1:
			if len(b) < 7 || b[3] != 0x01 {
				return nil, false
			}
			return AppTap{ID: uint16(b[4]) | uint16(b[5])<<8, Down: b[6] == 1}, true
		}
	}
	return nil, false
}
