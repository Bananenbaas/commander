package proto

import "fmt"

// PictureSize is the size of one button picture: 142 x 148 pixels of RGB565
// little-endian plus one straight alpha byte each.
const PictureSize = 63048

const (
	frameSize   = 64
	frameData   = frameSize - 2 // payload bytes in a full data frame
	blockSize   = 4096
	maxPicSlot  = 63
	opSlotBase  = 0x40
	opBlockOpen = 0xf1
	opBlockEnd  = 0xf2
	opUploadEnd = 0xff
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
	frames := []UploadFrame{echoFrame(opSlotBase + slot)}
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
	return append(frames, echoFrame(opUploadEnd)), nil
}
