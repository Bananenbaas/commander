// SPDX-License-Identifier: GPL-2.0-or-later

package proto

import (
	"bytes"
	"testing"
)

func seal(b []byte) []byte {
	n := len(b) - 2
	s := Sum16(b[:n])
	b[n], b[n+1] = byte(s), byte(s>>8)
	return b
}

func blank29(profile byte) []byte {
	b := make([]byte, 584)
	b[0], b[1], b[2], b[3] = 0x29, 0x48, 0x02, profile
	return seal(b)
}

func blank25(profile byte) []byte {
	b := make([]byte, 164)
	b[0], b[1], b[2] = 0x25, 0xa4, profile
	return seal(b)
}

func TestTable29Parse(t *testing.T) {
	tb, err := ParseTable29(blank29(1))
	if err != nil {
		t.Fatal(err)
	}
	if tb.Profile() != 1 {
		t.Fatalf("profile %d", tb.Profile())
	}
}

func TestTable29Errors(t *testing.T) {
	short := blank29(1)[:583]
	badID := blank29(1)
	badID[0] = 0x2a
	seal(badID)
	badHdr := blank29(1)
	badHdr[1] = 0x49
	seal(badHdr)
	badSum := blank29(1)
	badSum[583] ^= 0xff
	for name, b := range map[string][]byte{"short": short, "id": badID, "hdr": badHdr, "sum": badSum} {
		if _, err := ParseTable29(b); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestTable29SetEntry(t *testing.T) {
	tb, _ := ParseTable29(blank29(2))
	if err := tb.SetEntry(6, AppEntry(3, "slot3")); err != nil {
		t.Fatal(err)
	}
	b := tb.Bytes()
	if !bytes.Equal(b[103:107], []byte{0x00, 0x01, 0x0b, 0x03}) {
		t.Fatalf("% x", b[103:107])
	}
	if string(b[107:112]) != "slot3" || b[112] != 0 {
		t.Fatalf("label bytes % x", b[107:119])
	}
	s := Sum16(b[:582])
	if b[582] != byte(s) || b[583] != byte(s>>8) {
		t.Fatal("checksum")
	}
	if e := tb.Entry(6); e != AppEntry(3, "slot3") {
		t.Fatalf("%+v", e)
	}
	b[0] = 0
	if tb.Bytes()[0] != 0x29 {
		t.Fatal("Bytes is not a copy")
	}
}

func TestTable29Labels(t *testing.T) {
	tb, _ := ParseTable29(blank29(0))
	if err := tb.SetEntry(0, Entry29{Label: "abcdefghijk"}); err != nil {
		t.Fatalf("11 chars: %v", err)
	}
	if tb.Entry(0).Label != "abcdefghijk" {
		t.Fatal("11 chars lost")
	}
	for name, l := range map[string]string{
		"12 chars": "abcdefghijkl", "0x7f": "a\x7f", "control": "a\x1f", "non-ASCII": "é",
	} {
		if err := tb.SetEntry(0, Entry29{Label: l}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := tb.SetEntry(0, Entry29{}); err != nil {
		t.Fatal(err)
	}
	if tb.Entry(0).Label != "" {
		t.Fatal("empty label")
	}
	if tb.SetEntry(35, Entry29{}) == nil || tb.SetEntry(-1, Entry29{}) == nil {
		t.Fatal("out-of-range position accepted")
	}
	if (tb.Entry(35) != Entry29{}) || (tb.Entry(-1) != Entry29{}) {
		t.Fatal("out-of-range Entry not zero")
	}
}

func TestTable29RoundTripKeepsUnknown(t *testing.T) {
	raw := blank29(3)
	raw[4], raw[5], raw[6] = 0xaa, 0xbb, 0xcc
	raw[7+16*34+13] = 0x77
	o := 7 + 16*2
	raw[o], raw[o+1], raw[o+2], raw[o+3] = 1, 2, 3, 4
	copy(raw[o+4:], "hi\x00zz")
	seal(raw)
	tb, err := ParseTable29(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tb.Bytes(), raw) {
		t.Fatal("round trip differs")
	}
	if e := tb.Entry(2); e != (Entry29{1, 2, 3, 4, "hi"}) {
		t.Fatalf("%+v", e)
	}
	// The parsed table must not alias the caller's slice.
	raw[4] = 0
	if tb.Bytes()[4] != 0xaa {
		t.Fatal("table aliases input")
	}
}

func TestTable25(t *testing.T) {
	tb, err := ParseTable25(blank25(4))
	if err != nil {
		t.Fatal(err)
	}
	if tb.Profile() != 4 {
		t.Fatal("profile")
	}
	if err := tb.SetType(6, AppType); err != nil {
		t.Fatal(err)
	}
	b := tb.Bytes()
	if !bytes.Equal(b[34:38], []byte{5, 0, 6, 0}) {
		t.Fatalf("% x", b[34:38])
	}
	s := Sum16(b[:162])
	if b[162] != byte(s) || b[163] != byte(s>>8) {
		t.Fatal("checksum")
	}
	if tb.Type(6) != AppType {
		t.Fatal("Type")
	}
	if err := tb.SetType(37, TypeEntry{1, 2}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tb.Bytes()[158:162], []byte{1, 0, 2, 0}) {
		t.Fatalf("% x", tb.Bytes()[158:162])
	}
	if tb.SetType(38, AppType) == nil || tb.SetType(-1, AppType) == nil {
		t.Fatal("out of range accepted")
	}
	if (tb.Type(38) != TypeEntry{}) || (tb.Type(-1) != TypeEntry{}) {
		t.Fatal("Type out of range not zero")
	}
}

func TestTable25Errors(t *testing.T) {
	badID := blank25(0)
	badID[0] = 0x26
	seal(badID)
	badLen := blank25(0)[:163]
	badSum := blank25(0)
	badSum[162]++
	badHdr := blank25(0)
	badHdr[1] = 0
	seal(badHdr)
	for name, b := range map[string][]byte{"id": badID, "len": badLen, "sum": badSum, "hdr": badHdr} {
		if _, err := ParseTable25(b); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestTable25RoundTrip(t *testing.T) {
	raw := blank25(1)
	raw[3], raw[9], raw[11] = 9, 8, 7
	raw[10+4*5+1] = 0x42
	seal(raw)
	tb, err := ParseTable25(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(tb.Bytes(), raw) {
		t.Fatal("round trip differs")
	}
}

func TestIndexes(t *testing.T) {
	for _, c := range []struct{ page, pos, want int }{{1, 0, 0}, {2, 4, 16}, {3, 10, 34}, {1, 11, 11}} {
		if got, err := Entry29Index(c.page, c.pos); err != nil || got != c.want {
			t.Errorf("Entry29Index(%d, %d) = %d, %v; want %d", c.page, c.pos, got, err, c.want)
		}
	}
	for _, c := range []struct{ page, pos int }{{3, 11}, {4, 0}, {0, 0}, {1, 12}, {1, -1}} {
		if _, err := Entry29Index(c.page, c.pos); err == nil {
			t.Errorf("Entry29Index(%d, %d): no error", c.page, c.pos)
		}
	}
	for _, c := range []struct{ page, pos, want int }{{1, 11, 11}, {2, 0, 13}, {2, 4, 17}, {3, 11, 37}} {
		if got, err := TypeIndex(c.page, c.pos); err != nil || got != c.want {
			t.Errorf("TypeIndex(%d, %d) = %d, %v; want %d", c.page, c.pos, got, err, c.want)
		}
	}
	for _, c := range []struct{ page, pos int }{{4, 0}, {0, 0}, {1, 12}, {2, -1}} {
		if _, err := TypeIndex(c.page, c.pos); err == nil {
			t.Errorf("TypeIndex(%d, %d): no error", c.page, c.pos)
		}
	}
}
