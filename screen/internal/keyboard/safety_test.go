// SPDX-License-Identifier: GPL-2.0-or-later

package keyboard

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/device"
	"github.com/Bananenbaas/commander/screen/internal/proto"
)

// tickSpy advances the clock by step on every read and answers each with the
// given report or a junk frame, so that nothing ever completes.
type tickSpy struct {
	*device.Fake
	c              *clock
	step           time.Duration
	input          []byte
	inputTimeouts  []time.Duration
	streamTimeouts []time.Duration
	streamReads    int
}

func (s *tickSpy) ReadInput(d time.Duration) ([]byte, error) {
	s.inputTimeouts = append(s.inputTimeouts, d)
	s.c.t = s.c.t.Add(s.step)
	return s.input, nil
}

func (s *tickSpy) ReadStream(d time.Duration) ([64]byte, error) {
	s.streamReads++
	if s.streamReads == 1 {
		return s.Fake.ReadStream(d) // the drain: quiet
	}
	s.streamTimeouts = append(s.streamTimeouts, d)
	s.c.t = s.c.t.Add(s.step)
	var junk [64]byte
	junk[0] = 0x12
	return junk, nil
}

func checkShrinking(t *testing.T, got []time.Duration) {
	t.Helper()
	want := []time.Duration{5 * time.Second, 3 * time.Second, time.Second}
	if len(got) != len(want) {
		t.Fatalf("timeouts %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("timeouts %v, want %v", got, want)
		}
	}
}

func TestUploadSkippedFramesDoNotExtendDeadline(t *testing.T) {
	c := &clock{t: time.Unix(1000, 0)}
	s := &tickSpy{Fake: &device.Fake{}, c: c, step: 2 * time.Second}
	kb := newKB(s, c.now, c.sleep)
	if err := kb.Upload(1, picture()); !errors.Is(err, ErrUploadLeftOpen) {
		t.Fatalf("got %v", err)
	}
	checkShrinking(t, s.streamTimeouts)
}

func TestSkippedEventsDoNotExtendDeadline(t *testing.T) {
	c := &clock{t: time.Unix(1000, 0)}
	s := &tickSpy{Fake: &device.Fake{}, c: c, step: 2 * time.Second, input: []byte{0x03, 0x00, 0x99, 0x00}}
	kb := newKB(s, c.now, c.sleep)
	if _, err := kb.NextEvent(5 * time.Second); !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("got %v", err)
	}
	checkShrinking(t, s.inputTimeouts)
}

func TestDiscardedEventsDoNotExtendDeadline(t *testing.T) {
	c := &clock{t: time.Unix(1000, 0)}
	s := &tickSpy{Fake: &device.Fake{}, c: c, step: 2 * time.Second, input: []byte{0x03, 0x00, 0x01, 0x00}}
	kb := newKB(s, c.now, c.sleep)
	if err := kb.EnableNotifications(); !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("got %v", err)
	}
	checkShrinking(t, s.inputTimeouts)
}

func TestUploadOpenerWriteTimeoutIsLeftOpen(t *testing.T) {
	f := &device.Fake{Echo: true}
	f.FailAt(device.OpWriteStream, 1, device.ErrTimeout)
	kb, _ := newTest(f)
	err := kb.Upload(1, picture())
	if !errors.Is(err, ErrUploadLeftOpen) || !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("got %v", err)
	}
	reads := 0
	for _, c := range f.Calls() {
		if c.Op == device.OpReadStream {
			reads++
		}
	}
	if reads != 1 { // only the drain
		t.Fatalf("%d stream reads, want only the drain", reads)
	}
}

func TestUploadOpenerWriteDisconnectedIsPlain(t *testing.T) {
	f := &device.Fake{Echo: true}
	f.FailAt(device.OpWriteStream, 1, device.ErrDisconnected)
	kb, _ := newTest(f)
	err := kb.Upload(1, picture())
	if !errors.Is(err, device.ErrDisconnected) || errors.Is(err, ErrUploadLeftOpen) {
		t.Fatalf("got %v", err)
	}
}

func TestUploadComparesFullFrame(t *testing.T) {
	frames, _ := proto.UploadFrames(1, picture())
	// Opener echo differing only in byte 63.
	bad := frames[0].Ack
	bad[63] ^= 1
	s := &spy{Fake: &device.Fake{Echo: true}, junk: bad, junkAt: 2}
	kb, _ := newTest(s)
	if err := kb.Upload(1, picture()); !errors.Is(err, ErrOpenerRefused) {
		t.Fatalf("opener: got %v", err)
	}
	// Block-open echo differing only in byte 63.
	bad = frames[1].Ack
	bad[63] ^= 1
	f := &device.Fake{Echo: true}
	s = &spy{Fake: f, junk: bad, junkAt: 3}
	kb, _ = newTest(s)
	if err := kb.Upload(1, picture()); !errors.Is(err, ErrUploadLeftOpen) {
		t.Fatalf("block open: got %v", err)
	}
	if n := len(f.Frames()); n != 2 {
		t.Fatalf("%d frames sent", n)
	}
}

func TestUploadRefusesBusyStream(t *testing.T) {
	f := &device.Fake{Echo: true}
	var fr [64]byte
	fr[0] = 0xa5
	for i := 0; i < 70; i++ {
		f.QueueStream(fr)
	}
	kb, _ := newTest(f)
	err := kb.Upload(1, picture())
	if err == nil || errors.Is(err, ErrUploadLeftOpen) || !strings.Contains(err.Error(), "not quiet") {
		t.Fatalf("got %v", err)
	}
	if len(f.Frames()) != 0 {
		t.Fatal("frames sent")
	}
}

func TestSwitchProfileRefusesBusyInput(t *testing.T) {
	f := &device.Fake{}
	for i := 0; i < 70; i++ {
		f.QueueInput([]byte{0x03, 0x00, 0x99, 0x00})
	}
	kb, _ := newTest(f)
	if err := kb.SwitchProfile(1); err == nil || !strings.Contains(err.Error(), "not quiet") {
		t.Fatalf("got %v", err)
	}
	if len(f.Features()) != 0 {
		t.Fatal("frame sent")
	}
}

func TestUploadLatchAfterLeftOpen(t *testing.T) {
	f := &device.Fake{}
	kb, _ := newTest(f)
	if err := kb.Upload(1, picture()); !errors.Is(err, ErrUploadLeftOpen) {
		t.Fatalf("got %v", err)
	}
	n := len(f.Calls())
	if err := kb.Upload(1, picture()); !errors.Is(err, ErrUploadLeftOpen) {
		t.Fatalf("second: got %v", err)
	}
	if len(f.Calls()) != n {
		t.Fatal("device touched after a left-open upload")
	}
}

func TestRefusedOpenerMessageHasReply(t *testing.T) {
	var wrong [64]byte
	wrong[0], wrong[1] = 0xa5, 0x42
	s := &spy{Fake: &device.Fake{}, junk: wrong, junkAt: 2}
	kb, _ := newTest(s)
	err := kb.Upload(1, picture())
	if !errors.Is(err, ErrOpenerRefused) || !strings.Contains(err.Error(), "a5 42") {
		t.Fatalf("got %v", err)
	}
}

func TestSettleBeforeOtherReports(t *testing.T) {
	f := &device.Fake{}
	kb, c := newTest(f)
	tb, _ := proto.ParseTable29(blank29(0))
	_ = kb.WriteButtons(tb)
	_ = kb.SwitchProfile(1) // times out; only the pause matters here
	if len(c.sleeps) == 0 || c.sleeps[0] != 200*time.Millisecond {
		t.Fatalf("sleeps %v", c.sleeps)
	}
	f.QueueFeature(ready(), blank29(0))
	c.sleeps = nil
	_ = kb.WriteButtons(tb)
	_, _ = kb.ReadButtons(0)
	if len(c.sleeps) < 1 || c.sleeps[0] != 200*time.Millisecond {
		t.Fatalf("read did not wait: %v", c.sleeps)
	}
}
