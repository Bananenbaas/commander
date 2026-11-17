// SPDX-License-Identifier: GPL-2.0-or-later

package tray

import (
	"bytes"
	"encoding/binary"
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
