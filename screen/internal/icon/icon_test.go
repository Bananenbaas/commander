// SPDX-License-Identifier: GPL-2.0-or-later

package icon

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"strings"
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
	obj, err := Syso(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(obj) != 0x8664 {
		t.Errorf("machine %#x, want 0x8664", binary.LittleEndian.Uint16(obj))
	}
	if !bytes.Contains(obj[:200], []byte(".rsrc")) {
		t.Error("no .rsrc section")
	}
	sec := rsrcOf(t, obj)
	if got := dirIDs(t, sec, 0); len(got) != 2 || got[0] != rtIcon || got[1] != rtGroupIcon {
		t.Errorf("resource types %v, want [3 14] without a manifest", got)
	}
}

// rsrcOf returns the raw data of the only section of a COFF object.
func rsrcOf(t *testing.T, obj []byte) []byte {
	t.Helper()
	le := binary.LittleEndian
	if le.Uint16(obj[2:]) != 1 {
		t.Fatalf("%d sections, want 1", le.Uint16(obj[2:]))
	}
	sh := obj[20:]
	size, off := int(le.Uint32(sh[16:])), int(le.Uint32(sh[20:]))
	if off+size > len(obj) {
		t.Fatalf("section %d+%d outside the object of %d", off, size, len(obj))
	}
	return obj[off : off+size]
}

// dirIDs returns the IDs of the entries of the resource directory at off.
func dirIDs(t *testing.T, sec []byte, off int) []uint32 {
	t.Helper()
	le := binary.LittleEndian
	if le.Uint16(sec[off+12:]) != 0 {
		t.Fatalf("directory at %d has named entries", off)
	}
	n := int(le.Uint16(sec[off+14:]))
	ids := make([]uint32, n)
	for i := range ids {
		ids[i] = le.Uint32(sec[off+16+8*i:])
	}
	return ids
}

// resource follows the resource tree of sec to the data of type typ, ID id
// and language lang. Before linking, a data entry's RVA is its offset in the
// section.
func resource(t *testing.T, sec []byte, typ, id, lang uint32) []byte {
	t.Helper()
	le := binary.LittleEndian
	off := 0
	for level, want := range []uint32{typ, id, lang} {
		n := int(le.Uint16(sec[off+14:]))
		next := -1
		for i := 0; i < n; i++ {
			e := sec[off+16+8*i:]
			if le.Uint32(e) == want {
				target := le.Uint32(e[4:])
				if (target&subdirFlag != 0) != (level < 2) {
					t.Fatalf("level %d entry %d: subdirectory flag %#x wrong", level, want, target)
				}
				next = int(target &^ subdirFlag)
			}
		}
		if next < 0 {
			t.Fatalf("no resource %d/%d/%#x (level %d)", typ, id, lang, level)
		}
		off = next
	}
	rva, size := int(le.Uint32(sec[off:])), int(le.Uint32(sec[off+4:]))
	if rva+size > len(sec) {
		t.Fatalf("resource %d/%d data %d+%d outside the section", typ, id, rva, size)
	}
	return sec[rva : rva+size]
}

func TestSysoManifest(t *testing.T) {
	data, err := ICO(Sizes, false)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := Syso(data, []byte(Manifest))
	if err != nil {
		t.Fatal(err)
	}
	sec := rsrcOf(t, obj)
	if got := dirIDs(t, sec, 0); len(got) != 3 || got[0] != rtIcon || got[1] != rtGroupIcon || got[2] != rtManifest {
		t.Fatalf("resource types %v, want [3 14 24]", got)
	}
	if got := resource(t, sec, rtManifest, 1, langUS); string(got) != Manifest {
		t.Errorf("manifest resource differs:\n%s", got)
	}
	icons, err := parseICO(data)
	if err != nil {
		t.Fatal(err)
	}
	for i, ic := range icons {
		if got := resource(t, sec, rtIcon, uint32(i+1), langUS); !bytes.Equal(got, ic.data) {
			t.Errorf("icon %d differs from the ICO entry", i+1)
		}
	}
	grp := resource(t, sec, rtGroupIcon, 1, langUS)
	if len(grp) != 6+14*len(icons) || binary.LittleEndian.Uint16(grp[4:]) != uint16(len(icons)) {
		t.Errorf("group icon of %d bytes for %d icons", len(grp), len(icons))
	}
	// The relocations cover every data entry.
	if n := binary.LittleEndian.Uint16(obj[20+32:]); int(n) != len(icons)+2 {
		t.Errorf("%d relocations, want %d", n, len(icons)+2)
	}
	for _, want := range []string{
		"<dpiAwareness xmlns=\"http://schemas.microsoft.com/SMI/2016/WindowsSettings\">PerMonitorV2</dpiAwareness>",
		"<dpiAware xmlns=\"http://schemas.microsoft.com/SMI/2005/WindowsSettings\">true/pm</dpiAware>",
		"name=\"Microsoft.Windows.Common-Controls\" version=\"6.0.0.0\"",
		"<requestedExecutionLevel level=\"asInvoker\" uiAccess=\"false\"/>",
	} {
		if !strings.Contains(Manifest, want) {
			t.Errorf("manifest lacks %s", want)
		}
	}
}
