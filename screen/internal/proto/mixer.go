// SPDX-License-Identifier: GPL-2.0-or-later

package proto

import (
	"errors"
	"fmt"
)

const (
	// Len28 is the length of report 0x28, the volume mixer's list.
	Len28 = 308

	// SystemRow is the name of row 0 of the mixer list.
	SystemRow = "SystemVolume"

	// MaxMixerRows is the most rows sent: the system volume and four
	// programs, the programs that the 03 0e and 06 0e level frames carry.
	MaxMixerRows = 5

	// MaxMixerName is the longest name in a row: the 41-byte field keeps a
	// NUL after it.
	MaxMixerName = 40

	mixerBase    = 5  // where the first row starts
	mixerRowSize = 43 // row number, 41-byte name field, level
	mixerName    = 41
)

// MixerRow is one row of the volume mixer page: a name and a level, 0 to 100.
type MixerRow struct {
	Name  string
	Level int
}

// SystemSlide is the system volume slider moved on the mixer page.
type SystemSlide struct{ Level int }

// ProgramSlide is a program's slider moved on the mixer page. Row is the
// program's row, 1 for the first program.
type ProgramSlide struct{ Row, Level int }

// MixerList builds report 0x28, the rows of the volume mixer page: `28 34 01`,
// the row count, `00`, one 43-byte row per entry from offset 5 (row number,
// the name in a 41-byte zero-padded field, the level), zeros, and the 16-bit
// little-endian sum of the first 306 bytes. Row 0 must be SystemRow. Names
// are sent as ASCII: other characters become '?', and they are cut to
// MaxMixerName bytes.
func MixerList(rows []MixerRow) ([]byte, error) {
	switch {
	case len(rows) == 0:
		return nil, errors.New("mixer list: no rows")
	case len(rows) > MaxMixerRows:
		return nil, fmt.Errorf("mixer list: %d rows, at most %d", len(rows), MaxMixerRows)
	case rows[0].Name != SystemRow:
		return nil, fmt.Errorf("mixer list: row 0 is %q, want %q", rows[0].Name, SystemRow)
	}
	b := make([]byte, Len28)
	b[0], b[1], b[2], b[3] = 0x28, 0x34, 0x01, byte(len(rows))
	for i, r := range rows {
		row := b[mixerBase+i*mixerRowSize:]
		row[0] = byte(i)
		copy(row[1:1+MaxMixerName], asciiName(r.Name))
		row[1+mixerName] = level(r.Level)
	}
	s := Sum16(b[:Len28-2])
	b[Len28-2], b[Len28-1] = byte(s), byte(s>>8)
	return b, nil
}

// asciiName replaces every character that is not printable ASCII with '?'
// and cuts the result to MaxMixerName bytes.
func asciiName(name string) []byte {
	var out []byte
	for _, r := range name {
		if len(out) == MaxMixerName {
			break
		}
		if r < 0x20 || r > 0x7e {
			r = '?'
		}
		out = append(out, byte(r))
	}
	return out
}

// level clamps v to 0..100.
func level(v int) byte {
	return byte(min(max(v, 0), 100))
}

// SystemLevelFrame builds the a3 frame with the system volume:
// `a3 00 01 00 00 0d <v> 00 <unmuted> 00 <v>`, padded to 64 bytes.
func SystemLevelFrame(v int, muted bool) [64]byte {
	var f [64]byte
	copy(f[:], []byte{0xa3, 0x00, 0x01, 0x00, 0x00, 0x0d})
	f[6], f[10] = level(v), level(v)
	if !muted {
		f[8] = 1
	}
	return f
}

// ProgramLevelFrames builds the two a3 frames with the programs' levels:
// `a3 00 01 00 03 0e 00 <p1> <p1> 00 <p2> <p2>` and the same with `06 0e`
// for programs 3 and 4. A program not in levels is sent as 00 00; levels
// past the fourth are ignored.
func ProgramLevelFrames(levels []int) [][64]byte {
	frames := make([][64]byte, 2)
	for i := range frames {
		f := &frames[i]
		copy(f[:], []byte{0xa3, 0x00, 0x01, 0x00, byte(3 + 3*i), 0x0e})
		for j := 0; j < 2; j++ {
			if k := 2*i + j; k < len(levels) {
				f[7+3*j], f[8+3*j] = level(levels[k]), level(levels[k])
			}
		}
	}
	return frames
}
