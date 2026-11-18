// SPDX-License-Identifier: GPL-2.0-or-later

package proto

import "fmt"

// PictureSize is the size of one button picture: 142 x 148 pixels of RGB565
// little-endian plus one straight alpha byte each.
const PictureSize = 63048

// BackgroundSize is the size of a screen background: 480 x 800 pixels of
// RGB565 little-endian, row by row from the top left, without alpha.
const BackgroundSize = 480 * 800 * 2

const (
	frameSize    = 64
	frameData    = frameSize - 2 // payload bytes in a full data frame
	blockSize    = 4096
	maxPicSlot   = 63
	opSlotBase   = 0x40 // + slot; slot 0 (0x40) is the background's thumbnail
	opBackground = 0x00
	opBlockOpen  = 0xf1
	opBlockEnd   = 0xf2
	opUploadEnd  = 0xff
)

// UploadFrame is one frame of a picture upload: the 64 bytes to send and the
// 64 bytes the device answers with.
type UploadFrame struct {
	Out, Ack [64]byte
}

func echoFrame(op byte) UploadFrame {
	var f UploadFrame
	f.Out[0], f.Out[1] = 0xa5, op
	f.Ack = f.Out
	return f
}

// UploadFrames cuts a picture into the frames that upload it to the given
// slot (1-63), in the order they are sent.
func UploadFrames(slot byte, data []byte) ([]UploadFrame, error) {
	if slot < 1 || slot > maxPicSlot {
		return nil, fmt.Errorf("slot %d out of range 1-%d", slot, maxPicSlot)
	}
	if len(data) != PictureSize {
		return nil, fmt.Errorf("picture is %d bytes, want %d", len(data), PictureSize)
	}
	return framesFor(opSlotBase+slot, data), nil
}

// BackgroundFrames cuts a screen background and its thumbnail into the frames
// that upload them, in the order Swarm II sends them: the background (opener
// 0x00), then the thumbnail the keyboard shows in its background picker
// (opener 0x40, the picture format of a button).
func BackgroundFrames(full, thumb []byte) ([]UploadFrame, error) {
	if len(full) != BackgroundSize {
		return nil, fmt.Errorf("background is %d bytes, want %d", len(full), BackgroundSize)
	}
	if len(thumb) != PictureSize {
		return nil, fmt.Errorf("thumbnail is %d bytes, want %d", len(thumb), PictureSize)
	}
	return append(framesFor(opBackground, full), framesFor(opSlotBase, thumb)...), nil
}

// framesFor frames data as one upload opened with op.
func framesFor(op byte, data []byte) []UploadFrame {
	frames := []UploadFrame{echoFrame(op)}
	for off := 0; off < len(data); off += blockSize {
		block := data[off:min(off+blockSize, len(data))]
		frames = append(frames, echoFrame(opBlockOpen))
		for len(block) > 0 {
			n := min(frameData, len(block))
			var f UploadFrame
			f.Out[0], f.Out[1] = 0xa5, byte(n)
			copy(f.Out[2:], block[:n])
			for i := range f.Ack {
				f.Ack[i] = 0xff
			}
			frames = append(frames, f)
			block = block[n:]
		}
		frames = append(frames, echoFrame(opBlockEnd))
	}
	return append(frames, echoFrame(opUploadEnd))
}
