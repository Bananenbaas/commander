// SPDX-License-Identifier: GPL-2.0-or-later

// Package icon draws the Commander helmet icon and packs it as an ICO file
// and as a Windows resource object.
package icon

import (
	"image"
	"math"
)

// Sizes are the icon sizes written to the ICO file, in order.
var Sizes = []int{16, 20, 24, 32, 48, 256}

const supersample = 4

type rgb [3]float64

type layer struct {
	in      func(x, y float64) bool
	colour  func(x, y float64) rgb
	opacity float64
}

func mix(a, b rgb, t float64) rgb {
	return rgb{a[0] + (b[0]-a[0])*t, a[1] + (b[1]-a[1])*t, a[2] + (b[2]-a[2])*t}
}

func flat(c rgb) func(x, y float64) rgb { return func(x, y float64) rgb { return c } }

func rrect(x0, y0, x1, y1, r float64) func(x, y float64) bool {
	return func(x, y float64) bool {
		cx := math.Min(math.Max(x, x0+r), x1-r)
		cy := math.Min(math.Max(y, y0+r), y1-r)
		return math.Hypot(x-cx, y-cy) <= r
	}
}

func ellv(cx, cy, rx, ry float64) func(x, y float64) float64 {
	return func(x, y float64) float64 {
		dx, dy := (x-cx)/rx, (y-cy)/ry
		return dx*dx + dy*dy
	}
}

func ell(cx, cy, rx, ry float64) func(x, y float64) bool {
	v := ellv(cx, cy, rx, ry)
	return func(x, y float64) bool { return v(x, y) <= 1 }
}

func and(fs ...func(x, y float64) bool) func(x, y float64) bool {
	return func(x, y float64) bool {
		for _, f := range fs {
			if !f(x, y) {
				return false
			}
		}
		return true
	}
}

// design returns the helmet layers in a 0..100 space, painted in order. At
// 24 px and below the light streak is left out and the antenna and the visor
// frame are thicker so they stay at least about one pixel wide.
func design(size int, failed bool) []layer {
	small := size <= 24
	var L []layer
	add := func(in func(x, y float64) bool, c func(x, y float64) rgb) {
		L = append(L, layer{in, c, 1})
	}

	add(rrect(3, 3, 97, 97, 20), func(x, y float64) rgb {
		return mix(rgb{26, 36, 70}, rgb{10, 14, 30}, y/100)
	})
	dome := and(ell(50, 57, 33, 31), func(x, y float64) bool { return y < 76 })
	add(dome, func(x, y float64) rgb {
		return mix(rgb{110, 210, 255}, rgb{0, 110, 200}, math.Min(1, math.Hypot((x-38)/40, (y-36)/40)*1.1))
	})
	domeV := ellv(50, 57, 33, 31)
	add(and(dome, func(x, y float64) bool { return domeV(x, y) > 0.88 }), flat(rgb{0, 80, 155}))
	add(and(dome, func(x, y float64) bool { return math.Abs(x-50) < 4.2 && y < 50 }), flat(rgb{240, 248, 255}))

	// The glass sits inside the frame: 2 units in, or 4 for small sizes.
	add(rrect(25, 47, 75, 66, 9), flat(rgb{20, 24, 40}))
	gx0, gy0, gx1, gy1, gr := 27.0, 49.0, 73.0, 64.0, 7.5
	if small {
		gx0, gy0, gx1, gy1, gr = 29, 51, 71, 62, 5.5
	}
	glass := rrect(gx0, gy0, gx1, gy1, gr)
	add(glass, func(x, y float64) rgb {
		return mix(rgb{255, 150, 40}, rgb{170, 60, 15}, (y-gy0)/(gy1-gy0))
	})
	if !small {
		L = append(L, layer{
			in: and(glass, func(x, y float64) bool {
				return math.Abs((x-37)+(y-51)*1.4) < 2.0 && y < 60
			}),
			colour:  flat(rgb{255, 235, 200}),
			opacity: 0.7,
		})
	}

	add(rrect(15, 73, 85, 82, 3), func(x, y float64) rgb {
		return mix(rgb{40, 170, 240}, rgb{0, 90, 170}, (y-73)/9)
	})
	half := 1.8
	if small {
		half = 3.2
	}
	add(func(x, y float64) bool { return math.Abs(x-50) < half && y > 9 && y < 27 }, flat(rgb{200, 220, 240}))
	add(ell(50, 9, 4.2, 4.2), func(x, y float64) rgb {
		return mix(rgb{255, 245, 210}, rgb{255, 140, 40}, math.Hypot(x-49, y-8)/5)
	})

	if failed {
		add(ell(78, 78, 19, 19), flat(rgb{20, 24, 40}))
		add(ell(78, 78, 14.5, 14.5), flat(rgb{255, 48, 48}))
	}
	return L
}

// Draw renders the icon at size x size pixels. With failed set a red dot with
// a dark ring is added at the bottom right.
func Draw(size int, failed bool) *image.NRGBA {
	L := design(size, failed)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	clamp := func(v float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(v)))) }
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var acc [4]float64
			for sy := 0; sy < supersample; sy++ {
				for sx := 0; sx < supersample; sx++ {
					u := (float64(x) + (float64(sx)+.5)/supersample) * 100 / float64(size)
					v := (float64(y) + (float64(sy)+.5)/supersample) * 100 / float64(size)
					var col rgb
					a, seen := 0.0, false
					for _, l := range L {
						if !l.in(u, v) {
							continue
						}
						c := l.colour(u, v)
						if !seen {
							col, a, seen = c, l.opacity, true
						} else {
							col = mix(col, c, l.opacity)
							a += (1 - a) * l.opacity
						}
					}
					if seen {
						acc[0] += col[0] * a
						acc[1] += col[1] * a
						acc[2] += col[2] * a
						acc[3] += a
					}
				}
			}
			if n := acc[3]; n > 0 {
				i := img.PixOffset(x, y)
				img.Pix[i+0] = clamp(acc[0] / n)
				img.Pix[i+1] = clamp(acc[1] / n)
				img.Pix[i+2] = clamp(acc[2] / n)
				img.Pix[i+3] = clamp(255 * n / (supersample * supersample))
			}
		}
	}
	return img
}
