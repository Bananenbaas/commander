// Package picture turns a button into the bytes the keyboard stores for a
// picture: 142 x 148 pixels as RGB565 little-endian followed by one straight
// alpha byte per pixel.
package picture

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
)

// W and H are the picture size in pixels.
const W, H = 142, 148

// Size is the length of an encoded picture.
const Size = W * H * 3

// Encode returns the keyboard's picture bytes for img, which must be exactly
// W x H with its bounds starting at (0, 0). Alpha is straight, not
// premultiplied.
func Encode(img *image.NRGBA) ([]byte, error) {
	if img == nil {
		return nil, errors.New("picture: nil image")
	}
	b := img.Bounds()
	if b.Min.X != 0 || b.Min.Y != 0 || b.Dx() != W || b.Dy() != H {
		return nil, fmt.Errorf("picture: image is %v, want %dx%d starting at (0,0)", b, W, H)
	}
	out := make([]byte, Size)
	alpha := out[W*H*2:]
	for y := 0; y < H; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+W*4]
		for x := 0; x < W; x++ {
			r, g, bl, a := row[x*4], row[x*4+1], row[x*4+2], row[x*4+3]
			v := uint16(r>>3)<<11 | uint16(g>>2)<<5 | uint16(bl>>3)
			i := y*W + x
			out[i*2] = byte(v)
			out[i*2+1] = byte(v >> 8)
			alpha[i] = a
		}
	}
	return out, nil
}

// Hash returns the SHA-256 of data as lowercase hex.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
