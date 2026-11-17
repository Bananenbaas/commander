// SPDX-License-Identifier: GPL-2.0-or-later

package device

import (
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	hidrawClass = "/sys/class/hidraw"

	// streamWriteTimeout bounds a stream write the kernel keeps retrying;
	// the kernel itself gives up on an interrupt transfer after 5 s.
	streamWriteTimeout = 5 * time.Second
	// maxPollMillis is the longest finite poll.
	maxPollMillis = 1<<31 - 1
	// frameLen is the length of a stream frame; the stream has no report
	// ID, so hidraw delivers frames without one.
	frameLen = 64
	// readBufLen holds the longest input report of interface 2.
	readBufLen = 256
)

// errHangup is the node reporting a hang-up or error through poll, or an
// end of file; ioErr turns it into ErrDisconnected.
var errHangup = errors.New("hang-up")

// readiness interprets the poll events of a hidraw node. Reports still
// buffered are read before a hang-up counts: hidraw keeps POLLIN set for
// them after the keyboard is gone. POLLNVAL means the descriptor is not
// open, a fault here rather than an unplugged keyboard.
func readiness(revents int16) (readable bool, err error) {
	switch {
	case revents&unix.POLLIN != 0:
		return true, nil
	case revents&(unix.POLLERR|unix.POLLHUP) != 0:
		return false, errHangup
	case revents&unix.POLLNVAL != 0:
		return false, unix.EBADF
	}
	return false, nil
}

// ioErr names the operation that failed and sorts the error into
// ErrTimeout, ErrClosed, ErrDisconnected or anything else. The kernel
// reports a removed hidraw device with EIO (read) or ENODEV (write, ioctl),
// and a USB transfer that got no answer with ETIMEDOUT.
func ioErr(op string, err error) error {
	switch {
	case errors.Is(err, ErrTimeout), errors.Is(err, unix.ETIMEDOUT):
		return ErrTimeout
	case errors.Is(err, ErrClosed):
		return ErrClosed
	case errors.Is(err, errHangup), errors.Is(err, unix.EIO), errors.Is(err, unix.ENODEV):
		return disconnected(op, err)
	}
	return fmt.Errorf("device: %s: %w", op, err)
}

// Open finds the KB7's hidraw nodes for interfaces 1 and 2 and opens them.
// The user needs read and write access to both, normally through a udev
// rule.
func Open() (Device, error) {
	nodes, err := findHidraw(hidrawClass, filepath.EvalSymlinks)
	if err != nil {
		return nil, err
	}
	streamPath, ctrlPath, err := pickNodes(nodes)
	if err != nil {
		return nil, err
	}
	stream, err := openNode(streamPath)
	if err != nil {
		return nil, err
	}
	ctrl, err := openNode(ctrlPath)
	if err != nil {
		unix.Close(stream)
		return nil, err
	}
	return newLinuxDevice(stream, ctrl)
}

func openNode(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, unix.EACCES) {
			return -1, fmt.Errorf("device: open %s: %w (a udev rule must give this user access)", path, err)
		}
		return -1, fmt.Errorf("device: open %s: %w", path, err)
	}
	return fd, nil
}

type linuxDevice struct {
	stream, ctrl int // hidraw nodes of interfaces 1 and 2
	wake         int // eventfd; Close makes it readable to end every poll

	ioMu   sync.Mutex // one stream or feature operation at a time
	closed atomic.Bool
	in     *inputQueue
	wg     sync.WaitGroup // the input reader
}

// newLinuxDevice takes over the open nodes and starts the reader that
// drains interface 2. On error it closes them.
func newLinuxDevice(stream, ctrl int) (*linuxDevice, error) {
	wake, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		unix.Close(stream)
		unix.Close(ctrl)
		return nil, fmt.Errorf("device: eventfd: %w", err)
	}
	d := &linuxDevice{stream: stream, ctrl: ctrl, wake: wake, in: newInputQueue()}
	d.wg.Add(1)
	go d.readInput()
	return d, nil
}

// readInput drains interface 2 for as long as the device is open: it queues
// the notification and touch reports and drops everything else, which is
// keyboard and media key input, unkept.
func (d *linuxDevice) readInput() {
	defer d.wg.Done()
	buf := make([]byte, readBufLen)
	for {
		n, err := d.read(d.ctrl, buf, -1)
		if errors.Is(err, ErrClosed) {
			return
		}
		if err != nil {
			d.in.fail(ioErr("read input", err))
			return
		}
		var r []byte
		if isInputReport(buf[:n]) {
			r = append(r, buf[:n]...)
		}
		clear(buf[:n])
		if r != nil && !d.in.put(r) {
			return
		}
	}
}

// read waits until fd has a report, then reads it into buf. A negative
// timeout waits without limit. It returns ErrTimeout, ErrClosed once Close
// has been called, errHangup, or the error of poll or read.
func (d *linuxDevice) read(fd int, buf []byte, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for {
		ms := -1
		if timeout >= 0 {
			ms = int(millis(time.Until(deadline), maxPollMillis))
		}
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}, {Fd: int32(d.wake), Events: unix.POLLIN}}
		if _, err := unix.Poll(fds, ms); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return 0, err
		}
		if fds[1].Revents != 0 {
			return 0, ErrClosed
		}
		readable, err := readiness(fds[0].Revents)
		if err != nil {
			return 0, err
		}
		if readable {
			n, err := unix.Read(fd, buf)
			switch {
			case errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR):
				continue
			case err != nil:
				return 0, err
			case n == 0:
				return 0, errHangup
			}
			return n, nil
		}
		if timeout >= 0 && !time.Now().Before(deadline) {
			return 0, ErrTimeout
		}
	}
}

// lock takes the I/O lock, or returns ErrClosed.
func (d *linuxDevice) lock() error {
	if d.closed.Load() {
		return ErrClosed
	}
	d.ioMu.Lock()
	if d.closed.Load() {
		d.ioMu.Unlock()
		return ErrClosed
	}
	return nil
}

// ioctl runs a hidraw ioctl whose argument is buf.
func ioctl(fd int, req uint32, buf []byte) (int, error) {
	r, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(req), uintptr(unsafe.Pointer(&buf[0])))
	if e != 0 {
		return 0, e
	}
	return int(r), nil
}

func (d *linuxDevice) SetFeature(b []byte) error {
	if err := checkFeatureLen(len(b), maxIoctlLen); err != nil {
		return err
	}
	if err := d.lock(); err != nil {
		return err
	}
	defer d.ioMu.Unlock()
	buf := append([]byte(nil), b...)
	if _, err := ioctl(d.ctrl, hidioc(hidiocSFeature, len(buf)), buf); err != nil {
		return ioErr(fmt.Sprintf("set feature report 0x%02x", b[0]), err)
	}
	return nil
}

func (d *linuxDevice) GetFeature(id byte, n int) ([]byte, error) {
	if err := checkFeatureLen(n, maxIoctlLen); err != nil {
		return nil, err
	}
	if err := d.lock(); err != nil {
		return nil, err
	}
	defer d.ioMu.Unlock()
	buf := make([]byte, n)
	buf[0] = id
	if _, err := ioctl(d.ctrl, hidioc(hidiocGFeature, n), buf); err != nil {
		return nil, ioErr(fmt.Sprintf("get feature report 0x%02x", id), err)
	}
	return buf, nil
}

func (d *linuxDevice) WriteStream(f [64]byte) error {
	if err := d.lock(); err != nil {
		return err
	}
	defer d.ioMu.Unlock()
	// hidraw takes the report ID first; 0 because the stream has none, and
	// the kernel sends the 64 bytes after it.
	var buf [1 + frameLen]byte
	copy(buf[1:], f[:])
	deadline := time.Now().Add(streamWriteTimeout)
	for {
		n, err := unix.Write(d.stream, buf[:])
		switch {
		case errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR):
			if !time.Now().Before(deadline) {
				return ErrTimeout
			}
			if err := d.waitWritable(deadline); err != nil {
				return ioErr("write stream", err)
			}
			continue
		case err != nil:
			return ioErr("write stream", err)
		case n != len(buf):
			return fmt.Errorf("device: wrote %d of %d stream bytes", n, len(buf))
		}
		return nil
	}
}

// waitWritable polls the stream node for writing until deadline.
func (d *linuxDevice) waitWritable(deadline time.Time) error {
	fds := []unix.PollFd{{Fd: int32(d.stream), Events: unix.POLLOUT}, {Fd: int32(d.wake), Events: unix.POLLIN}}
	if _, err := unix.Poll(fds, int(millis(time.Until(deadline), maxPollMillis))); err != nil && !errors.Is(err, unix.EINTR) {
		return err
	}
	if fds[1].Revents != 0 {
		return ErrClosed
	}
	if fds[0].Revents&(unix.POLLERR|unix.POLLHUP) != 0 {
		return errHangup
	}
	return nil
}

func (d *linuxDevice) ReadStream(timeout time.Duration) ([64]byte, error) {
	var f [64]byte
	if err := d.lock(); err != nil {
		return f, err
	}
	defer d.ioMu.Unlock()
	if timeout < 0 {
		timeout = 0
	}
	buf := make([]byte, readBufLen)
	n, err := d.read(d.stream, buf, timeout)
	switch {
	case err != nil:
		return f, ioErr("read stream", err)
	case n != frameLen:
		return f, fmt.Errorf("device: stream report of %d bytes, want %d", n, frameLen)
	}
	copy(f[:], buf)
	return f, nil
}

func (d *linuxDevice) ReadInput(timeout time.Duration) ([]byte, error) {
	if d.closed.Load() {
		return nil, ErrClosed
	}
	return d.in.get(timeout)
}

func (d *linuxDevice) Close() error {
	if !d.closed.CompareAndSwap(false, true) {
		return ErrClosed
	}
	var one [8]byte
	binary.NativeEndian.PutUint64(one[:], 1)
	unix.Write(d.wake, one[:]) // ends every poll
	d.in.close()
	d.wg.Wait()
	d.ioMu.Lock()
	defer d.ioMu.Unlock()
	var first error
	for _, fd := range []int{d.stream, d.ctrl, d.wake} {
		if err := unix.Close(fd); err != nil && first == nil {
			first = err
		}
	}
	if first != nil {
		return fmt.Errorf("device: close: %w", first)
	}
	return nil
}
