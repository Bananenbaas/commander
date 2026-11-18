// SPDX-License-Identifier: GPL-2.0-or-later

package proto

import (
	"bytes"
	"testing"
)

func TestUploadFramesLayout(t *testing.T) {
	data := make([]byte, PictureSize)
	for i := range data {
		data[i] = byte(i*7 + 1)
	}
	frames, err := UploadFrames(1, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1065 {
		t.Fatalf("frames = %d, want 1065", len(frames))
	}
	var opener [64]byte
	opener[0], opener[1] = 0xa5, 0x41
	if frames[0].Out != opener || frames[0].Ack != opener {
		t.Errorf("opener = %x / %x", frames[0].Out, frames[0].Ack)
	}
	last := frames[len(frames)-1]
	var end [64]byte
	end[0], end[1] = 0xa5, 0xff
	if last.Out != end || last.Ack != end {
		t.Errorf("closer = %x / %x", last.Out, last.Ack)
	}

	var ff [64]byte
	for i := range ff {
		ff[i] = 0xff
	}
	counts := map[byte]int{}
	var payload []byte
	for _, f := range frames[1 : len(frames)-1] {
		if f.Out[0] != 0xa5 {
			t.Fatalf("frame without a5: %x", f.Out)
		}
		n := f.Out[1]
		counts[n]++
		switch n {
		case 0xf1, 0xf2:
			if f.Ack != f.Out {
				t.Errorf("marker not echoed: %x", f.Out)
			}
		default:
			if f.Ack != ff {
				t.Errorf("data ack = %x", f.Ack)
			}
			payload = append(payload, f.Out[2:2+int(n)]...)
			for _, b := range f.Out[2+int(n):] {
				if b != 0 {
					t.Fatal("padding not zero")
				}
			}
		}
	}
	want := map[byte]int{0xf1: 16, 0xf2: 16, 0x3e: 1015, 0x04: 15, 0x3a: 1}
	for k, v := range want {
		if counts[k] != v {
			t.Errorf("count[%#x] = %d, want %d", k, counts[k], v)
		}
	}
	if len(counts) != len(want) {
		t.Errorf("unexpected frame kinds: %v", counts)
	}
	if !bytes.Equal(payload, data) {
		t.Error("payload differs from input")
	}
}

func TestUploadFramesErrors(t *testing.T) {
	ok := make([]byte, PictureSize)
	for _, slot := range []byte{0, 64, 255} {
		if _, err := UploadFrames(slot, ok); err == nil {
			t.Errorf("slot %d accepted", slot)
		}
	}
	if _, err := UploadFrames(63, ok); err != nil {
		t.Errorf("slot 63: %v", err)
	}
	for _, n := range []int{0, PictureSize - 1, PictureSize + 1} {
		if _, err := UploadFrames(1, make([]byte, n)); err == nil {
			t.Errorf("length %d accepted", n)
		}
	}
}

// The frame count of a background upload is pinned by the capture
// 06-screen-background-quadrants-1: 14,006 frames for background and
// thumbnail together.
func TestBackgroundFrames(t *testing.T) {
	full := make([]byte, BackgroundSize)
	thumb := make([]byte, PictureSize)
	frames, err := BackgroundFrames(full, thumb)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 14006 {
		t.Errorf("%d frames, want 14006", len(frames))
	}
	if frames[0].Out[0] != 0xa5 || frames[0].Out[1] != 0x00 {
		t.Errorf("first opener % x, want a5 00", frames[0].Out[:2])
	}
	n := 0
	for i, f := range frames {
		if i > 0 && f.Out[0] == 0xa5 && f.Out[1] == 0x40 {
			n = i
		}
	}
	if n != 12941 {
		t.Errorf("thumbnail opener at frame %d, want 12941", n)
	}
	if _, err := BackgroundFrames(full[:10], thumb); err == nil {
		t.Error("short background accepted")
	}
	if _, err := BackgroundFrames(full, thumb[:10]); err == nil {
		t.Error("short thumbnail accepted")
	}
}
