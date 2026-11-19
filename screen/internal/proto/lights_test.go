// SPDX-License-Identifier: GPL-2.0-or-later

package proto

import (
	"bytes"
	"testing"
)

func TestSetLEDEscape(t *testing.T) {
	var p LightPayload
	SetLED(&p, 0, 0x10, 0x40, 0xc0)
	if p[0] != 0x10 || p[16] != 0x40 || p[32] != 0xc0 {
		t.Errorf("LED 0: % x % x % x", p[0], p[16], p[32])
	}
	SetLED(&p, 17, 1, 2, 3)
	if p[49] != 1 || p[65] != 2 || p[81] != 3 {
		t.Errorf("LED 17: % x % x % x", p[49], p[65], p[81])
	}
}

func TestLightFrames(t *testing.T) {
	var p LightPayload
	for i := range 112 {
		SetLED(&p, i, 255, 255, 255)
	}
	f := LightFrames(&p)
	var joined []byte
	for n := range f {
		if f[n][0] != 0xa1 || f[n][1] != byte(n+1) {
			t.Errorf("bank %d header % x", n, f[n][:2])
		}
		want := [2]byte{}
		if n == 0 {
			want = [2]byte{0x68, 0x01}
		}
		if [2]byte(f[n][2:4]) != want {
			t.Errorf("bank %d length % x", n, f[n][2:4])
		}
		joined = append(joined, f[n][4:]...)
	}
	if !bytes.Equal(joined, p[:]) {
		t.Error("frames do not give the payload back")
	}
	for i := 336; i < 360; i++ {
		if p[i] != 0 {
			t.Fatalf("byte %d is %x, want 0", i, p[i])
		}
	}
}

func TestIsLightAck(t *testing.T) {
	var fr [64]byte
	fr[0], fr[1] = 0x32, 3
	if !IsLightAck(fr, 3) || IsLightAck(fr, 2) {
		t.Error("IsLightAck")
	}
	fr[0] = 0xa5
	if IsLightAck(fr, 3) {
		t.Error("a5 is not a light ack")
	}
}

func TestLights11(t *testing.T) {
	var want LightPayload
	SetLED(&want, 0, 0x10, 0x40, 0xc0)
	SetLED(&want, 17, 1, 2, 3)
	SetLED(&want, 111, 0xff, 0xfe, 0xfd) // the last LED, in the last block
	b := make([]byte, Len11)
	copy(b, []byte{0x11, 0x5c, 0x01, 0x00, 0x01, 0x06, 0x63, 0x0b, 0x01, 0x00})
	copy(b[10:346], want[:336])
	s := Sum16(b[:346])
	b[346], b[347] = byte(s), byte(s>>8)

	p, level, err := Lights11(b)
	if err != nil || level != 99 {
		t.Fatalf("got level %d, %v", level, err)
	}
	if *p != want {
		t.Error("colours differ from the table")
	}
	if got := [3]byte{p[17/16*48+17%16], p[17/16*48+17%16+16], p[17/16*48+17%16+32]}; got != [3]byte{1, 2, 3} {
		t.Errorf("LED 17 is % x", got)
	}

	b[347]++
	if _, _, err := Lights11(b); err == nil {
		t.Error("wrong sum accepted")
	}
	b[347]--
	b[0] = 0x12
	if _, _, err := Lights11(b); err == nil {
		t.Error("wrong report id accepted")
	}
	if _, _, err := Lights11(b[:100]); err == nil {
		t.Error("short report accepted")
	}
}

// blueBlock is a 0x11 block like the ones in 46-light-save-static-blue-1:
// profile 3, brightness 99, every LED 28, 54, 255.
func blueBlock() []byte {
	b := make([]byte, Len11)
	copy(b, []byte{0x11, 0x5c, 0x01, 0x03, 0x01, 0x06, 0x63, 0x0b, 0x01, 0x00})
	for led := range 112 {
		base := 10 + led/16*48 + led%16
		b[base], b[base+16], b[base+32] = 0x1c, 0x36, 0xff
	}
	s := Sum16(b[:Len11-2])
	b[Len11-2], b[Len11-1] = byte(s), byte(s>>8)
	return b
}

// differing lists the offsets where a and b differ.
func differing(a, b []byte) []int {
	var out []int
	for i := range a {
		if a[i] != b[i] {
			out = append(out, i)
		}
	}
	return out
}

func TestTable11RoundTrip(t *testing.T) {
	orig := blueBlock()
	tb, err := ParseTable11(orig)
	if err != nil {
		t.Fatal(err)
	}
	if tb.Profile() != 3 || tb.Brightness() != 99 {
		t.Fatalf("profile %d brightness %d", tb.Profile(), tb.Brightness())
	}
	if !bytes.Equal(tb.Bytes(), orig) {
		t.Fatal("Bytes does not give the block back")
	}
	c := tb.Colours()
	if c[0] != 0x1c || c[16] != 0x36 || c[32] != 0xff || c[6*48+15+32] != 0xff {
		t.Errorf("colours % x", c[:48])
	}
	for i := 336; i < LightPayloadLen; i++ {
		if c[i] != 0 {
			t.Fatalf("payload byte %d is %x, want 0", i, c[i])
		}
	}

	if err := tb.SetBrightness(42); err != nil {
		t.Fatal(err)
	}
	got := tb.Bytes()
	for _, i := range differing(orig, got) {
		if i != 6 && i != 346 && i != 347 {
			t.Errorf("SetBrightness changed byte %d", i)
		}
	}
	if got[6] != 42 || tb.Brightness() != 42 {
		t.Errorf("brightness byte %d", got[6])
	}
	if _, err := ParseTable11(got); err != nil {
		t.Errorf("sum after SetBrightness: %v", err)
	}
	if err := tb.SetBrightness(101); err == nil {
		t.Error("brightness 101 accepted")
	}
	if err := tb.SetBrightness(-1); err == nil {
		t.Error("brightness -1 accepted")
	}

	before := tb.Bytes()
	p := tb.Colours()
	SetLED(p, 0, 0x10, 0x40, 0xc0)
	SetLED(p, 111, 1, 2, 3)
	tb.SetColours(p)
	after := tb.Bytes()
	for _, i := range differing(before, after) {
		if i < 10 || i > 347 {
			t.Errorf("SetColours changed byte %d", i)
		}
	}
	if after[10] != 0x10 || after[26] != 0x40 || after[42] != 0xc0 {
		t.Errorf("LED 0 % x", []byte{after[10], after[26], after[42]})
	}
	if *tb.Colours() != *p {
		t.Error("Colours differ from what was set")
	}
	if _, err := ParseTable11(after); err != nil {
		t.Errorf("sum after SetColours: %v", err)
	}
	// The table keeps its own copy.
	after[7] = 0x99
	if tb.Bytes()[7] != 0x0b {
		t.Error("Bytes shares memory with the table")
	}
}

func TestParseTable11Refuses(t *testing.T) {
	b := blueBlock()
	b[347]++
	if _, err := ParseTable11(b); err == nil {
		t.Error("wrong sum accepted")
	}
	b = blueBlock()
	b[0] = 0x12
	if _, err := ParseTable11(b); err == nil {
		t.Error("wrong report id accepted")
	}
	if _, err := ParseTable11(blueBlock()[:100]); err == nil {
		t.Error("short report accepted")
	}
}
