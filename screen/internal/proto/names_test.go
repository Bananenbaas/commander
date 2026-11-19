// SPDX-License-Identifier: GPL-2.0-or-later

package proto

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// rename1 is capture 43-rename-profile-1: slot 2 renamed to TEST. Slots 3 and
// 4 keep bytes after their NUL.
const rename1 = "16 68 44 45 53 4b 54 4f 50 00 00 00 00 00 00 00 00 00 00 00 00 00" +
	" 53 43 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00" +
	" 54 45 53 54 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00 00" +
	" 44 45 46 41 55 4c 54 20 30 34 00 04 00 00 05 00 00 00 00 00" +
	" 44 45 46 41 55 4c 54 20 30 35 00 23 01 10 01 00 00 00 18 13 ea 09"

func hexBytes(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// rename2 is capture 43-rename-profile-2: slot 2 back to DEFAULT 03.
func rename2(t *testing.T) []byte {
	b := hexBytes(t, rename1)
	copy(b[42:62], "DEFAULT 03")
	b[102], b[103] = 0x32, 0x0b
	return b
}

func TestTable16Capture(t *testing.T) {
	c1 := hexBytes(t, rename1)
	if len(c1) != Len16 {
		t.Fatalf("capture is %d bytes", len(c1))
	}
	tb, err := ParseTable16(c1)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"DESKTOP", "SC", "TEST", "DEFAULT 04", "DEFAULT 05"}
	for i, w := range want {
		if got := tb.Name(i); got != w {
			t.Errorf("Name(%d) = %q, want %q", i, got, w)
		}
	}
	if !bytes.Equal(tb.Bytes(), c1) {
		t.Errorf("unchanged table: % x", tb.Bytes())
	}
	if err := tb.SetName(2, "DEFAULT 03"); err != nil {
		t.Fatal(err)
	}
	if got, c2 := tb.Bytes(), rename2(t); !bytes.Equal(got, c2) {
		t.Errorf("renamed:\n got % x\nwant % x", got, c2)
	}
	// And forward again: TEST zero-fills the rest of the field.
	if err := tb.SetName(2, "TEST"); err != nil {
		t.Fatal(err)
	}
	if got := tb.Bytes(); !bytes.Equal(got, c1) {
		t.Errorf("renamed back:\n got % x\nwant % x", got, c1)
	}
}

func TestTable16SetNameRefuses(t *testing.T) {
	tb, err := ParseTable16(hexBytes(t, rename1))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ what, name string }{
		{"empty", ""},
		{"20 characters", strings.Repeat("a", 20)},
		{"non-ASCII", "café"},
		{"control byte", "a\x01b"},
		{"DEL", "a\x7f"},
	} {
		if err := tb.SetName(2, c.name); err == nil {
			t.Errorf("%s accepted", c.what)
		}
	}
	if err := tb.SetName(5, "X"); err == nil {
		t.Error("slot 5 accepted")
	}
	if err := tb.SetName(2, strings.Repeat("a", 19)); err != nil {
		t.Errorf("19 characters refused: %v", err)
	}
	if !bytes.Equal(tb.Bytes()[2:22], hexBytes(t, rename1)[2:22]) {
		t.Error("another field changed")
	}
}

func TestParseTable16Refuses(t *testing.T) {
	good := hexBytes(t, rename1)
	for name, mod := range map[string]func([]byte) []byte{
		"short":     func(b []byte) []byte { return b[:103] },
		"long":      func(b []byte) []byte { return append(b, 0) },
		"report id": func(b []byte) []byte { b[0] = 0x17; return b },
		"length":    func(b []byte) []byte { b[1] = 0x67; return b },
		"checksum":  func(b []byte) []byte { b[103]++; return b },
	} {
		if _, err := ParseTable16(mod(bytes.Clone(good))); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
