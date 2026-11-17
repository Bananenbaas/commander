// SPDX-License-Identifier: GPL-2.0-or-later

package picture

import (
	"errors"
	"image"
	"image/color"
	"image/draw"
	"strings"
	"testing"

	"golang.org/x/image/font"
)

func TestEncodeLayout(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, W, H))
	img.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
	img.SetNRGBA(1, 0, color.NRGBA{255, 255, 255, 128})
	out, err := Encode(img)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 63048 {
		t.Fatalf("len = %d", len(out))
	}
	if out[0] != 0x00 || out[1] != 0xf8 || out[2] != 0xff || out[3] != 0xff {
		t.Errorf("pixels: % x", out[0:4])
	}
	if out[42032] != 0xff || out[42033] != 0x80 {
		t.Errorf("alpha: % x", out[42032:42034])
	}
}

func TestEncodeWrongSize(t *testing.T) {
	if _, err := Encode(image.NewNRGBA(image.Rect(0, 0, 10, 10))); err == nil {
		t.Error("expected error for wrong size")
	}
	if _, err := Encode(image.NewNRGBA(image.Rect(1, 1, 1+W, 1+H))); err == nil {
		t.Error("expected error for non-zero origin")
	}
	if _, err := Encode(nil); err == nil {
		t.Error("expected error for nil")
	}
}

func TestHash(t *testing.T) {
	got := Hash([]byte("abc"))
	want := "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got != want {
		t.Errorf("hash = %s", got)
	}
}

func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(c), image.Point{}, draw.Src)
	return img
}

func TestFitExact(t *testing.T) {
	out := Fit(solid(284, 296, color.NRGBA{10, 20, 30, 255}))
	if out.Bounds() != image.Rect(0, 0, W, H) {
		t.Fatalf("bounds %v", out.Bounds())
	}
	if c := out.NRGBAAt(70, 70); c.A != 255 {
		t.Errorf("centre %v", c)
	}
}

func TestFitLetterbox(t *testing.T) {
	out := Fit(solid(300, 100, color.NRGBA{200, 0, 0, 255}))
	if out.Bounds() != image.Rect(0, 0, W, H) {
		t.Fatalf("bounds %v", out.Bounds())
	}
	if a := out.NRGBAAt(70, 2).A; a != 0 {
		t.Errorf("top padding alpha %d", a)
	}
	if a := out.NRGBAAt(70, H-3).A; a != 0 {
		t.Errorf("bottom padding alpha %d", a)
	}
	if c := out.NRGBAAt(70, H/2); c.A < 250 || c.R < 190 {
		t.Errorf("centre %v", c)
	}
	if a := out.NRGBAAt(1, H/2).A; a < 250 {
		t.Errorf("left edge alpha %d", a)
	}
}

func TestRender(t *testing.T) {
	col := color.NRGBA{0, 200, 80, 255}
	img, err := Render(Style{Color: col, Title: "Power On/Off"})
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, W, H) {
		t.Fatalf("bounds %v", img.Bounds())
	}
	f := img.NRGBAAt(12, 50)
	if f.A < 250 || f.G != 200 {
		t.Errorf("frame pixel %v", f)
	}
	in := img.NRGBAAt(60, 20)
	if d := int(in.A) - 209; d < -3 || d > 3 {
		t.Errorf("inside alpha %d", in.A)
	}
	if in.R > 5 || in.G > 5 || in.B > 5 {
		t.Errorf("inside should be black: %v", in)
	}
	if a := img.NRGBAAt(2, 2).A; a != 0 {
		t.Errorf("outside alpha %d", a)
	}
	// The lower-right corner of the box is cut away.
	if a := img.NRGBAAt(129, 110).A; a != 0 {
		t.Errorf("cut corner alpha %d", a)
	}
	white := 0
	for y := 114; y < 146; y++ {
		for x := 0; x < W; x++ {
			if c := img.NRGBAAt(x, y); c.A > 250 && c.R > 250 && c.G > 250 && c.B > 250 {
				white++
			}
		}
	}
	if white < 50 {
		t.Errorf("only %d white title pixels", white)
	}
	if _, err := Encode(img); err != nil {
		t.Error(err)
	}
}

func TestRenderSymbol(t *testing.T) {
	sym := solid(50, 50, color.NRGBA{255, 255, 255, 255})
	col := color.NRGBA{200, 30, 30, 255}
	img, err := Render(Style{Color: col, Symbol: sym})
	if err != nil {
		t.Fatal(err)
	}
	c := img.NRGBAAt(71, 57)
	if c.A < 250 || c.R < 195 || c.G > 40 {
		t.Errorf("symbol pixel %v", c)
	}
}

func TestRenderTitleTooLong(t *testing.T) {
	_, err := Render(Style{Color: color.NRGBA{255, 0, 0, 255}, Title: strings.Repeat("W", 60)})
	if !errors.Is(err, ErrTitleTooLong) {
		t.Errorf("err = %v", err)
	}
}

func TestRenderShrinksLongTitle(t *testing.T) {
	f, err := loadFont()
	if err != nil {
		t.Fatal(err)
	}
	width := func(px int, s string) int {
		fc, _ := newFace(f, px)
		defer fc.Close()
		return font.MeasureString(fc, s).Ceil() / ss
	}
	// Too wide at 23 px, fits at 17 px.
	title := ""
	for _, w := range strings.Fields("Mute microphone and speakers right now please thanks") {
		title += w + " "
		if width(23, title) > titleMaxW {
			break
		}
	}
	title = strings.TrimSpace(title)
	if width(23, title) <= titleMaxW || width(17, title) > titleMaxW {
		t.Skipf("test title does not straddle the limits: %d / %d", width(23, title), width(17, title))
	}
	if _, err := Render(Style{Color: color.NRGBA{255, 0, 0, 255}, Title: title}); err != nil {
		t.Errorf("err = %v", err)
	}
}
