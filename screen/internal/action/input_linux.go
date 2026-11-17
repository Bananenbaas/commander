// SPDX-License-Identifier: GPL-2.0-or-later

package action

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	evSyn = 0x00
	evKey = 0x01

	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502
	uiDevSetup   = 0x405c5503 // _IOW('U', 3, struct uinput_setup)
	uiSetEvBit   = 0x40045564 // _IOW('U', 100, int)
	uiSetKeyBit  = 0x40045565 // _IOW('U', 101, int)

	busUSB = 0x03
)

// uinputSetup mirrors struct uinput_setup.
type uinputSetup struct {
	bustype, vendor, product, version uint16
	name                              [80]byte
	ffEffectsMax                      uint32
}

// uinputUserDev mirrors the legacy struct uinput_user_dev.
type uinputUserDev struct {
	name                              [80]byte
	bustype, vendor, product, version uint16
	ffEffectsMax                      uint32
	absmax, absmin, absfuzz, absflat  [64]int32
}

type inputEvent struct {
	time  unix.Timeval
	typ   uint16
	code  uint16
	value int32
}

// uinputInjector is a virtual keyboard. It also implements io.Closer.
type uinputInjector struct{ f *os.File }

// NewInjector creates a virtual keyboard through /dev/uinput.
func NewInjector() (Injector, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open /dev/uinput: %w", err)
	}
	u := &uinputInjector{f: f}
	if err := u.create(); err != nil {
		f.Close()
		return nil, err
	}
	return u, nil
}

func ioctl(fd uintptr, req, arg uintptr) error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, req, arg); errno != 0 {
		return errno
	}
	return nil
}

// ioctlPtr converts the pointer inside the Syscall argument list so that it
// stays alive and valid for the duration of the call.
func ioctlPtr(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}

func (u *uinputInjector) create() error {
	fd := u.f.Fd()
	if err := ioctl(fd, uiSetEvBit, evKey); err != nil {
		return fmt.Errorf("enable EV_KEY: %w", err)
	}
	for _, e := range keyTable {
		if err := ioctl(fd, uiSetKeyBit, uintptr(e.lin)); err != nil {
			return fmt.Errorf("enable keycode %d: %w", e.lin, err)
		}
	}
	const name = "kb7screen virtual keyboard"
	setup := uinputSetup{bustype: busUSB, vendor: 0x1209, product: 0x0001, version: 1}
	copy(setup.name[:], name)
	if err := ioctlPtr(fd, uiDevSetup, unsafe.Pointer(&setup)); err != nil {
		// Older kernels lack UI_DEV_SETUP: describe the device by writing
		// the legacy structure instead.
		dev := uinputUserDev{bustype: busUSB, vendor: setup.vendor, product: setup.product, version: 1}
		copy(dev.name[:], name)
		b := unsafe.Slice((*byte)(unsafe.Pointer(&dev)), unsafe.Sizeof(dev))
		if _, werr := u.f.Write(b); werr != nil {
			return fmt.Errorf("describe uinput device: %v; %w", err, werr)
		}
	}
	if err := ioctl(fd, uiDevCreate, 0); err != nil {
		return fmt.Errorf("create uinput device: %w", err)
	}
	return nil
}

func (u *uinputInjector) Down(k Key) error { return u.emit(k, 1) }
func (u *uinputInjector) Up(k Key) error   { return u.emit(k, 0) }

func (u *uinputInjector) emit(k Key, value int32) error {
	code, ok := LinuxCode(k)
	if !ok {
		return fmt.Errorf("key %#x has no keycode", uint16(k))
	}
	ev := [2]inputEvent{
		{typ: evKey, code: code, value: value},
		{typ: evSyn, code: 0, value: 0},
	}
	b := unsafe.Slice((*byte)(unsafe.Pointer(&ev[0])), unsafe.Sizeof(ev))
	n, err := u.f.Write(b)
	if err != nil {
		return err
	}
	if n != len(b) {
		return fmt.Errorf("short uinput write: %d of %d bytes", n, len(b))
	}
	return nil
}

// Close destroys the virtual keyboard.
func (u *uinputInjector) Close() error {
	_ = ioctl(u.f.Fd(), uiDevDestroy, 0)
	return u.f.Close()
}
