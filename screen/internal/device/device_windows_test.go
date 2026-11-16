package device

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// These tests run the overlapped I/O of the Windows device on named pipes,
// which stand in for the HID collections: the device holds one end, the
// test plays the keyboard on the other.

var pipeSeq atomic.Int32

// keyboardEnd is the test's end of a pipe.
type keyboardEnd struct {
	h    windows.Handle
	once sync.Once
}

func (k *keyboardEnd) close() {
	k.once.Do(func() { windows.CloseHandle(k.h) })
}

// pipe returns the device's end of a new pipe, opened for overlapped I/O
// like a HID collection, and the keyboard's end.
func pipe(t *testing.T) (windows.Handle, *keyboardEnd) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(fmt.Sprintf(`\\.\pipe\kb7screen-device-test-%d-%d`, os.Getpid(), pipeSeq.Add(1)))
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateNamedPipe(name, windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_OVERLAPPED,
		windows.PIPE_TYPE_MESSAGE|windows.PIPE_READMODE_MESSAGE|windows.PIPE_WAIT, 1, 4096, 4096, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	kb := &keyboardEnd{h: h}
	t.Cleanup(kb.close)
	dev, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The device's end is already open, so the pipe is normally connected.
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(ev)
	ov := &windows.Overlapped{HEvent: ev}
	switch err := windows.ConnectNamedPipe(kb.h, ov); err {
	case nil, windows.ERROR_PIPE_CONNECTED:
	case windows.ERROR_IO_PENDING:
		var n uint32
		if err := windows.GetOverlappedResult(kb.h, ov, &n, true); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal(err)
	}
	return dev, kb
}

// kbIO runs one overlapped operation on the keyboard's end and waits for it.
func kbIO(t *testing.T, kb *keyboardEnd, start func(*windows.Overlapped) error) uint32 {
	t.Helper()
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(ev)
	ov := &windows.Overlapped{HEvent: ev}
	if err := start(ov); err != nil && err != windows.ERROR_IO_PENDING {
		t.Fatal(err)
	}
	if r, err := windows.WaitForSingleObject(ev, 5000); err != nil || r != windows.WAIT_OBJECT_0 {
		windows.CancelIoEx(kb.h, ov)
		var n uint32
		windows.GetOverlappedResult(kb.h, ov, &n, true)
		t.Fatalf("keyboard side: wait = %#x, %v", r, err)
	}
	var n uint32
	if err := windows.GetOverlappedResult(kb.h, ov, &n, false); err != nil {
		t.Fatal(err)
	}
	return n
}

func kbWrite(t *testing.T, kb *keyboardEnd, b []byte) {
	t.Helper()
	var done uint32
	kbIO(t, kb, func(ov *windows.Overlapped) error { return windows.WriteFile(kb.h, b, &done, ov) })
}

func kbRead(t *testing.T, kb *keyboardEnd, size int) []byte {
	t.Helper()
	buf := make([]byte, size)
	var done uint32
	n := kbIO(t, kb, func(ov *windows.Overlapped) error { return windows.ReadFile(kb.h, buf, &done, ov) })
	return buf[:n]
}

type testKB struct {
	d                     *winDevice
	stream, notify, touch *keyboardEnd
}

func newTestKB(t *testing.T) *testKB {
	t.Helper()
	sDev, stream := pipe(t)
	fDev, _ := pipe(t)
	nDev, notify := pipe(t)
	tDev, touch := pipe(t)
	d, err := newWinDevice(sDev, fDev, 1044, inputCollection{nDev, 8}, inputCollection{tDev, 12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &testKB{d, stream, notify, touch}
}

func report(size int, b ...byte) []byte {
	r := make([]byte, size)
	copy(r, b)
	return r
}

func TestWinStream(t *testing.T) {
	k := newTestKB(t)
	if err := k.d.WriteStream(frame(0xa5, 0x41)); err != nil {
		t.Fatal(err)
	}
	if got := kbRead(t, k.stream, 128); !bytes.Equal(got, report(65, 0x00, 0xa5, 0x41)) {
		t.Fatalf("keyboard got % x", got)
	}
	kbWrite(t, k.stream, report(65, 0x00, 0xa5, 0x41))
	got, err := k.d.ReadStream(time.Second)
	if err != nil || got != frame(0xa5, 0x41) {
		t.Fatalf("ReadStream = % x, %v", got[:4], err)
	}
}

func TestWinReadStreamTimeout(t *testing.T) {
	k := newTestKB(t)
	for range 3 {
		start := time.Now()
		if _, err := k.d.ReadStream(30 * time.Millisecond); err != ErrTimeout {
			t.Fatalf("ReadStream on silence: %v, want ErrTimeout", err)
		}
		if d := time.Since(start); d < 25*time.Millisecond {
			t.Fatalf("timed out after %v", d)
		}
	}
	if _, err := k.d.ReadStream(0); err != ErrTimeout {
		t.Fatalf("ReadStream(0) on silence: %v, want ErrTimeout", err)
	}
	// The cancelled reads are finished: the next one gets the next frame.
	kbWrite(t, k.stream, report(65, 0x00, 0xa5, 0xf1))
	got, err := k.d.ReadStream(time.Second)
	if err != nil || got != frame(0xa5, 0xf1) {
		t.Fatalf("ReadStream after timeouts = % x, %v", got[:4], err)
	}
}

func TestWinReadInput(t *testing.T) {
	k := newTestKB(t)
	kbWrite(t, k.touch, report(12, 0x33, 0x01, 0x05, 0x05, 0x00, 0x00, 0x01, 0x01))
	r, err := k.d.ReadInput(time.Second)
	if err != nil || !bytes.Equal(r, report(12, 0x33, 0x01, 0x05, 0x05, 0x00, 0x00, 0x01, 0x01)) {
		t.Fatalf("touch = % x, %v", r, err)
	}
	// Anything but a notification or touch report is dropped.
	kbWrite(t, k.notify, report(8, 0x04, 0x00, 0x04))
	kbWrite(t, k.notify, report(8, 0x03, 0x00, 0x01, 0x01))
	r, err = k.d.ReadInput(time.Second)
	if err != nil || !bytes.Equal(r, report(8, 0x03, 0x00, 0x01, 0x01)) {
		t.Fatalf("notification = % x, %v", r, err)
	}
	if r, err := k.d.ReadInput(30 * time.Millisecond); err != ErrTimeout {
		t.Fatalf("silence: % x, %v; want ErrTimeout", r, err)
	}
}

func TestWinReadInputKeepsBoth(t *testing.T) {
	k := newTestKB(t)
	kbWrite(t, k.notify, report(8, 0x03, 0x00, 0xf1, 0x01))
	kbWrite(t, k.touch, report(12, 0x33, 0x01, 0x05))
	seen := map[byte]bool{}
	for range 2 {
		r, err := k.d.ReadInput(time.Second)
		if err != nil {
			t.Fatal(err)
		}
		seen[r[0]] = true
	}
	if !seen[0x03] || !seen[0x33] {
		t.Fatalf("got reports %v, want 0x03 and 0x33", seen)
	}
}

func TestWinCloseInterrupts(t *testing.T) {
	k := newTestKB(t)
	stream := make(chan error, 1)
	input := make(chan error, 1)
	go func() {
		_, err := k.d.ReadStream(time.Minute)
		stream <- err
	}()
	go func() {
		_, err := k.d.ReadInput(time.Minute)
		input <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if err := k.d.Close(); err != nil {
		t.Fatal(err)
	}
	for name, ch := range map[string]chan error{"ReadStream": stream, "ReadInput": input} {
		select {
		case err := <-ch:
			if err != ErrClosed {
				t.Errorf("%s interrupted by Close: %v, want ErrClosed", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Close did not interrupt %s", name)
		}
	}
	if err := k.d.SetFeature([]byte{0x0e}); err != ErrClosed {
		t.Errorf("SetFeature after Close: %v", err)
	}
	if _, err := k.d.GetFeature(0x05, 4); err != ErrClosed {
		t.Errorf("GetFeature after Close: %v", err)
	}
	if err := k.d.WriteStream(frame()); err != ErrClosed {
		t.Errorf("WriteStream after Close: %v", err)
	}
	if _, err := k.d.ReadStream(0); err != ErrClosed {
		t.Errorf("ReadStream after Close: %v", err)
	}
	if _, err := k.d.ReadInput(0); err != ErrClosed {
		t.Errorf("ReadInput after Close: %v", err)
	}
	if err := k.d.Close(); err != ErrClosed {
		t.Errorf("second Close: %v", err)
	}
}

// A keyboard that goes away ends ReadInput and ReadStream with an error,
// not with endless timeouts.
func TestWinDisconnect(t *testing.T) {
	k := newTestKB(t)
	k.touch.close()
	_, err := k.d.ReadInput(5 * time.Second)
	if err == nil || errors.Is(err, ErrTimeout) || errors.Is(err, ErrClosed) {
		t.Fatalf("ReadInput after unplug: %v, want a read error", err)
	}
	if _, err := k.d.ReadInput(0); err == nil || errors.Is(err, ErrTimeout) {
		t.Fatalf("second ReadInput after unplug: %v, want the read error again", err)
	}
	k.stream.close()
	if _, err := k.d.ReadStream(5 * time.Second); err == nil || errors.Is(err, ErrTimeout) {
		t.Fatalf("ReadStream after unplug: %v, want a read error", err)
	}
}

// featureCalls replaces HidD_SetFeature or HidD_GetFeature: it records the
// buffer of every call and fails the calls whose length is in fail.
type featureCalls struct {
	bufs  [][]byte
	fail  map[int]bool
	reply []byte // copied into the buffer of a successful call
}

func (f *featureCalls) call(_ windows.Handle, buf []byte) error {
	f.bufs = append(f.bufs, bytes.Clone(buf))
	if f.fail[len(buf)] {
		return windows.ERROR_INVALID_PARAMETER
	}
	copy(buf, f.reply)
	return nil
}

func (f *featureCalls) lens() []int {
	var n []int
	for _, b := range f.bufs {
		n = append(n, len(b))
	}
	return n
}

func TestWinSetFeature(t *testing.T) {
	report := []byte{0x0e, 0x05, 0x01, 0x00, 0x00}
	for _, tc := range []struct {
		name     string
		fail     map[int]bool
		wantLens []int
		wantErr  bool
	}{
		{"exact length works", nil, []int{5}, false},
		{"falls back to the full length", map[int]bool{5: true}, []int{5, 1044}, false},
		{"both fail", map[int]bool{5: true, 1044: true}, []int{5, 1044}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := newTestKB(t)
			calls := &featureCalls{fail: tc.fail}
			k.d.setFeature = calls.call
			err := k.d.SetFeature(report)
			if (err != nil) != tc.wantErr {
				t.Fatalf("SetFeature: %v", err)
			}
			if tc.wantErr && !errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
				t.Fatalf("error %v does not carry the first failure", err)
			}
			if got := calls.lens(); fmt.Sprint(got) != fmt.Sprint(tc.wantLens) {
				t.Fatalf("buffer lengths %v, want %v", got, tc.wantLens)
			}
			for _, b := range calls.bufs {
				if !bytes.Equal(b[:5], report) || !bytes.Equal(b[5:], make([]byte, len(b)-5)) {
					t.Fatalf("buffer % x, want the report padded with zeros", b[:8])
				}
			}
		})
	}
}

func TestWinSetFeatureFullLengthHasNoFallback(t *testing.T) {
	k := newTestKB(t)
	calls := &featureCalls{fail: map[int]bool{1044: true}}
	k.d.setFeature = calls.call
	if err := k.d.SetFeature(report(1044, 0x08)); err == nil {
		t.Fatal("SetFeature succeeded")
	}
	if got := calls.lens(); fmt.Sprint(got) != "[1044]" {
		t.Fatalf("buffer lengths %v, want one full-length call", got)
	}
	if err := k.d.SetFeature(report(1045, 0x08)); err == nil {
		t.Fatal("a report longer than the collection was sent")
	}
	if len(calls.bufs) != 1 {
		t.Fatal("an over-long report reached HidD_SetFeature")
	}
}

func TestWinGetFeature(t *testing.T) {
	reply := report(1044, 0x05, 0x01, 0xba, 0x00, 0x99)
	for _, tc := range []struct {
		name     string
		fail     map[int]bool
		wantLens []int
		wantErr  bool
	}{
		{"exact length works", nil, []int{4}, false},
		{"falls back to the full length", map[int]bool{4: true}, []int{4, 1044}, false},
		{"both fail", map[int]bool{4: true, 1044: true}, []int{4, 1044}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k := newTestKB(t)
			calls := &featureCalls{fail: tc.fail, reply: reply}
			k.d.getFeature = calls.call
			got, err := k.d.GetFeature(0x05, 4)
			if (err != nil) != tc.wantErr {
				t.Fatalf("GetFeature: %v", err)
			}
			if gotLens := calls.lens(); fmt.Sprint(gotLens) != fmt.Sprint(tc.wantLens) {
				t.Fatalf("buffer lengths %v, want %v", gotLens, tc.wantLens)
			}
			for _, b := range calls.bufs {
				if b[0] != 0x05 {
					t.Fatalf("buffer starts with %#x, want the report ID", b[0])
				}
			}
			if !tc.wantErr && !bytes.Equal(got, []byte{0x05, 0x01, 0xba, 0x00}) {
				t.Fatalf("GetFeature = % x", got)
			}
		})
	}
}
