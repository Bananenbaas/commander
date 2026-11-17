// SPDX-License-Identifier: GPL-2.0-or-later

package icon

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

func TestDrawSizes(t *testing.T) {
	for _, s := range Sizes {
		img := Draw(s, false)
		if b := img.Bounds(); b.Dx() != s || b.Dy() != s {
			t.Errorf("size %d: got %v", s, b)
		}
	}
}

func TestDrawColours(t *testing.T) {
	for _, s := range Sizes {
		img := Draw(s, false)
		at := func(fx, fy float64) (r, g, b, a uint8) {
			c := img.NRGBAAt(int(fx*float64(s)), int(fy*float64(s)))
			return c.R, c.G, c.B, c.A
		}
		if _, _, _, a := at(0, 0); a != 0 {
			t.Errorf("size %d: corner alpha %d, want 0", s, a)
		}
		if r, g, b, a := at(0.5, 0.57); !(a == 255 && r > g && g > b) {
			t.Errorf("size %d: visor centre %d,%d,%d,%d not amber", s, r, g, b, a)
		}
		if r, _, b, a := at(0.33, 0.40); !(a == 255 && b > r) {
			t.Errorf("size %d: dome %d,_,%d,%d not blue", s, r, b, a)
		}
	}
}

func TestDrawFailed(t *testing.T) {
	for _, s := range Sizes {
		ok := Draw(s, false)
		bad := Draw(s, true)
		x, y := s*78/100, s*78/100
		c := bad.NRGBAAt(x, y)
		if !(c.R > 200 && c.G < 90 && c.B < 90 && c.A == 255) {
			t.Errorf("size %d: failed dot pixel %v not red", s, c)
		}
		if o := ok.NRGBAAt(x, y); o.R > 200 {
			t.Errorf("size %d: normal icon is red at the dot: %v", s, o)
		}
	}
}

func TestICOHeader(t *testing.T) {
	data, err := ICO(Sizes, false)
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	if le.Uint16(data[0:]) != 0 || le.Uint16(data[2:]) != 1 || le.Uint16(data[4:]) != 6 {
		t.Fatalf("bad ICO header % x", data[:6])
	}
	for i, s := range Sizes {
		e := data[6+16*i:]
		want := byte(s)
		if s == 256 {
			want = 0
		}
		if e[0] != want || e[1] != want {
			t.Errorf("entry %d: size byte %d, want %d", i, e[0], want)
		}
		n, off := int(le.Uint32(e[8:])), int(le.Uint32(e[12:]))
		if off < 6+16*6 || off+n > len(data) {
			t.Errorf("entry %d: range %d+%d outside file of %d", i, off, n, len(data))
			continue
		}
		img := data[off : off+n]
		if s == 256 {
			if _, err := png.Decode(bytes.NewReader(img)); err != nil {
				t.Errorf("256 entry is not a PNG: %v", err)
			}
		} else if le.Uint32(img[0:]) != 40 || int(le.Uint32(img[4:])) != s || int(le.Uint32(img[8:])) != 2*s || le.Uint16(img[14:]) != 32 {
			t.Errorf("entry %d: bad DIB header", i)
		}
	}
}

func TestSyso(t *testing.T) {
	data, err := ICO(Sizes, false)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := Syso(data)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(obj) != 0x8664 {
		t.Errorf("machine %#x, want 0x8664", binary.LittleEndian.Uint16(obj))
	}
	if !bytes.Contains(obj[:200], []byte(".rsrc")) {
		t.Error("no .rsrc section")
	}
}
