// SPDX-License-Identifier: GPL-2.0-or-later

package device

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// These tests run the Linux device on socket pairs, which keep report
// boundaries the way hidraw does: the device holds one end, the test plays
// the keyboard on the other.

// keyboardSock is the test's end of a socket pair.
type keyboardSock struct {
	fd   int
	once sync.Once
}

func (k *keyboardSock) close() {
	k.once.Do(func() { unix.Close(k.fd) })
}

func (k *keyboardSock) write(t *testing.T, b []byte) {
	t.Helper()
	if _, err := unix.Write(k.fd, b); err != nil {
		t.Fatal(err)
	}
}

func (k *keyboardSock) read(t *testing.T) []byte {
	t.Helper()
	buf := make([]byte, 256)
	n, err := unix.Read(k.fd, buf)
	if err != nil {
		t.Fatal(err)
	}
	return buf[:n]
}

func socketPair(t *testing.T) (int, *keyboardSock) {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.SetNonblock(fds[0], true); err != nil {
		t.Fatal(err)
	}
	kb := &keyboardSock{fd: fds[1]}
	t.Cleanup(kb.close)
	return fds[0], kb
}

type linuxTestKB struct {
	d            *linuxDevice
	stream, ctrl *keyboardSock
}

func newLinuxTestKB(t *testing.T) *linuxTestKB {
	t.Helper()
	sDev, stream := socketPair(t)
	cDev, ctrl := socketPair(t)
	d, err := newLinuxDevice(sDev, cDev)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &linuxTestKB{d, stream, ctrl}
}

func report(size int, b ...byte) []byte {
	r := make([]byte, size)
	copy(r, b)
	return r
}

func TestLinuxStream(t *testing.T) {
	k := newLinuxTestKB(t)
	if err := k.d.WriteStream(frame(0xa5, 0x41)); err != nil {
		t.Fatal(err)
	}
	if got := k.stream.read(t); !bytes.Equal(got, report(65, 0x00, 0xa5, 0x41)) {
		t.Fatalf("keyboard got % x", got)
	}
	k.stream.write(t, report(64, 0xa5, 0x41))
	got, err := k.d.ReadStream(time.Second)
	if err != nil || got != frame(0xa5, 0x41) {
		t.Fatalf("ReadStream = % x, %v", got[:4], err)
	}
}

func TestLinuxReadStreamTimeout(t *testing.T) {
	k := newLinuxTestKB(t)
	start := time.Now()
	if _, err := k.d.ReadStream(30 * time.Millisecond); err != ErrTimeout {
		t.Fatalf("ReadStream on silence: %v, want ErrTimeout", err)
	}
	if d := time.Since(start); d < 25*time.Millisecond {
		t.Fatalf("timed out after %v", d)
	}
	if _, err := k.d.ReadStream(0); err != ErrTimeout {
		t.Fatalf("ReadStream(0) on silence: %v, want ErrTimeout", err)
	}
	k.stream.write(t, report(64, 0xa5, 0xf1))
	if got, err := k.d.ReadStream(time.Second); err != nil || got != frame(0xa5, 0xf1) {
		t.Fatalf("ReadStream after a timeout = % x, %v", got[:4], err)
	}
}

func TestLinuxReadStreamWrongLength(t *testing.T) {
	k := newLinuxTestKB(t)
	k.stream.write(t, report(65, 0x00, 0xa5))
	if _, err := k.d.ReadStream(time.Second); err == nil {
		t.Fatal("a 65-byte stream report was taken as a frame")
	}
}

func TestLinuxReadInput(t *testing.T) {
	k := newLinuxTestKB(t)
	k.ctrl.write(t, report(21, 0x04, 0x00, 0x00, 0x04)) // keyboard
	k.ctrl.write(t, report(4, 0x01, 0xe9))              // media key
	k.ctrl.write(t, report(12, 0x33, 0x01, 0x05, 0x05, 0x00, 0x00, 0x01, 0x01))
	k.ctrl.write(t, report(8, 0x03, 0x00, 0x01, 0x01))
	r, err := k.d.ReadInput(time.Second)
	if err != nil || !bytes.Equal(r, report(12, 0x33, 0x01, 0x05, 0x05, 0x00, 0x00, 0x01, 0x01)) {
		t.Fatalf("first ReadInput = % x, %v; want the touch report", r, err)
	}
	r, err = k.d.ReadInput(time.Second)
	if err != nil || !bytes.Equal(r, report(8, 0x03, 0x00, 0x01, 0x01)) {
		t.Fatalf("second ReadInput = % x, %v; want the notification", r, err)
	}
	if r, err := k.d.ReadInput(30 * time.Millisecond); err != ErrTimeout {
		t.Fatalf("silence: % x, %v; want ErrTimeout", r, err)
	}
}

func TestLinuxCloseInterrupts(t *testing.T) {
	k := newLinuxTestKB(t)
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

// A keyboard that goes away ends ReadInput and ReadStream with
// ErrDisconnected, not with endless timeouts. A closed socket shows a
// hang-up or end of file, as hidraw does.
func TestLinuxDisconnect(t *testing.T) {
	k := newLinuxTestKB(t)
	k.ctrl.close()
	if _, err := k.d.ReadInput(5 * time.Second); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("ReadInput after unplug: %v, want ErrDisconnected", err)
	}
	if _, err := k.d.ReadInput(0); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("second ReadInput after unplug: %v, want ErrDisconnected", err)
	}
	k.stream.close()
	if _, err := k.d.ReadStream(5 * time.Second); !errors.Is(err, ErrDisconnected) {
		t.Fatalf("ReadStream after unplug: %v, want ErrDisconnected", err)
	}
}

func TestLinuxReadiness(t *testing.T) {
	for _, tc := range []struct {
		revents  int16
		readable bool
		err      error
	}{
		{0, false, nil},
		{unix.POLLIN, true, nil},
		{unix.POLLIN | unix.POLLHUP | unix.POLLERR, true, nil}, // buffered reports first
		{unix.POLLHUP, false, errHangup},
		{unix.POLLERR, false, errHangup},
		{unix.POLLOUT | unix.POLLERR | unix.POLLHUP, false, errHangup},
		{unix.POLLNVAL, false, unix.EBADF},
	} {
		readable, err := readiness(tc.revents)
		if readable != tc.readable || err != tc.err {
			t.Errorf("readiness(%#x) = %v, %v; want %v, %v", tc.revents, readable, err, tc.readable, tc.err)
		}
	}
}

func TestLinuxIoErr(t *testing.T) {
	for _, tc := range []struct {
		err          error
		disconnected bool
	}{
		{errHangup, true},
		{unix.EIO, true},
		{unix.ENODEV, true},
		{fmt.Errorf("wrapped: %w", unix.ENODEV), true},
		{unix.EPIPE, false},  // the keyboard refused a request
		{unix.EBADF, false},  // a fault here
		{unix.EINVAL, false}, // a bad request
	} {
		err := ioErr("read input", tc.err)
		if errors.Is(err, ErrDisconnected) != tc.disconnected {
			t.Errorf("ioErr(%v) = %v, disconnected %v", tc.err, err, !tc.disconnected)
		}
		if !errors.Is(err, tc.err) {
			t.Errorf("ioErr(%v) = %v, lost the cause", tc.err, err)
		}
		if errors.Is(err, ErrTimeout) || errors.Is(err, ErrClosed) {
			t.Errorf("ioErr(%v) = %v", tc.err, err)
		}
	}
	if err := ioErr("get feature report 0x05", unix.ETIMEDOUT); err != ErrTimeout {
		t.Errorf("ETIMEDOUT: %v, want ErrTimeout", err)
	}
	if err := ioErr("read stream", ErrTimeout); err != ErrTimeout {
		t.Errorf("ErrTimeout: %v", err)
	}
	if err := ioErr("read stream", ErrClosed); err != ErrClosed {
		t.Errorf("ErrClosed: %v", err)
	}
}

// The feature ioctls reach the kernel: a socket refuses them as an unknown
// ioctl (ENOTTY), and that error number comes back.
func TestLinuxFeatureIoctl(t *testing.T) {
	k := newLinuxTestKB(t)
	var errno unix.Errno
	if err := k.d.SetFeature([]byte{0x0e, 0x05, 0x01, 0x00, 0x00}); !errors.As(err, &errno) {
		t.Fatalf("SetFeature on a socket: %v, want the kernel's error", err)
	}
	if _, err := k.d.GetFeature(0x05, 4); !errors.As(err, &errno) {
		t.Fatalf("GetFeature on a socket: %v, want the kernel's error", err)
	}
	if err := k.d.SetFeature(make([]byte, maxIoctlLen+1)); err == nil || errors.As(err, &errno) {
		t.Fatalf("over-long SetFeature: %v, want a length error before the ioctl", err)
	}
}
