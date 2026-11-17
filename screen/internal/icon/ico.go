// SPDX-License-Identifier: GPL-2.0-or-later

package icon

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
)

// dib encodes img as the image data of a 32-bit ICO entry: a BITMAPINFOHEADER
// with doubled height, bottom-up BGRA pixels and an all-zero AND mask.
func dib(img *image.NRGBA) []byte {
	n := img.Bounds().Dx()
	var b bytes.Buffer
	le := binary.LittleEndian
	hdr := make([]byte, 40)
	le.PutUint32(hdr[0:], 40)
	le.PutUint32(hdr[4:], uint32(n))
	le.PutUint32(hdr[8:], uint32(2*n))
	le.PutUint16(hdr[12:], 1)
	le.PutUint16(hdr[14:], 32)
	b.Write(hdr)
	for y := n - 1; y >= 0; y-- {
		for x := 0; x < n; x++ {
			c := img.NRGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	mask := (n + 31) / 32 * 4
	b.Write(make([]byte, mask*n))
	return b.Bytes()
}

// ICO returns an ICO file with one entry per size: PNG for 256, a 32-bit DIB
// for the others.
func ICO(sizes []int, failed bool) ([]byte, error) {
	if len(sizes) == 0 || len(sizes) > 0xFFFF {
		return nil, fmt.Errorf("icon: bad number of sizes %d", len(sizes))
	}
	images := make([][]byte, len(sizes))
	for i, s := range sizes {
		if s < 1 || s > 256 {
			return nil, fmt.Errorf("icon: size %d out of range", s)
		}
		img := Draw(s, failed)
		if s == 256 {
			var b bytes.Buffer
			if err := png.Encode(&b, img); err != nil {
				return nil, err
			}
			images[i] = b.Bytes()
		} else {
			images[i] = dib(img)
		}
	}
	le := binary.LittleEndian
	out := make([]byte, 6+16*len(sizes))
	le.PutUint16(out[2:], 1)
	le.PutUint16(out[4:], uint16(len(sizes)))
	off := len(out)
	for i, s := range sizes {
		e := out[6+16*i:]
		e[0], e[1] = byte(s), byte(s) // 256 wraps to 0
		le.PutUint16(e[4:], 1)
		le.PutUint16(e[6:], 32)
		le.PutUint32(e[8:], uint32(len(images[i])))
		le.PutUint32(e[12:], uint32(off))
		off += len(images[i])
	}
	for _, im := range images {
		out = append(out, im...)
	}
	return out, nil
}
