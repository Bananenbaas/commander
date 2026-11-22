// SPDX-License-Identifier: GPL-2.0-or-later

package apply

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/picture"
)

func TestRenderButtonMatchesWhatApplySends(t *testing.T) {
	img, err := RenderButton(btn(2, "Flight"))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != picture.W || img.Bounds().Dy() != picture.H {
		t.Fatalf("size %v", img.Bounds())
	}
	data, err := picture.Encode(img)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, rendered(t, "Flight")) {
		t.Fatal("RenderButton differs from the picture apply sends")
	}
	if _, err := RenderButton(layout.Button{Picture: filepath.Join(t.TempDir(), "missing.png")}); err == nil {
		t.Fatal("a missing picture file must be an error")
	}
}

func TestCheckPictures(t *testing.T) {
	n, err := CheckPictures(cfgOf(
		prof(0, "a.toml", "", btn(0, "Same"), btn(1, "Other")),
		prof(1, "b.toml", "", btn(0, "Same")),
	))
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("CheckPictures = %d, want 2: two buttons with the same look count once", n)
	}
	bad := layout.Button{Page: 2, Position: 3, Picture: filepath.Join(t.TempDir(), "missing.png")}
	_, err = CheckPictures(cfgOf(prof(0, "a.toml", "", bad)))
	if err == nil || !strings.Contains(err.Error(), "a.toml: page 2 position 3: ") {
		t.Fatalf("err = %v, want one naming the button", err)
	}
}
