// SPDX-License-Identifier: GPL-2.0-or-later

package apply

import (
	"bytes"
	"path/filepath"
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
