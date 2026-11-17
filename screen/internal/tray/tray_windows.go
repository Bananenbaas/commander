// SPDX-License-Identifier: GPL-2.0-or-later

package tray

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	wmNull            = 0x0000
	wmDestroy         = 0x0002
	wmClose           = 0x0010
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016
	wmContextMenu     = 0x007B
	wmTimer           = 0x0113
	wmLButtonUp       = 0x0202
	wmRButtonUp       = 0x0205
	wmUser            = 0x0400
	wmApp             = 0x8000

	wmTray   = wmApp + 1 // notification-area callback
	wmUpdate = wmApp + 2 // SetTip, SetFailed or Balloon left something to apply

	ninSelect    = wmUser + 0
	ninKeySelect = wmUser + 1

	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	nifMessage  = 0x01
	nifIcon     = 0x02
	nifTip      = 0x04
	nifInfo     = 0x10
	nifShowTip  = 0x80
	niifInfo    = 0x01
	nidVersion4 = 4

	mfString    = 0x0000
	mfChecked   = 0x0008
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	smCxSmIcon = 49
	smCySmIcon = 50

	msgfltAllow = 1

	iconID     = 1
	retryTimer = 1
	retryEvery = 1000 // ms between attempts to add the icon while the shell is not up
	// addWait is how long Run waits for the icon to appear; at log on the
	// notification area may not be there yet, and the icon is added later.
	addWait = 10 * time.Second
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procRegisterClassEx          = user32.NewProc("RegisterClassExW")
	procUnregisterClass          = user32.NewProc("UnregisterClassW")
	procCreateWindowEx           = user32.NewProc("CreateWindowExW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procDefWindowProc            = user32.NewProc("DefWindowProcW")
	procGetMessage               = user32.NewProc("GetMessageW")
	procDispatchMessage          = user32.NewProc("DispatchMessageW")
	procPostMessage              = user32.NewProc("PostMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessage    = user32.NewProc("RegisterWindowMessageW")
	procChangeWindowMessageFilEx = user32.NewProc("ChangeWindowMessageFilterEx")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenu               = user32.NewProc("AppendMenuW")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon              = user32.NewProc("DestroyIcon")
	procSetTimer                 = user32.NewProc("SetTimer")
	procKillTimer                = user32.NewProc("KillTimer")
	procShellNotifyIcon          = windows.NewLazySystemDLL("shell32.dll").NewProc("Shell_NotifyIconW")
	procGetModuleHandle          = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
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

type point struct{ x, y int32 }

// notifyIconData is NOTIFYICONDATAW (Vista and later).
type notifyIconData struct {
	size            uint32
	wnd             uintptr
	id              uint32
	flags           uint32
	callbackMessage uint32
	icon            uintptr
	tip             [128]uint16
	state           uint32
	stateMask       uint32
	info            [256]uint16
	version         uint32 // also uTimeout
	infoTitle       [64]uint16
	infoFlags       uint32
	guidItem        windows.GUID
	balloonIcon     uintptr
}

type balloon struct{ title, text string }

// Tray is a running tray icon.
type Tray struct {
	hwnd uintptr // set before Run returns, then read-only
	done chan struct{}

	// Owned by the window thread.
	items          []Item
	onEnd          func()
	icon, failIcon uintptr
	class          *uint16
	inst           uintptr
	taskbarCreated uintptr
	added          bool // the icon is in the notification area
	v4             bool // NOTIFYICON_VERSION_4 accepted
	menuOpen       bool
	endCalled      bool
	addedOnce      sync.Once
	shown          chan struct{} // closed when the icon is first added

	// Set from any goroutine, applied by the window thread.
	mu       sync.Mutex
	tip      string
	failed   bool
	balloons []balloon
}

var (
	trays     sync.Map // hwnd -> *Tray
	procOnce  sync.Once
	wndProc   uintptr
	classSeq  atomic.Int64
	errNoIcon = errors.New("tray: no icon given")
)

// Run shows the tray icon and returns when it is shown (or, when the
// notification area is not there yet, after a few seconds; the icon then
// appears as soon as it is). The icon goes away when ctx ends.
func Run(ctx context.Context, o Options) (*Tray, error) {
	if len(o.Icon) == 0 {
		return nil, errNoIcon
	}
	failed := o.FailedIcon
	if len(failed) == 0 {
		failed = o.Icon
	}
	procOnce.Do(func() { wndProc = syscall.NewCallback(windowProc) })
	t := &Tray{
		done:  make(chan struct{}),
		shown: make(chan struct{}),
		items: append([]Item(nil), o.Items...),
		onEnd: o.OnEndSession,
		tip:   o.Tip,
	}
	setup := make(chan error, 1)
	go t.thread(o.Icon, failed, setup)
	if err := <-setup; err != nil {
		return nil, err
	}
	timer := time.NewTimer(addWait)
	defer timer.Stop()
	select {
	case <-t.shown:
	case <-t.done:
	case <-timer.C:
	}
	go func() {
		select {
		case <-ctx.Done():
			procPostMessage.Call(t.hwnd, wmClose, 0, 0)
		case <-t.done:
		}
	}()
	return t, nil
}

// SetTip changes the tooltip. It may be called from any goroutine.
func (t *Tray) SetTip(s string) {
	t.mu.Lock()
	t.tip = s
	t.mu.Unlock()
	t.post()
}

// SetFailed switches between the normal and the error icon. It may be called
// from any goroutine.
func (t *Tray) SetFailed(failed bool) {
	t.mu.Lock()
	t.failed = failed
	t.mu.Unlock()
	t.post()
}

// Balloon shows a notification next to the icon. It may be called from any
// goroutine.
func (t *Tray) Balloon(title, text string) {
	t.mu.Lock()
	t.balloons = append(t.balloons, balloon{title, text})
	t.mu.Unlock()
	t.post()
}

// Done is closed when the tray's window thread has ended: after ctx ended,
// or at the end of the Windows session.
func (t *Tray) Done() <-chan struct{} { return t.done }

func (t *Tray) post() {
	select {
	case <-t.done:
	default:
		procPostMessage.Call(t.hwnd, wmUpdate, 0, 0)
	}
}

// thread owns the window and runs its message loop.
func (t *Tray) thread(icon, failed []byte, setup chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(t.done)
	if err := t.create(icon, failed); err != nil {
		setup <- err
		return
	}
	setup <- nil
	defer t.release()
	if !t.add() {
		procSetTimer.Call(t.hwnd, retryTimer, retryEvery, 0)
	}
	var m winMsg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// create loads the icons and makes the hidden window. It must be a normal
// top-level window, not a message-only one: only those receive the
// TaskbarCreated broadcast and the end-of-session messages.
func (t *Tray) create(icon, failed []byte) (err error) {
	if t.icon, err = loadIcon(icon); err != nil {
		return err
	}
	if t.failIcon, err = loadIcon(failed); err != nil {
		procDestroyIcon.Call(t.icon)
		return err
	}
	t.inst, _, _ = procGetModuleHandle.Call(0)
	t.class, _ = windows.UTF16PtrFromString(fmt.Sprintf("Commander-tray-%d", classSeq.Add(1)))
	wc := wndClassEx{size: uint32(unsafe.Sizeof(wndClassEx{})), wndProc: wndProc, instance: t.inst, className: t.class}
	if r, _, e := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		t.destroyIcons()
		return fmt.Errorf("tray: registering the window class: %w", e)
	}
	title, _ := windows.UTF16PtrFromString("Commander")
	hwnd, _, e := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(t.class)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, t.inst, 0)
	if hwnd == 0 {
		procUnregisterClass.Call(uintptr(unsafe.Pointer(t.class)), t.inst)
		t.destroyIcons()
		return fmt.Errorf("tray: creating the window: %w", e)
	}
	t.hwnd = hwnd
	trays.Store(hwnd, t)
	name, _ := windows.UTF16PtrFromString("TaskbarCreated")
	t.taskbarCreated, _, _ = procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(name)))
	if t.taskbarCreated != 0 {
		// Let the broadcast through when running elevated.
		procChangeWindowMessageFilEx.Call(hwnd, t.taskbarCreated, msgfltAllow, 0)
	}
	return nil
}

// release undoes create once the message loop has ended.
func (t *Tray) release() {
	t.remove()
	trays.Delete(t.hwnd)
	procDestroyWindow.Call(t.hwnd) // fails harmlessly when already destroyed
	procUnregisterClass.Call(uintptr(unsafe.Pointer(t.class)), t.inst)
	t.destroyIcons()
}

func (t *Tray) destroyIcons() {
	procDestroyIcon.Call(t.icon)
	if t.failIcon != t.icon {
		procDestroyIcon.Call(t.failIcon)
	}
}

// loadIcon makes an icon of the small system icon size from an ICO file.
func loadIcon(ico []byte) (uintptr, error) {
	cx, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	cy, _, _ := procGetSystemMetrics.Call(smCySmIcon)
	if cx == 0 || cy == 0 {
		cx, cy = 16, 16
	}
	data, _, err := pickICO(ico, int(cx))
	if err != nil {
		return 0, err
	}
	h, _, e := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)),
		1, 0x00030000, cx, cy, 0)
	if h == 0 {
		return 0, fmt.Errorf("tray: making the icon: %w", e)
	}
	return h, nil
}

// putUTF16 copies s into a zero-terminated fixed field.
func putUTF16(dst []uint16, s string) {
	u := utf16.Encode([]rune(s))
	n := copy(dst[:len(dst)-1], u)
	dst[n] = 0
}

// data is the full current state of the icon: callback, icon and tooltip.
func (t *Tray) data() notifyIconData {
	t.mu.Lock()
	tip, failed := t.tip, t.failed
	t.mu.Unlock()
	n := notifyIconData{
		size:            uint32(unsafe.Sizeof(notifyIconData{})),
		wnd:             t.hwnd,
		id:              iconID,
		flags:           nifMessage | nifIcon | nifTip | nifShowTip,
		callbackMessage: wmTray,
		icon:            t.icon,
	}
	if failed {
		n.icon = t.failIcon
	}
	putUTF16(n.tip[:], cutTip(tip))
	return n
}

func shellNotify(msg uintptr, n *notifyIconData) bool {
	r, _, _ := procShellNotifyIcon.Call(msg, uintptr(unsafe.Pointer(n)))
	return r != 0
}

// add puts the icon in the notification area. It fails while the shell is
// not up; the retry timer and TaskbarCreated try again.
func (t *Tray) add() bool {
	n := t.data()
	if !shellNotify(nimAdd, &n) {
		return false
	}
	n.version = nidVersion4
	t.v4 = shellNotify(nimSetVersion, &n)
	t.added = true
	procKillTimer.Call(t.hwnd, retryTimer)
	t.addedOnce.Do(func() { close(t.shown) })
	t.update()
	return true
}

// remove takes the icon out of the notification area.
func (t *Tray) remove() {
	procKillTimer.Call(t.hwnd, retryTimer)
	if t.added {
		n := notifyIconData{size: uint32(unsafe.Sizeof(notifyIconData{})), wnd: t.hwnd, id: iconID}
		shellNotify(nimDelete, &n)
		t.added = false
	}
}

// update applies what SetTip, SetFailed and Balloon left. Balloons wait
// until the icon is there.
func (t *Tray) update() {
	if !t.added {
		return
	}
	t.mu.Lock()
	bs := t.balloons
	t.balloons = nil
	t.mu.Unlock()
	n := t.data()
	shellNotify(nimModify, &n)
	for _, b := range bs {
		n := t.data()
		n.flags |= nifInfo
		n.infoFlags = niifInfo
		putUTF16(n.infoTitle[:], cutUnits(b.title, maxTitle))
		putUTF16(n.info[:], cutUnits(b.text, maxInfo))
		shellNotify(nimModify, &n)
	}
}

// showMenu shows the menu at (x, y) and runs the chosen item's OnClick on a
// goroutine of its own, so the window thread never waits for it.
func (t *Tray) showMenu(x, y int32) {
	if t.menuOpen {
		return
	}
	t.menuOpen = true
	defer func() { t.menuOpen = false }()
	m, _, _ := procCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer procDestroyMenu.Call(m)
	for i, it := range t.items {
		if it.Separator {
			procAppendMenu.Call(m, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if it.Checked != nil && it.Checked() {
			flags |= mfChecked
		}
		label := append(utf16.Encode([]rune(it.Label)), 0)
		procAppendMenu.Call(m, flags, menuID(i), uintptr(unsafe.Pointer(&label[0])))
	}
	// Without the foreground the menu does not close when clicking elsewhere.
	procSetForegroundWindow.Call(t.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(m, tpmReturnCmd|tpmRightButton|tpmNoNotify,
		uintptr(x), uintptr(y), 0, t.hwnd, 0)
	procPostMessage.Call(t.hwnd, wmNull, 0, 0)
	if i, ok := itemIndex(t.items, cmd); ok && t.items[i].OnClick != nil {
		go t.items[i].OnClick()
	}
}

func windowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	v, ok := trays.Load(hwnd)
	if !ok {
		r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
		return r
	}
	t := v.(*Tray)
	switch msg {
	case wmTray:
		event := lParam & 0xFFFF
		if t.v4 {
			if event == wmContextMenu || event == ninSelect || event == ninKeySelect {
				// Version 4 passes the anchor point in wParam.
				t.showMenu(int32(int16(wParam)), int32(int16(wParam>>16)))
			}
		} else if event == wmRButtonUp || event == wmLButtonUp {
			var p point
			procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
			t.showMenu(p.x, p.y)
		}
		return 0
	case wmUpdate:
		t.update()
		return 0
	case wmTimer:
		if wParam == retryTimer {
			t.add()
		}
		return 0
	case wmClose:
		t.remove()
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	case wmQueryEndSession:
		// The window thread waits here for the clean stop (OnEndSession
		// returns within 5 s); Windows waits for the answer.
		if t.onEnd != nil && !t.endCalled {
			t.endCalled = true
			t.onEnd()
		}
		return 1
	case wmEndSession:
		t.remove()
		procDestroyWindow.Call(hwnd)
		return 0
	}
	if t.taskbarCreated != 0 && msg == t.taskbarCreated {
		// Explorer (re)started: its notification area is new and empty.
		t.added = false
		if !t.add() {
			procSetTimer.Call(t.hwnd, retryTimer, retryEvery, 0)
		}
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return r
}
