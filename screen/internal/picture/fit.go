package picture

import (
	"image"

	"golang.org/x/image/draw"
)

// Fit scales src uniformly to fit inside W x H, centres it and pads with
// transparent pixels.
func Fit(src image.Image) *image.NRGBA {
	return fitInto(src, W, H)
}

// fitInto returns a w x h image holding src scaled uniformly to fit and
// centred, with transparent padding.
func fitInto(src image.Image, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	if src == nil {
		return dst
	}
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw <= 0 || sh <= 0 {
		return dst
	}
	nw, nh := w, h
	if sw*h > sh*w { // wider than the target: width limits
		nh = (sh*w + sw/2) / sw
	} else {
		nw = (sw*h + sh/2) / sh
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	x0, y0 := (w-nw)/2, (h-nh)/2
	draw.CatmullRom.Scale(dst, image.Rect(x0, y0, x0+nw, y0+nh), src, sb, draw.Src, nil)
	return dst
}
