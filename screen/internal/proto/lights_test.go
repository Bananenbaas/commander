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
