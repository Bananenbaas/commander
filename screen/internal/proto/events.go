// SPDX-License-Identifier: GPL-2.0-or-later

package proto

// Touch is a touch or release on a widget, from input report 0x33. Page is
// byte 1 of the report (1 to 3). The mixer page's sliders, with byte 1 00 and
// kind 06 or 07, are parsed as SystemSlide and ProgramSlide instead.
type Touch struct {
	Page, Position, Kind byte
	Down                 bool
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
		if b[1] == 0x00 { // the volume mixer page
			switch b[3] {
			case 0x06:
				return SystemSlide{Level: int(b[7])}, true
			case 0x07:
				return ProgramSlide{Row: int(b[5]), Level: int(b[7])}, true
			}
		}
		return Touch{Page: b[1], Position: b[2], Kind: b[3], Down: b[7] == 1}, true
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
		case 0x0c:
			return Brightness{Percent: int(b[3])}, true
		case 0x11:
			return LightsOff{Off: b[3] == 1}, true
		case 0xf1:
			if len(b) < 7 || b[3] != 0x01 {
				return nil, false
			}
			return AppTap{ID: uint16(b[4]) | uint16(b[5])<<8, Down: b[6] == 1}, true
		}
	}
	return nil, false
}

// Brightness is the notification sent when the keyboard's brightness changes.
type Brightness struct{ Percent int }

// LightsOff is the notification sent by the RGB button; Off is true when the
// lights were switched off.
type LightsOff struct{ Off bool }
