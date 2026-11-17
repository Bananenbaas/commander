// SPDX-License-Identifier: GPL-2.0-or-later

package keys

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmInput = 0x00FF
	wmQuit  = 0x0012

	ridInput        = 0x10000003
	ridiDeviceName  = 0x20000007
	rimTypeKeyboard = 1

	ridevRemove    = 0x00000001
	ridevInputSink = 0x00000100

	// hwndMessage is HWND_MESSAGE (-3): the window only receives messages.
	hwndMessage = ^uintptr(2)

	className = "kb7screen-keys"
	chanSize  = 64
)

var (
	user32                      = windows.NewLazySystemDLL("user32.dll")
	procRegisterClassEx         = user32.NewProc("RegisterClassExW")
	procUnregisterClass         = user32.NewProc("UnregisterClassW")
	procCreateWindowEx          = user32.NewProc("CreateWindowExW")
	procDestroyWindow           = user32.NewProc("DestroyWindow")
	procDefWindowProc           = user32.NewProc("DefWindowProcW")
	procGetMessage              = user32.NewProc("GetMessageW")
	procDispatchMessage         = user32.NewProc("DispatchMessageW")
	procPostThreadMessage       = user32.NewProc("PostThreadMessageW")
	procRegisterRawInputDevices = user32.NewProc("RegisterRawInputDevices")
	procGetRawInputData         = user32.NewProc("GetRawInputData")
	procGetRawInputDeviceInfo   = user32.NewProc("GetRawInputDeviceInfoW")
	procGetModuleHandle         = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
)

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSmall  uintptr
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	x, y    int32
	private uint32
}

type rawInputDevice struct {
	usagePage uint16
	usage     uint16
	flags     uint32
	target    uintptr
}

// rawInputHeader is RAWINPUTHEADER; the RAWKEYBOARD follows it directly.
type rawInputHeader struct {
	typ    uint32
	size   uint32
	device uintptr
	wParam uintptr
}

type rawKeyboard struct {
	makeCode uint16
	flags    uint16
	reserved uint16
	vKey     uint16
	message  uint32
	extra    uint32
}

// listener is the state the window procedure works on. Only one Listen runs at
// a time; a second call is refused.
type listener struct {
	out   chan Event
	names map[uintptr]bool // device handle -> is the KB7
}

var (
	active   atomic.Pointer[listener]
	procOnce sync.Once
	wndProc  uintptr
)

// Listen reports the key presses of the KB7. Presses of other keyboards and
// the keys this program sends itself are not reported. The channel is closed
// when ctx ends; events are dropped when the reader falls behind.
func Listen(ctx context.Context) (<-chan Event, error) {
	l := &listener{out: make(chan Event, chanSize), names: map[uintptr]bool{}}
	if !active.CompareAndSwap(nil, l) {
		return nil, errors.New("the key reader is already running")
	}
	procOnce.Do(func() { wndProc = syscall.NewCallback(windowProc) })

	type started struct {
		thread uint32
		err    error
	}
	ready := make(chan started, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer active.Store(nil)
		defer close(l.out)
		thread := windows.GetCurrentThreadId()
		cleanup, err := l.setup()
		ready <- started{thread, err}
		if err != nil {
			return
		}
		defer cleanup()
		var m winMsg
		for {
			r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()

	s := <-ready
	if s.err != nil {
		return nil, s.err
	}
	go func() {
		<-ctx.Done()
		procPostThreadMessage.Call(uintptr(s.thread), wmQuit, 0, 0)
	}()
	return l.out, nil
}

// setup creates the message window and registers for keyboard input. The
// returned function undoes both.
func (l *listener) setup() (func(), error) {
	inst, _, _ := procGetModuleHandle.Call(0)
	name, _ := windows.UTF16PtrFromString(className)
	wc := wndClassEx{size: uint32(unsafe.Sizeof(wndClassEx{})), wndProc: wndProc, instance: inst, className: name}
	if r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return nil, fmt.Errorf("registering the key reader window class: %w", err)
	}
	unregister := func() { procUnregisterClass.Call(uintptr(unsafe.Pointer(name)), inst) }
	hwnd, _, err := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(name)), 0, 0, 0, 0, 0, 0, hwndMessage, 0, inst, 0)
	if hwnd == 0 {
		unregister()
		return nil, fmt.Errorf("creating the key reader window: %w", err)
	}
	dev := rawInputDevice{usagePage: 1, usage: 6, flags: ridevInputSink, target: hwnd}
	r, _, err := procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&dev)), 1, unsafe.Sizeof(dev))
	if r == 0 {
		procDestroyWindow.Call(hwnd)
		unregister()
		return nil, fmt.Errorf("registering for keyboard input: %w", err)
	}
	return func() {
		rm := rawInputDevice{usagePage: 1, usage: 6, flags: ridevRemove}
		procRegisterRawInputDevices.Call(uintptr(unsafe.Pointer(&rm)), 1, unsafe.Sizeof(rm))
		procDestroyWindow.Call(hwnd)
		unregister()
	}, nil
}

func windowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if msg == wmInput {
		if l := active.Load(); l != nil {
			l.handle(lParam)
		}
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return r
}

// handle reads one WM_INPUT message and reports it when it is a key of the KB7.
func (l *listener) handle(hRawInput uintptr) {
	var size uint32
	hdrSize := unsafe.Sizeof(rawInputHeader{})
	procGetRawInputData.Call(hRawInput, ridInput, 0, uintptr(unsafe.Pointer(&size)), hdrSize)
	if uintptr(size) < hdrSize+unsafe.Sizeof(rawKeyboard{}) {
		return
	}
	buf := make([]uint64, (size+7)/8) // 8-byte aligned for the header
	r, _, _ := procGetRawInputData.Call(hRawInput, ridInput, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), hdrSize)
	if int32(r) <= 0 {
		return
	}
	hdr := (*rawInputHeader)(unsafe.Pointer(&buf[0]))
	// hDevice 0 is input this program sent itself.
	if hdr.typ != rimTypeKeyboard || hdr.device == 0 || !l.isKB7Handle(hdr.device) {
		return
	}
	kb := (*rawKeyboard)(unsafe.Add(unsafe.Pointer(&buf[0]), hdrSize))
	key, down, ok := decodeWindows(kb.makeCode, kb.flags)
	if !ok {
		return
	}
	select {
	case l.out <- Event{Key: key, Down: down, At: time.Now()}:
	default:
	}
}

// isKB7Handle looks up, once per device handle, whether it is the KB7.
func (l *listener) isKB7Handle(h uintptr) bool {
	if v, ok := l.names[h]; ok {
		return v
	}
	var n uint32
	procGetRawInputDeviceInfo.Call(h, ridiDeviceName, 0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return false
	}
	buf := make([]uint16, n)
	r, _, _ := procGetRawInputDeviceInfo.Call(h, ridiDeviceName, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if int32(r) <= 0 {
		return false
	}
	v := isKB7(windows.UTF16ToString(buf))
	l.names[h] = v
	return v
}
