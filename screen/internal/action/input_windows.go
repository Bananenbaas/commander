package action

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	inputKeyboard       = 1
	keyeventfExtended   = 0x0001
	keyeventfKeyUp      = 0x0002
	keyeventfScanCode   = 0x0008
	inputStructPadBytes = 8 // the union is sized by MOUSEINPUT, the largest member
)

type keybdInput struct {
	vk        uint16
	scan      uint16
	flags     uint32
	time      uint32
	extraInfo uintptr
}

// winInput mirrors INPUT; the nested struct gives the union its alignment on
// both 32-bit and 64-bit Windows.
type winInput struct {
	typ uint32
	ki  keybdInput
	_   [inputStructPadBytes]byte
}

// Compile-time check that winInput has the size of INPUT (40 bytes on 64-bit,
// 28 on 32-bit Windows).
const winInputSize = 28 + 12*(unsafe.Sizeof(uintptr(0))/8)

var (
	_ [winInputSize - unsafe.Sizeof(winInput{})]byte
	_ [unsafe.Sizeof(winInput{}) - winInputSize]byte
)

var procSendInput = windows.NewLazySystemDLL("user32.dll").NewProc("SendInput")

type winInjector struct{}

// NewInjector returns an injector that sends scan-code events with SendInput.
func NewInjector() (Injector, error) { return winInjector{}, nil }

func (winInjector) Down(k Key) error { return send(k, 0) }
func (winInjector) Up(k Key) error   { return send(k, keyeventfKeyUp) }

func send(k Key, flags uint32) error {
	code, extended, ok := WindowsScan(k)
	if !ok {
		return fmt.Errorf("key %#x has no scan code", uint16(k))
	}
	flags |= keyeventfScanCode
	if extended {
		flags |= keyeventfExtended
	}
	in := winInput{typ: inputKeyboard, ki: keybdInput{scan: uint16(code), flags: flags}}
	n, _, err := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	if n != 1 {
		if e, ok := err.(syscall.Errno); !ok || e == 0 {
			err = windows.GetLastError()
		}
		return fmt.Errorf("SendInput injected %d of 1 events: %w", n, err)
	}
	return nil
}
