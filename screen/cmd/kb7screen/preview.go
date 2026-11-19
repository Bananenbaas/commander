// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/Bananenbaas/kb7-commander/screen/internal/apply"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/picture"
)

const (
	panelCols = 3
	panelRows = 4
	panelGap  = 9
)

var panelBackground = color.NRGBA{R: 10, G: 10, B: 14, A: 0xFF}

// preview is one PNG file to write.
type preview struct {
	name string
	img  image.Image
}

// writePreviews renders every button exactly as apply would and writes one
// PNG per button, <profile>-<page>-<position>.png, and one panel per page
// into out: panel-<profile>.png for page 1, always, and
// panel-<profile>-<page>.png for page 2 or 3 when it has buttons. Everything
// is rendered before anything is written, so a button that cannot be rendered
// leaves out as it was. It touches nothing else.
func writePreviews(cfg *layout.Config, out string, stdout io.Writer) error {
	var files []preview
	for _, p := range cfg.Profiles {
		panels := map[int]*image.NRGBA{1: newPanel()}
		for _, b := range p.Buttons {
			img, err := apply.RenderButton(b)
			if err != nil {
				return fmt.Errorf("%s: page %d position %d: %w", filepath.Base(p.File), b.Page, b.Position, err)
			}
			files = append(files, preview{fmt.Sprintf("%d-%d-%d.png", p.Number, b.Page, b.Position), img})
			if panels[b.Page] == nil {
				panels[b.Page] = newPanel()
			}
			placeOnPanel(panels[b.Page], b.Position, img)
		}
		files = append(files, preview{fmt.Sprintf("panel-%d.png", p.Number), panels[1]})
		for pg := 2; pg <= 3; pg++ {
			if panels[pg] != nil {
				files = append(files, preview{fmt.Sprintf("panel-%d-%d.png", p.Number, pg), panels[pg]})
			}
		}
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	for _, f := range files {
		path := filepath.Join(out, f.name)
		if err := writePNG(path, f.img); err != nil {
			return err
		}
		fmt.Fprintln(stdout, path)
	}
	return nil
}

func newPanel() *image.NRGBA {
	w := panelCols*picture.W + (panelCols+1)*panelGap
	h := panelRows*picture.H + (panelRows+1)*panelGap
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(panelBackground), image.Point{}, draw.Src)
	return img
}

// placeOnPanel draws the picture of a position, row by row from the top left,
// over the dark background.
func placeOnPanel(panel *image.NRGBA, position int, img *image.NRGBA) {
	col, row := position%panelCols, position/panelCols
	x := panelGap + col*(picture.W+panelGap)
	y := panelGap + row*(picture.H+panelGap)
	r := image.Rect(x, y, x+picture.W, y+picture.H)
	draw.Draw(panel, r, img, img.Bounds().Min, draw.Over)
}

func writePNG(path string, img image.Image) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return png.Encode(f, img)
}
