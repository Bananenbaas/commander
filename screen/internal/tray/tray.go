// SPDX-License-Identifier: GPL-2.0-or-later

// Package tray shows an icon with a menu in the Windows notification area.
package tray

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unicode/utf16"
)

// Item is one entry of the tray menu.
type Item struct {
	Label string
	// Checked, when set, is asked each time the menu opens; true shows a tick.
	Checked func() bool
	// OnClick runs on a goroutine of its own when the item is chosen.
	OnClick func()
	// Separator makes the item a line; the other fields are ignored.
	Separator bool
}

// Options describe the tray icon.
type Options struct {
	Icon       []byte // ICO file for the normal state
	FailedIcon []byte // ICO file for the error state; Icon when empty
	Tip        string // tooltip, cut to 127 UTF-16 units
	Items      []Item
	// OnEndSession is called when Windows asks to end the session (log off,
	// shut down). It must return within 5 s. It runs on the window thread,
	// so it may call SetTip, SetFailed and Balloon (they only post) but must
	// not wait for anything the window thread does. When the end of the
	// session is then called off, the tray stays and Done stays open, so
	// whatever OnEndSession stopped must be restarted or ended by the caller.
	OnEndSession func()
}

// Limits of the NOTIFYICONDATAW text fields in UTF-16 units, without the
// terminating zero.
const (
	maxTip   = 127
	maxInfo  = 255
	maxTitle = 63
)

// cutTip cuts s to what fits in the tooltip.
func cutTip(s string) string { return cutUnits(s, maxTip) }

// cutUnits cuts s to at most n UTF-16 units without splitting a character.
func cutUnits(s string, n int) string {
	used := 0
	for i, r := range s {
		w := utf16.RuneLen(r)
		if w < 0 {
			w = 1 // invalid runes become U+FFFD, one unit
		}
		if used+w > n {
			return s[:i]
		}
		used += w
	}
	return s
}

// maxBalloons is how many balloons may wait to be shown.
const maxBalloons = 5

type balloon struct{ title, text string }

// queueBalloon adds a balloon to q. The shell shows no balloon without text,
// so a lone title becomes the text; an empty balloon is not added. When q is
// full the oldest one is dropped.
func queueBalloon(q []balloon, title, text string) []balloon {
	if text == "" {
		title, text = "", title
	}
	if text == "" {
		return q
	}
	q = append(q, balloon{title, text})
	if len(q) > maxBalloons {
		q = append(q[:0:0], q[len(q)-maxBalloons:]...)
	}
	return q
}

// menuID is the command id of item i; 0 is reserved for "nothing chosen".
func menuID(i int) uintptr { return uintptr(i) + 1 }

// itemIndex maps a command id back to the index of a clickable item.
func itemIndex(items []Item, id uintptr) (int, bool) {
	if id == 0 || id > uintptr(len(items)) {
		return 0, false
	}
	i := int(id - 1)
	if items[i].Separator {
		return 0, false
	}
	return i, true
}

// pickICO returns the image data (a DIB or a PNG) of the entry of an ICO file
// that suits an icon of want pixels: the smallest entry at least that large,
// else the largest one. size is the width of the entry picked.
func pickICO(ico []byte, want int) (data []byte, size int, err error) {
	le := binary.LittleEndian
	if len(ico) < 6 || le.Uint16(ico[0:]) != 0 || le.Uint16(ico[2:]) != 1 {
		return nil, 0, errors.New("tray: not an ICO file")
	}
	n := int(le.Uint16(ico[4:]))
	if n == 0 {
		return nil, 0, errors.New("tray: ICO file without images")
	}
	if len(ico) < 6+16*n {
		return nil, 0, errors.New("tray: ICO directory cut short")
	}
	best, bestSize := -1, 0
	for i := 0; i < n; i++ {
		s := int(ico[6+16*i])
		if s == 0 {
			s = 256
		}
		switch {
		case best < 0,
			s >= want && (bestSize < want || s < bestSize),
			s < want && bestSize < want && s > bestSize:
			best, bestSize = i, s
		}
	}
	e := ico[6+16*best:]
	length, off := uint64(le.Uint32(e[8:])), uint64(le.Uint32(e[12:]))
	if length == 0 || off+length > uint64(len(ico)) {
		return nil, 0, fmt.Errorf("tray: ICO entry of %d pixels lies outside the file", bestSize)
	}
	return ico[off : off+length], bestSize, nil
}
