// SPDX-License-Identifier: GPL-2.0-or-later

package picture

import (
	"image"
	"image/color"
	"testing"
)

func solidImg(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func TestBackgroundSolidRed(t *testing.T) {
	full, thumb, err := Background(solidImg(100, 50, color.NRGBA{255, 0, 0, 255}))
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != BW*BH*2 || len(thumb) != Size {
		t.Fatalf("sizes %d, %d", len(full), len(thumb))
	}
	// Red is 0xF800, little-endian: 00 f8 (as in the capture
	// 06-screen-background-ff0000-1). The image is wider than the screen,
	// so it covers the screen fully.
	for _, i := range []int{0, len(full) / 2, len(full) - 2} {
		if full[i] != 0x00 || full[i+1] != 0xf8 {
			t.Fatalf("pixel at byte %d is % x, want 00 f8", i, full[i:i+2])
		}
	}
	for i := W * H * 2; i < Size; i++ {
		if thumb[i] != 0xff {
			t.Fatalf("thumbnail alpha byte %d is %02x, want ff", i-W*H*2, thumb[i])
		}
	}
}

func TestBackgroundTopToBottom(t *testing.T) {
	img := solidImg(480, 800, color.NRGBA{0, 0, 255, 255})
	for y := 0; y < 400; y++ {
		for x := 0; x < 480; x++ {
			img.Set(x, y, color.NRGBA{0, 255, 0, 255})
		}
	}
	full, _, err := Background(img)
	if err != nil {
		t.Fatal(err)
	}
	// Green (0x07E0 -> e0 07) in the first row, blue (0x001F -> 1f 00) in
	// the last.
	if full[0] != 0xe0 || full[1] != 0x07 {
		t.Errorf("top pixel % x, want e0 07", full[:2])
	}
	if n := len(full); full[n-2] != 0x1f || full[n-1] != 0x00 {
		t.Errorf("bottom pixel % x, want 1f 00", full[n-2:])
	}
}

func TestCoverRect(t *testing.T) {
	if r := coverRect(100, 50, 480, 800); r.Dy() != 800 || r.Dx() < 480 {
		t.Errorf("wide image: %v", r)
	}
	if r := coverRect(50, 400, 480, 800); r.Dx() != 480 || r.Dy() < 800 {
		t.Errorf("tall image: %v", r)
	}
}
