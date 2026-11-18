// SPDX-License-Identifier: GPL-2.0-or-later

package tray

import (
	"bytes"
	"encoding/binary"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Bananenbaas/kb7-commander/screen/internal/icon"
)

func units(s string) int { return len(utf16.Encode([]rune(s))) }

func TestCutTipShortUnchanged(t *testing.T) {
	for _, s := range []string{"", "Commander", strings.Repeat("a", 127)} {
		if got := cutTip(s); got != s {
			t.Errorf("cutTip(%q) = %q, want unchanged", s, got)
		}
	}
}

func TestCutTipAt127Units(t *testing.T) {
	got := cutTip(strings.Repeat("a", 200))
	if units(got) != 127 {
		t.Fatalf("cut to %d units, want 127", units(got))
	}
}

func TestCutTipKeepsSurrogatePairWhole(t *testing.T) {
	// 126 units, then a character that needs two UTF-16 units: it does not
	// fit and must not be split.
	s := strings.Repeat("a", 126) + "\U0001F680" + "b"
	got := cutTip(s)
	if got != strings.Repeat("a", 126) {
		t.Fatalf("cutTip = %q (%d units), want the 126 a's", got, units(got))
	}
	// 125 units: the pair fits exactly.
	s = strings.Repeat("a", 125) + "\U0001F680" + "b"
	if got := cutTip(s); got != strings.Repeat("a", 125)+"\U0001F680" {
		t.Fatalf("cutTip = %q, want the pair kept", got)
	}
}

func TestCutTipMultiByteSingleUnit(t *testing.T) {
	// 'é' is two bytes in UTF-8 but one UTF-16 unit: it fits at unit 127.
	s := strings.Repeat("a", 126) + "éé"
	if got := cutTip(s); got != strings.Repeat("a", 126)+"é" {
		t.Fatalf("cutTip = %q, want 126 a's and one é", got)
	}
}

func TestCutUnitsBalloonLimits(t *testing.T) {
	if got := cutUnits(strings.Repeat("x", 100), maxTitle); units(got) != 63 {
		t.Errorf("title cut to %d units, want 63", units(got))
	}
	if got := cutUnits(strings.Repeat("x", 400), maxInfo); units(got) != 255 {
		t.Errorf("text cut to %d units, want 255", units(got))
	}
}

func TestQueueBalloon(t *testing.T) {
	q := queueBalloon(nil, "", "", nil)
	if len(q) != 0 {
		t.Fatalf("empty balloon queued: %v", q)
	}
	q = queueBalloon(q, "Only a title", "", nil)
	if len(q) != 1 || q[0].text != "Only a title" || q[0].title != "" {
		t.Fatalf("lone title not moved to the text: %+v", q)
	}
	for i := 0; i < 7; i++ {
		q = queueBalloon(q, "t", string(rune('a'+i)), nil)
	}
	if len(q) != maxBalloons {
		t.Fatalf("%d queued, want %d", len(q), maxBalloons)
	}
	if q[0].text != "c" || q[maxBalloons-1].text != "g" {
		t.Fatalf("oldest not dropped: first %q, last %q", q[0].text, q[maxBalloons-1].text)
	}
}

func TestQueueBalloonKeepsActions(t *testing.T) {
	var ran []string
	var q []balloon
	for i := 0; i < 7; i++ {
		name := string(rune('a' + i))
		var action func()
		if i%2 == 0 {
			action = func() { ran = append(ran, name) }
		}
		q = queueBalloon(q, "t", name, action)
	}
	if len(q) != maxBalloons {
		t.Fatalf("%d queued, want %d", len(q), maxBalloons)
	}
	for _, b := range q {
		if b.action != nil {
			b.action()
		}
	}
	// c..g are left; c, e and g have actions.
	if got := strings.Join(ran, ""); got != "ceg" {
		t.Fatalf("actions of the queued balloons ran %q, want %q", got, "ceg")
	}
	if q := queueBalloon(nil, "", "", func() {}); len(q) != 0 {
		t.Fatalf("empty balloon with an action queued: %v", q)
	}
}

func TestBalloonClicks(t *testing.T) {
	var c balloonClicks
	if c.click() != nil {
		t.Fatal("click before any balloon returned an action")
	}
	n := 0
	c.shown(func() { n++ })
	f := c.click()
	if f == nil {
		t.Fatal("click on a balloon with an action returned nothing")
	}
	f()
	if n != 1 {
		t.Fatalf("action ran %d times, want 1", n)
	}
	if c.click() != nil {
		t.Fatal("a second click ran the action again")
	}

	// Timeout or hide forgets the action.
	c.shown(func() { n++ })
	c.forget()
	if c.click() != nil {
		t.Fatal("click after the balloon went away returned an action")
	}

	// Only the balloon shown last counts: one without action replaces it.
	c.shown(func() { n++ })
	c.shown(nil)
	if c.click() != nil {
		t.Fatal("click on a balloon without action ran the earlier one's action")
	}

	// A later balloon with an action replaces an earlier one's.
	first, second := 0, 0
	c.shown(func() { first++ })
	c.shown(func() { second++ })
	if f := c.click(); f == nil {
		t.Fatal("no action for the last balloon")
	} else {
		f()
	}
	if first != 0 || second != 1 {
		t.Fatalf("first ran %d, second %d; want 0 and 1", first, second)
	}
}

func TestMenuEntries(t *testing.T) {
	show := false
	label := "Update to v1.2.0"
	items := []Item{
		{Label: "Reload", Checked: func() bool { return true }},
		{Separator: true},
		{Label: "static", LabelFunc: func() string { return label }, Visible: func() bool { return show }},
		{Separator: true},
		{Label: "Shown", Visible: func() bool { return true }},
		{Label: "Quit"},
	}
	want := []entry{
		{ID: menuID(0), Label: "Reload", Checked: true},
		{Separator: true},
		{Separator: true},
		{ID: menuID(4), Label: "Shown"},
		{ID: menuID(5), Label: "Quit"},
	}
	if got := menuEntries(items); !slices.Equal(got, want) {
		t.Fatalf("item hidden: got %+v, want %+v", got, want)
	}

	show = true
	want = []entry{
		{ID: menuID(0), Label: "Reload", Checked: true},
		{Separator: true},
		{ID: menuID(2), Label: "Update to v1.2.0"},
		{Separator: true},
		{ID: menuID(4), Label: "Shown"},
		{ID: menuID(5), Label: "Quit"},
	}
	if got := menuEntries(items); !slices.Equal(got, want) {
		t.Fatalf("item shown: got %+v, want %+v", got, want)
	}

	// LabelFunc is asked each time the menu opens.
	label = "Update to v1.3.0"
	if got := menuEntries(items)[2].Label; got != label {
		t.Fatalf("label %q, want %q", got, label)
	}
	// Every id leads back to its own item, whatever is hidden.
	show = false
	for _, e := range menuEntries(items) {
		if e.Separator {
			continue
		}
		i, ok := itemIndex(items, e.ID)
		if !ok || items[i].Label != e.Label {
			t.Fatalf("id %d of %q maps to item %d (%v)", e.ID, e.Label, i, ok)
		}
	}
}

func TestMenuIDMapping(t *testing.T) {
	items := []Item{{Label: "Reload"}, {Separator: true}, {Label: "Quit"}}
	for i := range items {
		id := menuID(i)
		if id == 0 {
			t.Fatalf("menuID(%d) = 0; 0 means no choice", i)
		}
		got, ok := itemIndex(items, id)
		if i == 1 {
			if ok {
				t.Errorf("separator id %d maps to an item", id)
			}
			continue
		}
		if !ok || got != i {
			t.Errorf("itemIndex(menuID(%d)) = %d, %v", i, got, ok)
		}
	}
	for _, id := range []uintptr{0, menuID(3), menuID(100)} {
		if _, ok := itemIndex(items, id); ok {
			t.Errorf("id %d maps to an item", id)
		}
	}
}

func TestPickICOEntry(t *testing.T) {
	ico, err := icon.ICO(icon.Sizes, false)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ want, size int }{
		{16, 16}, {20, 20}, {24, 24}, {32, 32}, {48, 48}, {256, 256},
		{1, 16}, {17, 20}, {40, 48}, {64, 256}, {300, 256},
	}
	for _, c := range cases {
		data, size, err := pickICO(ico, c.want)
		if err != nil {
			t.Fatalf("pickICO(%d): %v", c.want, err)
		}
		if size != c.size {
			t.Errorf("pickICO(%d) picked %d, want %d", c.want, size, c.size)
			continue
		}
		if size == 256 {
			if !bytes.HasPrefix(data, []byte("\x89PNG")) {
				t.Errorf("256 entry is not PNG")
			}
			continue
		}
		if w := binary.LittleEndian.Uint32(data[4:]); int(w) != size {
			t.Errorf("pickICO(%d): DIB width %d, want %d", c.want, w, size)
		}
	}
}

func TestPickICORejectsBadData(t *testing.T) {
	good, err := icon.ICO([]int{16}, false)
	if err != nil {
		t.Fatal(err)
	}
	bad := [][]byte{
		nil,
		{0, 0, 1, 0},
		{0, 0, 2, 0, 1, 0}, // a cursor file, not an icon
		{0, 0, 1, 0, 0, 0}, // no entries
		good[:6+16],        // entry points past the end
	}
	for i, b := range bad {
		if _, _, err := pickICO(b, 16); err == nil {
			t.Errorf("case %d: no error", i)
		}
	}
}
