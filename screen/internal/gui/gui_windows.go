// SPDX-License-Identifier: GPL-2.0-or-later

package gui

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"

	"github.com/Bananenbaas/commander/screen/internal/bridge"
	"github.com/Bananenbaas/commander/screen/internal/pipe"
)

const (
	wmDestroy       = 0x0002
	wmMove          = 0x0003
	wmSize          = 0x0005
	wmSetFocus      = 0x0007
	wmClose         = 0x0010
	wmGetMinMaxInfo = 0x0024
	wmMoving        = 0x0216
	wmDPIChanged    = 0x02E0
	wmApp           = 0x8000

	wmFlush = wmApp + 1 // lines are waiting in the queue for the page
	wmShow  = wmApp + 2 // Commander asked to bring the window forward

	wsOverlappedWindow = 0x00CF0000
	wsClipChildren     = 0x02000000

	swShowNormal = 1
	swRestore    = 9
	sizeMinimize = 1

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	smCxIcon   = 11
	smCyIcon   = 12
	smCxSmIcon = 49
	smCySmIcon = 50

	wmSetIcon = 0x0080
	iconSmall = 0
	iconBig   = 1

	idcArrow       = 32512
	spiGetWorkArea = 0x0030

	dwmwaUseImmersiveDarkMode = 20

	className = "CommanderWindow"
	title     = "Commander"

	startWidth, startHeight = 1180, 760
	minWidth, minHeight     = 900, 600

	// dialTimeout is how long Run waits for Commander's pipe.
	dialTimeout = 5 * time.Second
	// maxQueue is the most lines waiting for the window thread; beyond it
	// the oldest are dropped.
	maxQueue = 1024
)

// background is the page's background, #080c18, shown before the page
// paints so there is no white flash.
var background = [3]uint8{0x08, 0x0c, 0x18}

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procRegisterClassEx          = user32.NewProc("RegisterClassExW")
	procUnregisterClass          = user32.NewProc("UnregisterClassW")
	procCreateWindowEx           = user32.NewProc("CreateWindowExW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procDefWindowProc            = user32.NewProc("DefWindowProcW")
	procGetMessage               = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessage          = user32.NewProc("DispatchMessageW")
	procPostMessage              = user32.NewProc("PostMessageW")
	procSendMessage              = user32.NewProc("SendMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procShowWindow               = user32.NewProc("ShowWindow")
	procUpdateWindow             = user32.NewProc("UpdateWindow")
	procIsIconic                 = user32.NewProc("IsIconic")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procSetWindowPos             = user32.NewProc("SetWindowPos")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procSystemParametersInfo     = user32.NewProc("SystemParametersInfoW")
	procGetDpiForSystem          = user32.NewProc("GetDpiForSystem")
	procGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	procLoadCursor               = user32.NewProc("LoadCursorW")
	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon              = user32.NewProc("DestroyIcon")
	procCreateSolidBrush         = windows.NewLazySystemDLL("gdi32.dll").NewProc("CreateSolidBrush")
	procDeleteObject             = windows.NewLazySystemDLL("gdi32.dll").NewProc("DeleteObject")
	procGetModuleHandle          = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	procDwmSetWindowAttribute    = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	procSHCreateMemStream        = windows.NewLazySystemDLL("shlwapi.dll").NewProc("SHCreateMemStream")
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
}

type point struct{ x, y int32 }

type rect struct{ left, top, right, bottom int32 }

// minMaxInfo is MINMAXINFO.
type minMaxInfo struct {
	reserved, maxSize, maxPosition, minTrackSize, maxTrackSize point
}

// window is the one window of the process. Its fields are used on the
// window thread only, except hwnd (set before any other goroutine starts)
// and the queue, which has its own lock.
type window struct {
	opts   Options
	client *bridge.Client
	ctx    context.Context

	hwnd      uintptr
	inst      uintptr
	bigIcon   uintptr
	smallIcon uintptr
	brush     uintptr

	wv         *edge.Chromium
	view       *edge.ICoreWebView2
	embedding  bool // Embed runs its own message loop; closing must wait
	closeAsked bool
	ready      bool // the page can take messages

	mu     sync.Mutex
	queue  []string
	posted bool // a wmFlush is on its way
	gone   atomic.Bool
}

var (
	running  atomic.Bool
	procOnce sync.Once
	wndProc  uintptr
	// current is the running window; set and read on the window thread.
	current *window
)

// Run shows the window, connected to the Commander listening on pipeName,
// until the window is closed or the connection ends. It returns an error
// when the connection or WebView2 cannot be set up.
//
// A WebView2 failure after setup is logged with Options.Logf and ends the
// process: the binding cannot recover from it.
func Run(pipeName string, opts Options) error {
	if !running.CompareAndSwap(false, true) {
		return errors.New("gui: a window is already running")
	}
	defer running.Store(false)

	v, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if err != nil {
		return fmt.Errorf("gui: looking for WebView2: %w", err)
	}
	if v == "" {
		return errors.New("gui: the WebView2 runtime is not installed")
	}
	if opts.DataDir == "" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return errors.New("gui: no WebView2 data folder: LOCALAPPDATA is not set")
		}
		opts.DataDir = filepath.Join(local, "Commander", "WebView2")
	}

	conn, err := pipe.Dial(pipeName, dialTimeout)
	if err != nil {
		return fmt.Errorf("gui: connecting to Commander: %w", err)
	}
	client := bridge.NewClient(conn)
	defer client.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w := &window{opts: opts, client: client, ctx: ctx}
	if err := w.create(); err != nil {
		return err
	}
	defer w.release()
	if err := w.embed(); err != nil {
		w.gone.Store(true)
		procDestroyWindow.Call(w.hwnd)
		return err
	}
	go w.pump()
	w.loop()
	w.gone.Store(true)
	w.ready = false
	return nil
}

// create registers the class and makes the window, shown at once on the
// page's background colour.
func (w *window) create() error {
	procOnce.Do(func() { wndProc = syscall.NewCallback(windowProc) })
	w.inst, _, _ = procGetModuleHandle.Call(0)
	if len(w.opts.Icon) > 0 {
		var err error
		if w.bigIcon, err = loadIcon(w.opts.Icon, smCxIcon, smCyIcon, 32); err != nil {
			w.opts.logf("window: big icon: %v", err)
		}
		if w.smallIcon, err = loadIcon(w.opts.Icon, smCxSmIcon, smCySmIcon, 16); err != nil {
			w.opts.logf("window: small icon: %v", err)
		}
	}
	w.brush, _, _ = procCreateSolidBrush.Call(uintptr(background[0]) | uintptr(background[1])<<8 | uintptr(background[2])<<16)
	cursor, _, _ := procLoadCursor.Call(0, idcArrow)

	cls := windows.StringToUTF16Ptr(className)
	wc := wndClassEx{
		wndProc:    wndProc,
		instance:   w.inst,
		icon:       w.bigIcon,
		cursor:     cursor,
		background: w.brush,
		className:  cls,
		iconSmall:  w.smallIcon,
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, e := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 && e != windows.ERROR_CLASS_ALREADY_EXISTS {
		w.releaseResources()
		return fmt.Errorf("gui: registering the window class: %w", e)
	}

	x, y, cx, cy := startBounds()
	current = w
	hwnd, _, e := procCreateWindowEx.Call(0,
		uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(title))),
		wsOverlappedWindow|wsClipChildren,
		uintptr(x), uintptr(y), uintptr(cx), uintptr(cy),
		0, 0, w.inst, 0)
	if hwnd == 0 {
		current = nil
		procUnregisterClass.Call(uintptr(unsafe.Pointer(cls)), w.inst)
		w.releaseResources()
		return fmt.Errorf("gui: creating the window: %w", e)
	}
	w.hwnd = hwnd
	if w.bigIcon != 0 {
		procSendMessage.Call(hwnd, wmSetIcon, iconBig, w.bigIcon)
	}
	if w.smallIcon != 0 {
		procSendMessage.Call(hwnd, wmSetIcon, iconSmall, w.smallIcon)
	}
	// The page is dark until it says otherwise.
	setDarkTitleBar(hwnd, true)
	procShowWindow.Call(hwnd, swShowNormal)
	procUpdateWindow.Call(hwnd)
	return nil
}

// release undoes create once the window is gone.
func (w *window) release() {
	current = nil
	procUnregisterClass.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(className))), w.inst)
	w.releaseResources()
}

func (w *window) releaseResources() {
	if w.bigIcon != 0 {
		procDestroyIcon.Call(w.bigIcon)
	}
	if w.smallIcon != 0 {
		procDestroyIcon.Call(w.smallIcon)
	}
	if w.brush != 0 {
		procDeleteObject.Call(w.brush)
	}
	w.bigIcon, w.smallIcon, w.brush = 0, 0, 0
}

// embed puts WebView2 in the window and loads the page.
func (w *window) embed() error {
	wv := edge.NewChromium()
	wv.DataPath = w.opts.DataDir
	// The binding ends the process after most errors, and after a failed
	// environment it would wait for ever; end it here in every case.
	logf := w.opts.logf
	wv.SetErrorCallback(func(err error) {
		logf("window: WebView2: %v", err)
		os.Exit(1)
	})
	wv.MessageCallback = func(msg string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		w.fromPage(msg)
	}
	wv.WebResourceRequestedCallback = func(req *edge.ICoreWebView2WebResourceRequest, args *edge.ICoreWebView2WebResourceRequestedEventArgs) {
		w.serve(req, args)
	}
	w.wv = wv

	w.embedding = true
	wv.Embed(w.hwnd)
	w.embedding = false

	view, err := wv.GetController().GetCoreWebView2()
	if err != nil {
		return fmt.Errorf("gui: WebView2 has no view: %w", err)
	}
	w.view = view
	if err := w.settings(); err != nil {
		return err
	}
	wv.SetBackgroundColour(background[0], background[1], background[2], 255)
	wv.AddWebResourceRequestedFilter("*", edge.COREWEBVIEW2_WEB_RESOURCE_CONTEXT_ALL)
	wv.Resize()
	wv.Navigate(AppOrigin + "/")
	w.ready = true
	if w.closeAsked {
		procDestroyWindow.Call(w.hwnd)
		return nil
	}
	w.drain()
	return nil
}

// settings locks the browser down to what the page needs.
func (w *window) settings() error {
	s, err := w.wv.GetSettings()
	if err != nil {
		return fmt.Errorf("gui: WebView2 settings: %w", err)
	}
	dev := w.opts.DevTools
	for _, step := range []struct {
		name string
		put  func(bool) error
		v    bool
	}{
		{"dev tools", s.PutAreDevToolsEnabled, dev},
		{"context menus", s.PutAreDefaultContextMenusEnabled, false},
		{"zoom control", s.PutIsZoomControlEnabled, false},
		{"status bar", s.PutIsStatusBarEnabled, false},
		{"browser keys", s.PutAreBrowserAcceleratorKeysEnabled, dev},
	} {
		if err := step.put(step.v); err != nil {
			return fmt.Errorf("gui: WebView2 setting %s: %w", step.name, err)
		}
	}
	return nil
}

// loop runs the message loop until the window is destroyed.
func (w *window) loop() {
	var m winMsg
	for {
		r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// pump forwards Commander's events to the page and closes the window once
// the connection ends (Commander quit).
func (w *window) pump() {
	for m := range w.client.Events() {
		if m.Event == "show" && !w.gone.Load() {
			procPostMessage.Call(w.hwnd, wmShow, 0, 0)
		}
		w.enqueue(EventLine(m))
	}
	if !w.gone.Load() {
		procPostMessage.Call(w.hwnd, wmClose, 0, 0)
	}
}

// fromPage handles a message the page posted (window thread).
func (w *window) fromPage(msg string) {
	if dark, ok := themeOf(msg); ok {
		setDarkTitleBar(w.hwnd, dark)
		return
	}
	go func() { w.enqueue(Relay(w.ctx, w.client, msg)) }()
}

// serve answers a request of the page from the embedded files (window
// thread).
func (w *window) serve(req *edge.ICoreWebView2WebResourceRequest, args *edge.ICoreWebView2WebResourceRequestedEventArgs) {
	status, ctype, body := 403, textPlain, []byte("forbidden")
	if uri, err := req.GetUri(); err == nil {
		status, ctype, body = Resource(uri)
	}
	resp, err := newResponse(w.wv.Environment(), body, status, reasonPhrase(status), responseHeaders(ctype))
	if err != nil {
		w.opts.logf("window: making a response: %v", err)
		return
	}
	defer resp.Release()
	if err := args.PutResponse(resp); err != nil {
		w.opts.logf("window: answering a request: %v", err)
	}
}

// newResponse makes a WebView2 response with body. It calls
// ICoreWebView2Environment::CreateWebResourceResponse itself because the
// binding's CreateWebResourceResponse (v1.0.23) shadows its stream variable
// and so always sends an empty body.
func newResponse(env *edge.ICoreWebView2Environment, body []byte, status int, reason, headers string) (*edge.ICoreWebView2WebResourceResponse, error) {
	if env == nil {
		return nil, errors.New("no WebView2 environment")
	}
	reason16, err := windows.UTF16PtrFromString(reason)
	if err != nil {
		return nil, err
	}
	headers16, err := windows.UTF16PtrFromString(headers)
	if err != nil {
		return nil, err
	}
	var stream unsafe.Pointer // IStream, or nil for no body
	if len(body) > 0 {
		s, _, e := procSHCreateMemStream.Call(uintptr(unsafe.Pointer(&body[0])), uintptr(len(body)))
		if s == 0 {
			return nil, fmt.Errorf("making the body stream: %w", e)
		}
		stream = *(*unsafe.Pointer)(unsafe.Pointer(&s))
		// The response holds its own reference to the stream.
		defer comRelease(stream)
	}
	// The vtable of ICoreWebView2Environment: QueryInterface, AddRef,
	// Release, CreateCoreWebView2Controller, CreateWebResourceResponse.
	vtbl := *(**[5]uintptr)(unsafe.Pointer(env))
	var resp *edge.ICoreWebView2WebResourceResponse
	hr, _, _ := syscall.SyscallN(vtbl[4],
		uintptr(unsafe.Pointer(env)),
		uintptr(stream),
		uintptr(status),
		uintptr(unsafe.Pointer(reason16)),
		uintptr(unsafe.Pointer(headers16)),
		uintptr(unsafe.Pointer(&resp)))
	if int32(hr) < 0 {
		return nil, fmt.Errorf("CreateWebResourceResponse: %w", syscall.Errno(hr))
	}
	if resp == nil {
		return nil, errors.New("CreateWebResourceResponse gave no response")
	}
	return resp, nil
}

// comRelease calls IUnknown::Release on a COM object.
func comRelease(obj unsafe.Pointer) {
	vtbl := *(**[3]uintptr)(obj)
	syscall.SyscallN(vtbl[2], uintptr(obj))
}

// enqueue queues a line for the page and wakes the window thread (any
// goroutine).
func (w *window) enqueue(line string) {
	w.mu.Lock()
	if len(w.queue) >= maxQueue {
		w.queue = w.queue[1:]
	}
	w.queue = append(w.queue, line)
	wake := !w.posted
	w.posted = true
	w.mu.Unlock()
	if !wake || w.gone.Load() {
		return
	}
	if r, _, _ := procPostMessage.Call(w.hwnd, wmFlush, 0, 0); r == 0 {
		// The thread's queue is full; let the next line try again.
		w.mu.Lock()
		w.posted = false
		w.mu.Unlock()
	}
}

// drain posts the queued lines to the page (window thread).
func (w *window) drain() {
	if !w.ready {
		return
	}
	w.mu.Lock()
	lines := w.queue
	w.queue = nil
	w.posted = false
	w.mu.Unlock()
	for _, line := range lines {
		if err := w.view.PostWebMessageAsString(line); err != nil {
			w.opts.logf("window: posting to the page: %v", err)
		}
	}
}

// bringForward restores a minimised window and puts it in front.
func (w *window) bringForward() {
	if r, _, _ := procIsIconic.Call(w.hwnd); r != 0 {
		procShowWindow.Call(w.hwnd, swRestore)
	}
	procSetForegroundWindow.Call(w.hwnd)
}

func windowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	w := current
	if w == nil {
		r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
		return r
	}
	switch msg {
	case wmGetMinMaxInfo:
		mmi := *(**minMaxInfo)(unsafe.Pointer(&lParam)) // the system owns the struct
		dpi := dpiOf(hwnd)
		mmi.minTrackSize = point{scale(minWidth, dpi), scale(minHeight, dpi)}
		return 0
	case wmDPIChanged:
		r := *(**rect)(unsafe.Pointer(&lParam)) // the suggested bounds
		procSetWindowPos.Call(hwnd, 0, uintptr(r.left), uintptr(r.top),
			uintptr(r.right-r.left), uintptr(r.bottom-r.top), swpNoZOrder|swpNoActivate)
		return 0
	case wmSize:
		if w.wv != nil && wParam != sizeMinimize {
			w.wv.Resize()
		}
	case wmMove, wmMoving:
		if w.wv != nil {
			w.wv.NotifyParentWindowPositionChanged()
		}
	case wmSetFocus:
		if w.ready {
			w.wv.Focus()
		}
	case wmFlush:
		w.drain()
		return 0
	case wmShow:
		w.bringForward()
		return 0
	case wmClose:
		if w.embedding {
			w.closeAsked = true
			return 0
		}
		w.ready = false
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		w.gone.Store(true)
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProc.Call(hwnd, msg, wParam, lParam)
	return r
}

// setDarkTitleBar sets the title bar to the dark or the light theme.
func setDarkTitleBar(hwnd uintptr, dark bool) {
	if procDwmSetWindowAttribute.Find() != nil {
		return
	}
	var v int32
	if dark {
		v = 1
	}
	procDwmSetWindowAttribute.Call(hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
}

// dpiOf is the DPI of the window, or of the system without one.
func dpiOf(hwnd uintptr) int32 {
	if hwnd != 0 && procGetDpiForWindow.Find() == nil {
		if d, _, _ := procGetDpiForWindow.Call(hwnd); d != 0 {
			return int32(d)
		}
	}
	if procGetDpiForSystem.Find() == nil {
		if d, _, _ := procGetDpiForSystem.Call(); d != 0 {
			return int32(d)
		}
	}
	return 96
}

func scale(v, dpi int32) int32 { return v * dpi / 96 }

// startBounds centres the start size in the work area of the main screen.
func startBounds() (x, y, cx, cy int32) {
	dpi := dpiOf(0)
	cx, cy = scale(startWidth, dpi), scale(startHeight, dpi)
	var work rect
	if r, _, _ := procSystemParametersInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&work)), 0); r == 0 {
		work = rect{0, 0, 1920, 1080}
	}
	ww, wh := work.right-work.left, work.bottom-work.top
	cx, cy = min(cx, ww), min(cy, wh)
	return work.left + (ww-cx)/2, work.top + (wh-cy)/2, cx, cy
}

// loadIcon makes an icon of the given system metric size from an ICO file.
func loadIcon(ico []byte, mx, my uintptr, fallback int) (uintptr, error) {
	cx, _, _ := procGetSystemMetrics.Call(mx)
	cy, _, _ := procGetSystemMetrics.Call(my)
	if cx == 0 || cy == 0 {
		cx, cy = uintptr(fallback), uintptr(fallback)
	}
	data, err := pickICO(ico, int(cx))
	if err != nil {
		return 0, err
	}
	h, _, e := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)),
		1, 0x00030000, cx, cy, 0)
	if h == 0 {
		return 0, fmt.Errorf("making the icon: %w", e)
	}
	return h, nil
}

// pickICO returns the image of an ICO file closest to want pixels: the
// smallest at least that big, else the biggest.
func pickICO(ico []byte, want int) ([]byte, error) {
	le := binary.LittleEndian
	if len(ico) < 6 || le.Uint16(ico[0:]) != 0 || le.Uint16(ico[2:]) != 1 {
		return nil, errors.New("not an ICO file")
	}
	n := int(le.Uint16(ico[4:]))
	if n == 0 || len(ico) < 6+16*n {
		return nil, errors.New("ICO directory empty or cut short")
	}
	best, bestSize := -1, 0
	for i := 0; i < n; i++ {
		s := int(ico[6+16*i])
		if s == 0 {
			s = 256
		}
		switch {
		case best < 0,
			s >= want && (bestSize < want || s < bestSize),
			s < want && bestSize < want && s > bestSize:
			best, bestSize = i, s
		}
	}
	e := ico[6+16*best:]
	length, off := uint64(le.Uint32(e[8:])), uint64(le.Uint32(e[12:]))
	if length == 0 || off+length > uint64(len(ico)) {
		return nil, fmt.Errorf("ICO entry of %d pixels lies outside the file", bestSize)
	}
	return ico[off : off+length], nil
}
