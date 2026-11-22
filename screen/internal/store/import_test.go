// SPDX-License-Identifier: GPL-2.0-or-later

package store

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func pngBytes(t *testing.T, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i < 4; i++ {
		img.SetNRGBA(i, i, c)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeSrc(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImportPicture(t *testing.T) {
	dir := t.TempDir()
	a := pngBytes(t, color.NRGBA{R: 255, A: 255})
	b := pngBytes(t, color.NRGBA{B: 255, A: 255})

	rel, err := ImportPicture(dir, writeSrc(t, "Landing Gear!.PNG", a), "picture")
	if err != nil || rel != "pictures/landing-gear-.png" {
		t.Fatalf("first: %q, %v", rel, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "pictures", "landing-gear-.png"))
	if err != nil || !bytes.Equal(got, a) {
		t.Fatalf("copy: %v", err)
	}
	// The same bytes under the same name are reused.
	if rel, err = ImportPicture(dir, writeSrc(t, "landing-gear-.png", a), "picture"); err != nil || rel != "pictures/landing-gear-.png" {
		t.Fatalf("reuse: %q, %v", rel, err)
	}
	// Other bytes under the same name get a number.
	if rel, err = ImportPicture(dir, writeSrc(t, "landing-gear-.png", b), "picture"); err != nil || rel != "pictures/landing-gear--2.png" {
		t.Fatalf("second: %q, %v", rel, err)
	}
	c := pngBytes(t, color.NRGBA{G: 255, A: 255})
	if rel, err = ImportPicture(dir, writeSrc(t, "landing-gear-.png", c), "picture"); err != nil || rel != "pictures/landing-gear--3.png" {
		t.Fatalf("third: %q, %v", rel, err)
	}
	if rel, err = ImportPicture(dir, writeSrc(t, "star.png", a), "symbol"); err != nil || rel != "symbols/star.png" {
		t.Fatalf("symbol: %q, %v", rel, err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "pictures"))
	if len(entries) != 3 {
		t.Errorf("pictures/ holds %d files, want 3", len(entries))
	}
}

func TestImportPictureRefuses(t *testing.T) {
	dir := t.TempDir()
	if _, err := ImportPicture(dir, writeSrc(t, "x.png", []byte("not a picture")), "picture"); err == nil {
		t.Error("a text file was accepted")
	}
	if _, err := ImportPicture(dir, writeSrc(t, "x.png", pngBytes(t, color.NRGBA{A: 255})), "other"); err == nil {
		t.Error("an unknown kind was accepted")
	}
	if _, err := ImportPicture(dir, filepath.Join(dir, "missing.png"), "picture"); err == nil {
		t.Error("a missing file was accepted")
	}
	big := writeSrc(t, "big.png", make([]byte, maxPictureSize+1))
	if _, err := ImportPicture(dir, big, "picture"); err == nil {
		t.Error("a file over 20 MiB was accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "pictures")); err == nil {
		t.Error("a refused import left a folder behind")
	}
}
