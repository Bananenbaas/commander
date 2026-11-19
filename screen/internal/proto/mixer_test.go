// SPDX-License-Identifier: GPL-2.0-or-later

package proto

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

// capture45 is the report 0x28 Swarm II wrote in 45-mixer-new-app-1.
const capture45 = "" +
	"28340105000053797374656d566f6c756d6500000000000000000000000000000000000000000000000000" +
	"00000000640147726f76657250726f00000000000000000000000000000000000000000000000000000000" +
	"000000006402537465616d0000000000000000000000000000000000000000000000000000000000000000" +
	"0000000064034f7065726120475820496e7465726e65742042726f77736572000000000000000000000000" +
	"0000000064044f7065726120475820496e7465726e65742042726f77736572000000000000000000000000" +
	"00000000640000000000000000000000000000000000000000000000000000000000000000000000000000" +
	"00000000000000000000000000000000000000000000000000000000000000000000000000000000000000" +
	"0000000000431f"

func TestMixerListCapture(t *testing.T) {
	want, err := hex.DecodeString(capture45)
	if err != nil || len(want) != 308 {
		t.Fatalf("capture: %d bytes, %v", len(want), err)
	}
	rows := []MixerRow{
		{"SystemVolume"}, {"GroverPro"}, {"Steam"},
		{"Opera GX Internet Browser"}, {"Opera GX Internet Browser"},
	}
	got, err := MixerList(rows)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got\n% x\nwant\n% x", got, want)
	}

	six := append(append([]MixerRow(nil), rows...), MixerRow{"Spotify"})
	if _, err := MixerList(six); err == nil {
		t.Error("six rows: no error")
	}
	if _, err := MixerList(nil); err == nil {
		t.Error("no rows: no error")
	}
	if _, err := MixerList([]MixerRow{{"Steam"}}); err == nil {
		t.Error("row 0 not SystemVolume: no error")
	}
}

func TestMixerListNames(t *testing.T) {
	long := strings.Repeat("abcdefghij", 5)
	got, err := MixerList([]MixerRow{{"SystemVolume"}, {long}, {"Spotify · Premium"}, {"Café\x01"}})
	if err != nil {
		t.Fatal(err)
	}
	row := func(i int) []byte { return got[5+43*i : 5+43*(i+1)] }
	if r := row(1); r[0] != 1 || string(r[1:41]) != long[:40] || r[41] != 0 || r[42] != 100 {
		t.Errorf("long name row: % x", r)
	}
	if r := row(2); r[0] != 2 || string(bytes.TrimRight(r[1:42], "\x00")) != "Spotify ? Premium" || r[42] != 100 {
		t.Errorf("non-ASCII row: % x", r)
	}
	if r := row(3); string(bytes.TrimRight(r[1:42], "\x00")) != "Caf??" || r[42] != 100 {
		t.Errorf("accent row: % x", r)
	}
	if got[3] != 4 {
		t.Errorf("row count %d, want 4", got[3])
	}
	s := Sum16(got[:306])
	if got[306] != byte(s) || got[307] != byte(s>>8) {
		t.Error("bad sum")
	}
}

func TestLevelFrames(t *testing.T) {
	f := SystemLevelFrame(83, false)
	if want := []byte{0xa3, 0, 1, 0, 0, 0x0d, 0x53, 0, 1, 0, 0x53, 0}; !bytes.Equal(f[:12], want) || !allZero(f[12:]) {
		t.Errorf("system 83: % x", f)
	}
	f = SystemLevelFrame(100, true)
	if f[6] != 100 || f[8] != 0 || f[10] != 100 {
		t.Errorf("system muted: % x", f[:12])
	}
	if f = SystemLevelFrame(150, false); f[6] != 100 || f[10] != 100 {
		t.Errorf("system 150: % x", f[:12])
	}
	fs := ProgramLevelFrames([]int{100, 42, 100, 100})
	if len(fs) != 2 {
		t.Fatalf("%d frames, want 2", len(fs))
	}
	if want := []byte{0xa3, 0, 1, 0, 3, 0x0e, 0, 0x64, 0x64, 0, 0x2a, 0x2a}; !bytes.Equal(fs[0][:12], want) || !allZero(fs[0][12:]) {
		t.Errorf("programs 1-2: % x", fs[0])
	}
	if want := []byte{0xa3, 0, 1, 0, 6, 0x0e, 0, 0x64, 0x64, 0, 0x64, 0x64}; !bytes.Equal(fs[1][:12], want) || !allZero(fs[1][12:]) {
		t.Errorf("programs 3-4: % x", fs[1])
	}
	fs = ProgramLevelFrames([]int{30})
	if want := []byte{0xa3, 0, 1, 0, 3, 0x0e, 0, 30, 30, 0, 0, 0}; !bytes.Equal(fs[0][:12], want) {
		t.Errorf("one program: % x", fs[0][:12])
	}
	if want := []byte{0xa3, 0, 1, 0, 6, 0x0e, 0, 0, 0, 0, 0, 0}; !bytes.Equal(fs[1][:12], want) {
		t.Errorf("one program, 3-4: % x", fs[1][:12])
	}
}

func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestSlides(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want any
	}{
		{"system", []byte{0x33, 0, 0, 6, 0, 0, 3, 0x5f}, SystemSlide{95}},
		{"program 2", []byte{0x33, 0, 3, 7, 0, 2, 3, 0x5c}, ProgramSlide{Row: 2, Level: 92}},
		{"program 1, 12 bytes", []byte{0x33, 0, 3, 7, 0, 1, 3, 0x2a, 0, 0, 0, 0}, ProgramSlide{Row: 1, Level: 42}},
		{"page tap", []byte{0x33, 1, 3, 3, 0, 0, 1, 1}, Touch{Page: 1, Position: 3, Kind: 3, Down: true}},
		{"kind 06 on page 1", []byte{0x33, 1, 0, 6, 0, 0, 1, 1}, Touch{Page: 1, Position: 0, Kind: 6, Down: true}},
	}
	for _, c := range cases {
		got, ok := ParseReport(c.in)
		if !ok || got != c.want {
			t.Errorf("%s: got (%v, %v), want %v", c.name, got, ok, c.want)
		}
	}
}
