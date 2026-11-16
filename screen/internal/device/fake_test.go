package device

import (
	"bytes"
	"errors"
	"testing"

	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

var _ Device = (*Fake)(nil)

func frame(b ...byte) [64]byte {
	var f [64]byte
	copy(f[:], b)
	return f
}

func TestFakeRecordsInOrder(t *testing.T) {
	var f Fake
	f.QueueFeature([]byte{0x05, 0x01, 0xba, 0x00})
	if err := f.SetFeature(proto.Select(0, proto.SelNames)); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteStream(frame(0xa5, 0x41)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.GetFeature(0x05, 4); err != nil {
		t.Fatal(err)
	}
	if err := f.SetFeature(proto.SwitchProfile(1)); err != nil {
		t.Fatal(err)
	}

	calls := f.Calls()
	wantOps := []Op{OpSetFeature, OpWriteStream, OpGetFeature, OpSetFeature}
	if len(calls) != len(wantOps) {
		t.Fatalf("%d calls, want %d", len(calls), len(wantOps))
	}
	for i, op := range wantOps {
		if calls[i].Op != op {
			t.Fatalf("call %d is %s, want %s", i, calls[i].Op, op)
		}
	}
	if calls[2].ID != 0x05 || calls[2].N != 4 || !bytes.Equal(calls[2].Data, []byte{0x05, 0x01, 0xba, 0x00}) {
		t.Fatalf("GetFeature call = %+v", calls[2])
	}

	feats := f.Features()
	if len(feats) != 2 || !bytes.Equal(feats[0], []byte{0x05, 0x00, 0xba, 0x00}) || !bytes.Equal(feats[1], []byte{0x06, 0x04, 0x01, 0x05}) {
		t.Fatalf("Features = % x", feats)
	}
	if fr := f.Frames(); len(fr) != 1 || fr[0] != frame(0xa5, 0x41) {
		t.Fatalf("Frames = % x", fr)
	}
}

func TestFakeRecordsCopies(t *testing.T) {
	var f Fake
	b := []byte{0x0e, 0x05, 0x01, 0x00, 0x00}
	f.SetFeature(b)
	b[2] = 0
	if got := f.Features()[0]; got[2] != 0x01 {
		t.Fatal("the Fake kept the caller's slice")
	}
	f.Features()[0][2] = 0x07
	if got := f.Features()[0]; got[2] != 0x01 {
		t.Fatal("Features handed out the Fake's own slice")
	}
}

func TestFakeGetFeature(t *testing.T) {
	var f Fake
	f.QueueFeature([]byte{0x05, 0x01}, []byte{0x29, 0xaa}, []byte{0x05, 0x02})
	r, err := f.GetFeature(0x05, 4)
	if err != nil || !bytes.Equal(r, []byte{0x05, 0x01, 0x00, 0x00}) {
		t.Fatalf("first 0x05 = % x, %v", r, err)
	}
	r, err = f.GetFeature(0x05, 4)
	if err != nil || !bytes.Equal(r, []byte{0x05, 0x02, 0x00, 0x00}) {
		t.Fatalf("second 0x05 = % x, %v", r, err)
	}
	if _, err := f.GetFeature(0x05, 4); err != ErrTimeout {
		t.Fatalf("empty queue: %v, want ErrTimeout", err)
	}
	if _, err := f.GetFeature(0x29, 1); err == nil {
		t.Fatal("a reply longer than asked for was accepted")
	}
	if r, err := f.GetFeature(0x29, 584); err != nil || len(r) != 584 || r[1] != 0xaa {
		t.Fatalf("0x29 = %d bytes, %v", len(r), err)
	}
	if _, err := f.GetFeature(0x29, 0); err == nil {
		t.Fatal("GetFeature of 0 bytes accepted")
	}
}

func TestFakeReadStreamScripted(t *testing.T) {
	var f Fake
	f.QueueStream(frame(1), frame(2))
	for _, want := range []byte{1, 2} {
		got, err := f.ReadStream(0)
		if err != nil || got[0] != want {
			t.Fatalf("ReadStream = % x, %v; want frame %d", got[:2], err, want)
		}
	}
	if _, err := f.ReadStream(0); err != ErrTimeout {
		t.Fatalf("empty: %v, want ErrTimeout", err)
	}
}

func TestFakeEcho(t *testing.T) {
	ff := frame()
	for i := range ff {
		ff[i] = 0xff
	}
	for _, tc := range []struct {
		out    [64]byte
		answer bool
		want   [64]byte
	}{
		{frame(0xa5, 0x00), true, frame(0xa5, 0x00)},
		{frame(0xa5, 0x40), true, frame(0xa5, 0x40)},
		{frame(0xa5, 0x7f), true, frame(0xa5, 0x7f)},
		{frame(0xa5, 0xf1), true, frame(0xa5, 0xf1)},
		{frame(0xa5, 0xf2), true, frame(0xa5, 0xf2)},
		{frame(0xa5, 0xff), true, frame(0xa5, 0xff)},
		{frame(0xa5, 0x01, 0x12), true, ff},
		{frame(0xa5, 0x3e, 0x12), true, ff},
		{frame(0xa5, 0x3f), false, [64]byte{}},
		{frame(0xa5, 0x80), false, [64]byte{}},
		{frame(0xa3, 0x00, 0x01), false, [64]byte{}},
	} {
		f := Fake{Echo: true}
		if err := f.WriteStream(tc.out); err != nil {
			t.Fatal(err)
		}
		got, err := f.ReadStream(0)
		if !tc.answer {
			if err != ErrTimeout {
				t.Errorf("% x: answered % x, %v", tc.out[:2], got[:2], err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("% x: answer % x, %v; want % x", tc.out[:2], got[:4], err, tc.want[:4])
		}
	}
}

func TestFakeEchoAfterScripted(t *testing.T) {
	f := Fake{Echo: true}
	f.QueueStream(frame(0xa5, 0x42)) // a wrong echo, read first
	f.WriteStream(frame(0xa5, 0x41))
	for _, want := range []byte{0x42, 0x41} {
		got, err := f.ReadStream(0)
		if err != nil || got[1] != want {
			t.Fatalf("ReadStream = % x, %v; want a5 %02x", got[:2], err, want)
		}
	}
}

// The echoing Fake answers a whole picture upload exactly as the frames
// from proto expect.
func TestFakeEchoMatchesUpload(t *testing.T) {
	frames, err := proto.UploadFrames(1, make([]byte, proto.PictureSize))
	if err != nil {
		t.Fatal(err)
	}
	f := Fake{Echo: true}
	for i, fr := range frames {
		if err := f.WriteStream(fr.Out); err != nil {
			t.Fatal(err)
		}
		got, err := f.ReadStream(0)
		if err != nil || got != fr.Ack {
			t.Fatalf("frame %d: answer % x, %v; want % x", i, got[:4], err, fr.Ack[:4])
		}
	}
	if len(f.Frames()) != len(frames) {
		t.Fatalf("%d frames recorded, want %d", len(f.Frames()), len(frames))
	}
}

func TestFakeReadInput(t *testing.T) {
	var f Fake
	f.QueueInput([]byte{0x03, 0x00, 0x01, 0x01}, []byte{0x33, 0x01, 0x05})
	for _, want := range []byte{0x03, 0x33} {
		r, err := f.ReadInput(0)
		if err != nil || r[0] != want {
			t.Fatalf("ReadInput = % x, %v; want report %02x", r, err, want)
		}
	}
	if _, err := f.ReadInput(0); err != ErrTimeout {
		t.Fatalf("empty: %v, want ErrTimeout", err)
	}
}

func TestFakeFailAt(t *testing.T) {
	boom := errors.New("boom")
	var f Fake
	f.FailAt(OpSetFeature, 2, boom)
	f.FailAt(OpReadStream, 1, boom)
	f.FailAt(OpGetFeature, 1, boom)
	f.QueueStream(frame(7))
	f.QueueFeature([]byte{0x05, 0x01})

	if err := f.SetFeature([]byte{0x01}); err != nil {
		t.Fatalf("first SetFeature: %v", err)
	}
	if err := f.SetFeature([]byte{0x02}); err != boom {
		t.Fatalf("second SetFeature: %v, want boom", err)
	}
	if err := f.SetFeature([]byte{0x03}); err != nil {
		t.Fatalf("third SetFeature: %v", err)
	}
	if feats := f.Features(); len(feats) != 2 || feats[0][0] != 0x01 || feats[1][0] != 0x03 {
		t.Fatalf("Features = % x, want the accepted ones", feats)
	}
	if c := f.Calls()[1]; c.Op != OpSetFeature || c.Err != boom || c.Data[0] != 0x02 {
		t.Fatalf("failed call recorded as %+v", c)
	}

	// A failed read leaves the queue alone.
	if _, err := f.ReadStream(0); err != boom {
		t.Fatalf("first ReadStream: %v, want boom", err)
	}
	if got, err := f.ReadStream(0); err != nil || got[0] != 7 {
		t.Fatalf("second ReadStream = % x, %v", got[:1], err)
	}
	if _, err := f.GetFeature(0x05, 4); err != boom {
		t.Fatalf("first GetFeature: %v, want boom", err)
	}
	if r, err := f.GetFeature(0x05, 4); err != nil || r[1] != 0x01 {
		t.Fatalf("second GetFeature = % x, %v", r, err)
	}
}

func TestFakeDisconnected(t *testing.T) {
	var f Fake
	f.QueueInput([]byte{0x33}, []byte{0x03})
	f.FailAt(OpReadInput, 2, disconnected("read input", nil))
	f.FailAt(OpWriteStream, 1, ErrDisconnected)
	if _, err := f.ReadInput(0); err != nil {
		t.Fatalf("first ReadInput: %v", err)
	}
	if _, err := f.ReadInput(0); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("second ReadInput: %v, want ErrDisconnected", err)
	}
	if err := f.WriteStream(frame(0xa5, 0x41)); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("WriteStream: %v, want ErrDisconnected", err)
	}
}

func TestFakeFailAtUnknownOp(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("FailAt accepted an unknown op")
		}
	}()
	var f Fake
	f.FailAt("Flush", 1, errors.New("x"))
}

func TestFakeClose(t *testing.T) {
	var f Fake
	f.QueueInput([]byte{0x33})
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if !f.Closed() {
		t.Fatal("Closed() = false after Close")
	}
	if err := f.SetFeature([]byte{0x0e}); err != ErrClosed {
		t.Fatalf("SetFeature after Close: %v", err)
	}
	if _, err := f.GetFeature(0x05, 4); err != ErrClosed {
		t.Fatalf("GetFeature after Close: %v", err)
	}
	if err := f.WriteStream(frame()); err != ErrClosed {
		t.Fatalf("WriteStream after Close: %v", err)
	}
	if _, err := f.ReadStream(0); err != ErrClosed {
		t.Fatalf("ReadStream after Close: %v", err)
	}
	if _, err := f.ReadInput(0); err != ErrClosed {
		t.Fatalf("ReadInput after Close: %v", err)
	}
	if err := f.Close(); err != ErrClosed {
		t.Fatalf("second Close: %v", err)
	}
	if len(f.Features()) != 0 || len(f.Frames()) != 0 {
		t.Fatal("calls after Close were accepted")
	}
}

func TestFakeFailedCloseStillCloses(t *testing.T) {
	boom := errors.New("boom")
	var f Fake
	f.FailAt(OpClose, 1, boom)
	if err := f.Close(); err != boom {
		t.Fatalf("Close: %v, want boom", err)
	}
	if !f.Closed() {
		t.Fatal("a failed Close left the Fake open")
	}
}
