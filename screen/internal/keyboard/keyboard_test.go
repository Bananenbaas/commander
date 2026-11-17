// SPDX-License-Identifier: GPL-2.0-or-later

package keyboard

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// clock is a fake clock: sleep advances it and is recorded.
type clock struct {
	t      time.Time
	sleeps []time.Duration
}

func (c *clock) now() time.Time { return c.t }
func (c *clock) sleep(d time.Duration) {
	c.sleeps = append(c.sleeps, d)
	c.t = c.t.Add(d)
}

// spy wraps a Fake and records the timeouts asked for. It can also queue
// input once SetFeature has been called, and return one junk stream frame as
// the junkAt-th ReadStream result.
type spy struct {
	*device.Fake
	inputTimeouts, streamTimeouts []time.Duration
	afterSet                      [][]byte
	junk                          [64]byte
	junkAt, streamReads           int
}

func (s *spy) ReadInput(d time.Duration) ([]byte, error) {
	s.inputTimeouts = append(s.inputTimeouts, d)
	return s.Fake.ReadInput(d)
}

func (s *spy) ReadStream(d time.Duration) ([64]byte, error) {
	s.streamTimeouts = append(s.streamTimeouts, d)
	s.streamReads++
	if s.junkAt > 0 && s.streamReads == s.junkAt {
		return s.junk, nil
	}
	return s.Fake.ReadStream(d)
}

func (s *spy) SetFeature(b []byte) error {
	err := s.Fake.SetFeature(b)
	s.Fake.QueueInput(s.afterSet...)
	return err
}

func newTest(d device.Device) (*KB, *clock) {
	c := &clock{t: time.Unix(1000, 0)}
	return newKB(d, c.now, c.sleep), c
}

func picture() []byte {
	p := make([]byte, proto.PictureSize)
	for i := range p {
		p[i] = byte(i * 7)
	}
	return p
}

func seal(b []byte) []byte {
	n := len(b) - 2
	s := proto.Sum16(b[:n])
	b[n], b[n+1] = byte(s), byte(s>>8)
	return b
}

func blank29(profile byte) []byte {
	b := make([]byte, 584)
	b[0], b[1], b[2], b[3] = 0x29, 0x48, 0x02, profile
	return seal(b)
}

func blank25(profile byte) []byte {
	b := make([]byte, 164)
	b[0], b[1], b[2] = 0x25, 0xa4, profile
	return seal(b)
}

func TestUploadFullEcho(t *testing.T) {
	f := &device.Fake{Echo: true}
	kb, _ := newTest(f)
	data := picture()
	if err := kb.Upload(3, data); err != nil {
		t.Fatal(err)
	}
	want, _ := proto.UploadFrames(3, data)
	got := f.Frames()
	if len(got) != len(want) {
		t.Fatalf("%d frames sent, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i].Out {
			t.Fatalf("frame %d differs", i)
		}
	}
}

func TestUploadBadSlot(t *testing.T) {
	f := &device.Fake{Echo: true}
	kb, _ := newTest(f)
	if err := kb.Upload(0, picture()); err == nil {
		t.Fatal("slot 0 accepted")
	}
	if len(f.Frames()) != 0 {
		t.Fatal("frames sent")
	}
}

func TestUploadDrainsStaleFrames(t *testing.T) {
	f := &device.Fake{Echo: true}
	var stale [64]byte
	stale[0], stale[1] = 0xa5, 0x77
	f.QueueStream(stale, stale)
	kb, _ := newTest(f)
	if err := kb.Upload(1, picture()); err != nil {
		t.Fatal(err)
	}
}

func TestUploadOpenerRefused(t *testing.T) {
	f := &device.Fake{}
	var wrong [64]byte
	wrong[0], wrong[1] = 0xa5, 0x42
	// ReadStream 1 is the drain, 2 the opener's acknowledgement.
	s := &spy{Fake: f, junk: wrong, junkAt: 2}
	kb, _ := newTest(s)
	err := kb.Upload(1, picture())
	if !errors.Is(err, ErrOpenerRefused) {
		t.Fatalf("got %v", err)
	}
	if errors.Is(err, ErrUploadLeftOpen) {
		t.Fatal("must not be left-open")
	}
	if n := len(f.Frames()); n != 1 {
		t.Fatalf("%d frames sent, want only the opener", n)
	}
}

func TestUploadOpenerNotEchoed(t *testing.T) {
	f := &device.Fake{}
	kb, _ := newTest(f)
	err := kb.Upload(1, picture())
	if !errors.Is(err, ErrUploadLeftOpen) {
		t.Fatalf("got %v", err)
	}
	if n := len(f.Frames()); n != 1 {
		t.Fatalf("%d frames sent", n)
	}
}

func TestUploadAckMissingAfterOpener(t *testing.T) {
	f := &device.Fake{Echo: true}
	// Reads: 1 drain, 2 opener ack, 3 block-open ack, 4 first data ack.
	f.FailAt(device.OpReadStream, 4, device.ErrTimeout)
	kb, _ := newTest(f)
	err := kb.Upload(1, picture())
	if !errors.Is(err, ErrUploadLeftOpen) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), "unplug") {
		t.Fatalf("message does not tell to unplug: %v", err)
	}
	if !errors.Is(err, device.ErrTimeout) {
		t.Fatal("cause lost")
	}
	if n := len(f.Frames()); n != 3 {
		t.Fatalf("%d frames sent, want 3", n)
	}
}

func TestUploadMismatchedAck(t *testing.T) {
	f := &device.Fake{Echo: true}
	var bad [64]byte // an a5 frame that is not the ack of the block-open frame
	bad[0], bad[1] = 0xa5, 0x01
	// ReadStream 1 drain, 2 opener ack, 3 block-open ack (replaced).
	s := &spy{Fake: f, junk: bad, junkAt: 3}
	kb, _ := newTest(s)
	err := kb.Upload(1, picture())
	if !errors.Is(err, ErrUploadLeftOpen) {
		t.Fatalf("got %v", err)
	}
	if n := len(f.Frames()); n != 2 {
		t.Fatalf("%d frames sent, want 2 (nothing after the mismatch)", n)
	}
}

func TestUploadSkipsUnexpectedFrame(t *testing.T) {
	f := &device.Fake{Echo: true}
	var junk [64]byte
	junk[0] = 0x12 // neither a5 nor all ff
	// ReadStream 1 drain, 2 junk, then the real opener ack follows.
	s := &spy{Fake: f, junk: junk, junkAt: 2}
	kb, _ := newTest(s)
	if err := kb.Upload(2, picture()); err != nil {
		t.Fatal(err)
	}
	want, _ := proto.UploadFrames(2, picture())
	if len(f.Frames()) != len(want) {
		t.Fatalf("%d frames", len(f.Frames()))
	}
}

func TestUploadDisconnectedMidway(t *testing.T) {
	f := &device.Fake{Echo: true}
	f.FailAt(device.OpWriteStream, 3, device.ErrDisconnected)
	kb, _ := newTest(f)
	err := kb.Upload(1, picture())
	if !errors.Is(err, ErrUploadLeftOpen) || !errors.Is(err, device.ErrDisconnected) {
		t.Fatalf("got %v", err)
	}
}

func TestUploadAckTimeoutIs5s(t *testing.T) {
	s := &spy{Fake: &device.Fake{Echo: true}}
	kb, _ := newTest(s)
	if err := kb.Upload(1, picture()); err != nil {
		t.Fatal(err)
	}
	if len(s.streamTimeouts) < 3 || s.streamTimeouts[1] != 5*time.Second {
		t.Fatalf("timeouts %v", s.streamTimeouts[:3])
	}
}

func TestNextEventSkipsUnparsed(t *testing.T) {
	f := &device.Fake{}
	f.QueueInput([]byte{0x03, 0x00, 0x99, 0x00}, []byte{0x33}, []byte{0x03, 0x00, 0x01, 0x02})
	kb, _ := newTest(f)
	ev, err := kb.NextEvent(time.Second)
	if err != nil || ev != (proto.ProfileChanged{Profile: 2}) {
		t.Fatalf("%v %v", ev, err)
	}
	if _, err := kb.NextEvent(time.Second); !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("got %v", err)
	}
}

func TestNextEventDeviceError(t *testing.T) {
	f := &device.Fake{}
	f.FailAt(device.OpReadInput, 1, device.ErrDisconnected)
	kb, _ := newTest(f)
	if _, err := kb.NextEvent(time.Second); !errors.Is(err, device.ErrDisconnected) {
		t.Fatalf("got %v", err)
	}
}

func TestClose(t *testing.T) {
	f := &device.Fake{}
	kb, _ := newTest(f)
	if err := kb.Close(); err != nil || !f.Closed() {
		t.Fatal("not closed")
	}
}

func TestSwitchProfile(t *testing.T) {
	f := &device.Fake{}
	f.QueueInput([]byte{0x03, 0x00, 0x01, 0x01}) // stale, drained before sending
	s := &spy{Fake: f, afterSet: [][]byte{
		{0x33, 0, 1, 0, 0, 0, 0, 1}, // a touch: discarded
		{0x03, 0x00, 0x01, 0x01},
	}}
	kb, _ := newTest(s)
	if err := kb.SwitchProfile(1); err != nil {
		t.Fatal(err)
	}
	if got := f.Features(); len(got) != 1 || !bytes.Equal(got[0], proto.SwitchProfile(1)) {
		t.Fatalf("%x", got)
	}
	// The stale report must have been read before the frame was sent.
	staleBefore, sawSet := false, false
	for _, c := range f.Calls() {
		if c.Op == device.OpSetFeature {
			sawSet = true
		}
		if c.Op == device.OpReadInput && c.Err == nil && !sawSet {
			staleBefore = true
		}
	}
	if !staleBefore {
		t.Fatal("old input was not dropped before sending")
	}
}

func TestSwitchProfileTimeout(t *testing.T) {
	s := &spy{Fake: &device.Fake{}}
	kb, _ := newTest(s)
	err := kb.SwitchProfile(1)
	if !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("got %v", err)
	}
	if last := s.inputTimeouts[len(s.inputTimeouts)-1]; last != 3*time.Second {
		t.Fatalf("waited %v", last)
	}
}

func TestSwitchProfileWrongProfileTimesOut(t *testing.T) {
	s := &spy{Fake: &device.Fake{}, afterSet: [][]byte{{0x03, 0x00, 0x01, 0x02}}}
	kb, _ := newTest(s)
	if err := kb.SwitchProfile(1); !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("got %v", err)
	}
}

func TestEnableNotifications(t *testing.T) {
	s := &spy{Fake: &device.Fake{}, afterSet: [][]byte{
		{0x03, 0x00, 0x01, 0x00},
		{0x03, 0x00, 0x31, 0x20},
	}}
	kb, _ := newTest(s)
	if err := kb.EnableNotifications(); err != nil {
		t.Fatal(err)
	}
	if got := s.Fake.Features(); len(got) != 1 || !bytes.Equal(got[0], proto.Notifications(true)) {
		t.Fatalf("%x", got)
	}
}

func TestEnableNotificationsTimeout(t *testing.T) {
	s := &spy{Fake: &device.Fake{}}
	kb, _ := newTest(s)
	if err := kb.EnableNotifications(); !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("got %v", err)
	}
	if last := s.inputTimeouts[len(s.inputTimeouts)-1]; last != 5*time.Second {
		t.Fatalf("waited %v", last)
	}
}

func TestWriteTypesWaitsRemainder(t *testing.T) {
	f := &device.Fake{}
	kb, c := newTest(f)
	tb, _ := proto.ParseTable29(blank29(0))
	t25, _ := proto.ParseTable25(blank25(0))
	if err := kb.WriteButtons(tb); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(50 * time.Millisecond)
	if err := kb.WriteTypes(t25); err != nil {
		t.Fatal(err)
	}
	if len(c.sleeps) != 1 || c.sleeps[0] != 150*time.Millisecond {
		t.Fatalf("sleeps %v", c.sleeps)
	}
	// 200 ms have now passed: no more waiting.
	if err := kb.WriteTypes(t25); err != nil {
		t.Fatal(err)
	}
	if len(c.sleeps) != 1 {
		t.Fatalf("sleeps %v", c.sleeps)
	}
}

func TestWriteTypesWaits(t *testing.T) {
	f := &device.Fake{}
	kb, c := newTest(f)
	tb, _ := proto.ParseTable29(blank29(0))
	t25, _ := proto.ParseTable25(blank25(0))
	_ = kb.WriteButtons(tb)
	if err := kb.WriteTypes(t25); err != nil {
		t.Fatal(err)
	}
	if len(c.sleeps) != 1 || c.sleeps[0] != 200*time.Millisecond {
		t.Fatalf("sleeps %v", c.sleeps)
	}
	if got := f.Features(); !bytes.Equal(got[1], t25.Bytes()) {
		t.Fatal("table not sent")
	}
}

func TestWriteTypesWithoutButtonsDoesNotWait(t *testing.T) {
	f := &device.Fake{}
	kb, c := newTest(f)
	t25, _ := proto.ParseTable25(blank25(0))
	if err := kb.WriteTypes(t25); err != nil {
		t.Fatal(err)
	}
	if len(c.sleeps) != 0 {
		t.Fatalf("sleeps %v", c.sleeps)
	}
}

func ready() []byte { return []byte{0x05, 0x01, 0, 0} }

func TestReadButtons(t *testing.T) {
	f := &device.Fake{}
	f.QueueFeature(ready(), blank29(2))
	kb, c := newTest(f)
	tb, err := kb.ReadButtons(2)
	if err != nil || tb.Profile() != 2 {
		t.Fatalf("%v", err)
	}
	if got := f.Features(); len(got) != 1 || !bytes.Equal(got[0], proto.Select(2, proto.SelButtons)) {
		t.Fatalf("%x", got)
	}
	if len(c.sleeps) != 1 || c.sleeps[0] != 150*time.Millisecond {
		t.Fatalf("sleeps %v", c.sleeps)
	}
}

func TestReadButtonsNotReady(t *testing.T) {
	f := &device.Fake{}
	f.QueueFeature([]byte{0x05, 0x00, 0, 0}, blank29(2))
	kb, _ := newTest(f)
	_, err := kb.ReadButtons(2)
	if err == nil || !strings.Contains(err.Error(), "05 00") {
		t.Fatalf("got %v", err)
	}
	for _, c := range f.Calls() {
		if c.Op == device.OpGetFeature && c.ID == 0x29 {
			t.Fatal("table read after refused reply")
		}
	}
}

func TestReadButtonsWrongProfile(t *testing.T) {
	f := &device.Fake{}
	f.QueueFeature(ready(), blank29(3))
	kb, _ := newTest(f)
	if _, err := kb.ReadButtons(2); err == nil {
		t.Fatal("wrong profile accepted")
	}
}

func TestReadTypes(t *testing.T) {
	f := &device.Fake{}
	f.QueueFeature(ready(), blank25(4))
	kb, _ := newTest(f)
	tb, err := kb.ReadTypes(4)
	if err != nil || tb.Profile() != 4 {
		t.Fatalf("%v", err)
	}
	if got := f.Features(); !bytes.Equal(got[0], proto.Select(4, proto.SelTypes)) {
		t.Fatalf("%x", got)
	}
}

func TestReadTypesWrongProfile(t *testing.T) {
	f := &device.Fake{}
	f.QueueFeature(ready(), blank25(1))
	kb, _ := newTest(f)
	if _, err := kb.ReadTypes(4); err == nil {
		t.Fatal("wrong profile accepted")
	}
}

func TestProfileNames(t *testing.T) {
	f := &device.Fake{}
	r := make([]byte, 104)
	r[0] = 0x16
	copy(r[2:], "Alpha")
	copy(r[22:], "Beta")
	copy(r[42:], strings.Repeat("c", 20))
	r[62] = 'D'
	r[64] = 'x' // after the NUL: ignored
	f.QueueFeature(ready(), r)
	kb, _ := newTest(f)
	names, err := kb.ProfileNames()
	if err != nil {
		t.Fatal(err)
	}
	want := [5]string{"Alpha", "Beta", strings.Repeat("c", 20), "D", ""}
	if names != want {
		t.Fatalf("%q", names)
	}
	if got := f.Features(); !bytes.Equal(got[0], proto.Select(0, proto.SelNames)) {
		t.Fatalf("%x", got)
	}
}

func TestReadDisconnected(t *testing.T) {
	f := &device.Fake{}
	f.FailAt(device.OpSetFeature, 1, device.ErrDisconnected)
	kb, _ := newTest(f)
	if _, err := kb.ReadButtons(0); !errors.Is(err, device.ErrDisconnected) {
		t.Fatalf("got %v", err)
	}
}

// switcher answers profile switches as the keyboard does: a switch to
// another profile is confirmed, a switch to the active one is not.
type switcher struct {
	*device.Fake
	active byte
}

func (s *switcher) SetFeature(b []byte) error {
	err := s.Fake.SetFeature(b)
	if len(b) == 4 && b[0] == 0x06 {
		if p := b[2]; p != s.active {
			s.active = p
			s.Fake.QueueInput([]byte{0x03, 0x00, 0x01, p})
		}
	}
	return err
}

func TestSwitchToTheActiveProfileIsNotConfirmed(t *testing.T) {
	s := &switcher{Fake: &device.Fake{}, active: 2}
	kb, _ := newTest(s)
	if err := kb.SwitchProfile(2); !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("got %v, want a timeout", err)
	}
}

func TestSwitchProfileFresh(t *testing.T) {
	for _, c := range []struct {
		name          string
		active, p     byte
		first, second byte
	}{
		{"to the active profile", 2, 2, 3, 2},
		{"to another profile", 0, 2, 3, 2},
		{"to the last profile", 4, 4, 0, 4},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := &switcher{Fake: &device.Fake{}, active: c.active}
			kb, _ := newTest(s)
			if err := kb.SwitchProfileFresh(c.p); err != nil {
				t.Fatal(err)
			}
			got := s.Fake.Features()
			if len(got) != 2 || !bytes.Equal(got[0], proto.SwitchProfile(c.first)) || !bytes.Equal(got[1], proto.SwitchProfile(c.second)) {
				t.Fatalf("frames %x", got)
			}
			if s.active != c.p {
				t.Fatalf("profile %d is active, want %d", s.active, c.p)
			}
		})
	}
}

func TestSwitchProfileFreshStopsWhenTheFirstSwitchFails(t *testing.T) {
	f := &device.Fake{}
	kb, _ := newTest(f)
	if err := kb.SwitchProfileFresh(1); !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("got %v", err)
	}
	if got := f.Features(); len(got) != 1 || !bytes.Equal(got[0], proto.SwitchProfile(2)) {
		t.Fatalf("frames %x", got)
	}
}

func TestSwitchProfileFreshRefusesUnknownProfile(t *testing.T) {
	f := &device.Fake{}
	kb, _ := newTest(f)
	if err := kb.SwitchProfileFresh(5); err == nil {
		t.Fatal("no error")
	}
	if got := f.Features(); len(got) != 0 {
		t.Fatalf("frames %x", got)
	}
}

func TestWaitRedraw(t *testing.T) {
	f := &device.Fake{}
	f.QueueInput(
		[]byte{0x03, 0x00, 0x31, 0x01, 0x03}, // one position: not the answer
		[]byte{0x03, 0x00, 0x31, 0x20},
	)
	s := &spy{Fake: f}
	kb, _ := newTest(s)
	if err := kb.WaitRedraw(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	if s.inputTimeouts[0] != 5*time.Second {
		t.Fatalf("waited %v", s.inputTimeouts[0])
	}
}

func TestWaitRedrawTimeout(t *testing.T) {
	f := &device.Fake{}
	f.QueueInput([]byte{0x03, 0x00, 0x31, 0x01, 0x03})
	kb, _ := newTest(f)
	if err := kb.WaitRedraw(5 * time.Second); !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("got %v", err)
	}
}

func TestWriteTypesDropsOldInput(t *testing.T) {
	f := &device.Fake{}
	f.QueueInput([]byte{0x03, 0x00, 0x31, 0x20}) // an older redraw request
	kb, _ := newTest(f)
	t25, _ := proto.ParseTable25(blank25(1))
	if err := kb.WriteTypes(t25); err != nil {
		t.Fatal(err)
	}
	if err := kb.WaitRedraw(5 * time.Second); !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("the older redraw request was taken for the answer: %v", err)
	}
}
