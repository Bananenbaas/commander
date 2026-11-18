// SPDX-License-Identifier: GPL-2.0-or-later

package picture

import (
	"errors"
	"image"
	"image/draw"

	xdraw "golang.org/x/image/draw"
)

// BW and BH are the screen background size in pixels (portrait).
const BW, BH = 480, 800

// Background turns src into the keyboard's background bytes and the
// thumbnail its background picker shows. src is scaled to cover the whole
// 480 x 800 screen, keeping its proportions, and cropped in the middle; it is
// drawn over black, so transparency shows as black. The thumbnail is the
// whole background squashed to 142 x 148, as Swarm II sends it.
func Background(src image.Image) (full, thumb []byte, err error) {
	if src == nil {
		return nil, nil, errors.New("picture: nil background")
	}
	sb := src.Bounds()
	if sb.Dx() <= 0 || sb.Dy() <= 0 {
		return nil, nil, errors.New("picture: empty background")
	}
	screen := image.NewNRGBA(image.Rect(0, 0, BW, BH))
	draw.Draw(screen, screen.Bounds(), image.Black, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(screen, coverRect(sb.Dx(), sb.Dy(), BW, BH), src, sb, xdraw.Over, nil)

	full = make([]byte, BW*BH*2)
	for y := 0; y < BH; y++ {
		row := screen.Pix[y*screen.Stride:]
		for x := 0; x < BW; x++ {
			r, g, b := row[x*4], row[x*4+1], row[x*4+2]
			v := uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(b>>3)
			i := (y*BW + x) * 2
			full[i], full[i+1] = byte(v), byte(v>>8)
		}
	}

	small := image.NewNRGBA(image.Rect(0, 0, W, H))
	xdraw.CatmullRom.Scale(small, small.Bounds(), screen, screen.Bounds(), xdraw.Src, nil)
	thumb, err = Encode(small)
	if err != nil {
		return nil, nil, err
	}
	return full, thumb, nil
}

// coverRect is where an sw x sh image goes, scaled uniformly, so that it
// covers a w x h area centred on it; the parts outside the area are cut off.
func coverRect(sw, sh, w, h int) image.Rectangle {
	nw, nh := w, h
	if sw*h > sh*w { // wider than the area: height limits, sides are cut
		nw = (sw*h + sh/2) / sh
	} else {
		nh = (sh*w + sw/2) / sw
	}
	x0, y0 := (w-nw)/2, (h-nh)/2
	return image.Rect(x0, y0, x0+nw, y0+nh)
}
