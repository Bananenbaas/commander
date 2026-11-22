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
	// LabelFunc, when set, is asked each time the menu opens and used
	// instead of Label.
	LabelFunc func() string
	// Visible, when set, is asked each time the menu opens; false leaves the
	// item out of the menu that time. Separators are always shown.
	Visible func() bool
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
	// OnClick, when set, runs on a goroutine of its own when the icon is
	// clicked with the left button, instead of opening the menu. The right
	// click still opens the menu. Unset, both open the menu.
	OnClick func()
}

// Notification events of the tray icon.
const (
	wmContextMenu = 0x007B
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	ninSelect     = 0x0400 + 0
	ninKeySelect  = 0x0400 + 1
)

type action int

const (
	actionNone  action = iota // ignore the event
	actionMenu                // open the menu
	actionClick               // run Options.OnClick
)

// clickAction says what a notification event of the icon asks for. With
// version 4 of the notification protocol (v4) the left click arrives as
// NIN_SELECT or NIN_KEYSELECT and the right click as WM_CONTEXTMENU; before
// it as WM_LBUTTONUP and WM_RBUTTONUP.
func clickAction(v4 bool, event uintptr, hasOnClick bool) action {
	left, right := event == wmLButtonUp, event == wmRButtonUp
	if v4 {
		left = event == ninSelect || event == ninKeySelect
		right = event == wmContextMenu
	}
	switch {
	case left && hasOnClick:
		return actionClick
	case left || right:
		return actionMenu
	}
	return actionNone
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

type balloon struct {
	title, text string
	action      func() // run when the balloon is clicked; may be nil
}

// queueBalloon adds a balloon to q. The shell shows no balloon without text,
// so a lone title becomes the text; an empty balloon is not added. When q is
// full the oldest one is dropped.
func queueBalloon(q []balloon, title, text string, action func()) []balloon {
	if text == "" {
		title, text = "", title
	}
	if text == "" {
		return q
	}
	q = append(q, balloon{title, text, action})
	if len(q) > maxBalloons {
		q = append(q[:0:0], q[len(q)-maxBalloons:]...)
	}
	return q
}

// balloonClicks remembers the action of the balloon shown last. A click
// counts only for that balloon, and only while it is there.
type balloonClicks struct{ action func() }

// shown records that a balloon with action (nil for none) was shown; it
// replaces whatever was shown before.
func (c *balloonClicks) shown(action func()) { c.action = action }

// forget is called when the balloon timed out or was hidden.
func (c *balloonClicks) forget() { c.action = nil }

// click returns the action to run for a click on the balloon, or nil. The
// action is returned once.
func (c *balloonClicks) click() func() {
	f := c.action
	c.action = nil
	return f
}

// entry is one line of the menu as it is shown.
type entry struct {
	ID        uintptr // menuID of the item; 0 for a separator
	Label     string
	Checked   bool
	Separator bool
}

// menuEntries builds the menu as it is to be shown now: labels from
// LabelFunc, ticks from Checked, hidden items left out. Each id stays tied
// to the item's index, so hiding one does not change what another runs.
func menuEntries(items []Item) []entry {
	es := make([]entry, 0, len(items))
	for i, it := range items {
		if it.Separator {
			es = append(es, entry{Separator: true})
			continue
		}
		if it.Visible != nil && !it.Visible() {
			continue
		}
		label := it.Label
		if it.LabelFunc != nil {
			label = it.LabelFunc()
		}
		es = append(es, entry{
			ID:      menuID(i),
			Label:   label,
			Checked: it.Checked != nil && it.Checked(),
		})
	}
	return es
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
