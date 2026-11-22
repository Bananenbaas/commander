// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"

	"github.com/Bananenbaas/commander/screen/internal/appdata"
	"github.com/Bananenbaas/commander/screen/internal/autostart"
	"github.com/Bananenbaas/commander/screen/internal/bridge"
	"github.com/Bananenbaas/commander/screen/internal/control"
	"github.com/Bananenbaas/commander/screen/internal/gui"
	"github.com/Bananenbaas/commander/screen/internal/icon"
	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/pipe"
	"github.com/Bananenbaas/commander/screen/internal/supervisor"
)

// acceptRetry is the wait after a failed Accept before the next one.
const acceptRetry = time.Second

// asfwAny lets any process take the foreground (ASFW_ANY).
const asfwAny = 0xFFFFFFFF

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
)

// allowForeground lets the window process pid bring its window to the
// front. Windows refuses SetForegroundWindow to a background process unless
// the process the user is working with (Commander, after a click on its
// icon or menu) allows it first. pid 0 (unknown) allows any process.
func allowForeground(pid uint32, logf func(string, ...any)) {
	if pid == 0 {
		pid = asfwAny
	}
	if r, _, err := procAllowSetForegroundWindow.Call(uintptr(pid)); r == 0 {
		logf("the window may not come to the front: %v", err)
	}
}

// clientPID is the process ID at the other end of the pipe connection c, 0
// when it cannot be read.
func clientPID(c io.ReadWriteCloser) uint32 {
	sc, ok := c.(syscall.Conn)
	if !ok {
		return 0
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return 0
	}
	var pid uint32
	if err := raw.Control(func(fd uintptr) {
		if windows.GetNamedPipeClientProcessId(windows.Handle(fd), &pid) != nil {
			pid = 0
		}
	}); err != nil {
		return 0
	}
	return pid
}

// startWindow starts commander.exe --window in the program's folder and
// lets it come to the front.
func startWindow(exe string, logf func(string, ...any)) error {
	cmd := exec.Command(exe, "--window")
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return err
	}
	allowForeground(uint32(cmd.Process.Pid), logf)
	return cmd.Process.Release()
}

// webviewRuntime is the version of the installed WebView2 runtime; "" or an
// error means there is none. It never ends the process.
func webviewRuntime() (string, error) {
	return webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
}

// windowMain is commander.exe --window: it shows the window, connected to the
// Commander already running, until the window is closed or Commander ends,
// and returns the exit code. It takes no lock and never opens the keyboard.
func windowMain() int {
	logw, logErr := appdata.OpenWindowLog()
	out := io.Discard
	if logErr == nil {
		defer logw.Close()
		out = logw
	}
	logf := log.New(out, "", 0).Printf
	logf("Commander %s window starts", version)

	if v, err := webviewRuntime(); err != nil || v == "" {
		logf("no WebView2 runtime: %v", err)
		messageBox(webviewMissingText, windows.MB_ICONINFORMATION)
		return 1
	}
	name, err := pipe.Name()
	if err != nil {
		logf("no pipe name: %v", err)
		messageBox("Commander's window cannot find its connection to Commander:\n\n"+err.Error(), windows.MB_ICONWARNING)
		return 1
	}
	ico, err := icon.ICO(icon.Sizes, false)
	if err != nil {
		logf("cannot draw the icon: %v", err)
		ico = nil
	}
	err = gui.Run(name, gui.Options{
		DataDir:  appdata.WebView2Dir(),
		DevTools: version == "dev",
		Icon:     ico,
		Logf:     logf,
	})
	switch {
	case err == nil:
		logf("window closed")
		return 0
	case errors.Is(err, windows.ERROR_FILE_NOT_FOUND):
		// pipe.Dial fails at once when no Commander listens.
		logf("Commander is not running: %v", err)
		messageBox(notRunningText, windows.MB_ICONINFORMATION)
	default:
		logf("window failed: %v", err)
		messageBox(openFailedMessage(err), windows.MB_ICONWARNING)
	}
	return 1
}

// windowServer is the tray process's side of the window: the pipe it
// listens on, the connection it serves and the control service behind it.
type windowServer struct {
	svc  *control.Service
	conn windowConn
	ctl  *windowCtl
	logf func(string, ...any)

	mu      sync.Mutex
	ln      *pipe.Listener // nil until listening works
	stopped bool           // stop was called: no new listener
	loops   sync.WaitGroup // the running accept loop
}

// newWindowServer listens on the user's pipe. A failure is logged and the
// tray goes on; Open Commander tries to listen again before it says why
// there is no window (after an update's restart the old Commander may still
// have held the pipe).
func newWindowServer(svc *control.Service, exe string, logf func(string, ...any)) *windowServer {
	ws := &windowServer{svc: svc, logf: logf}
	ws.ctl = &windowCtl{
		connected: ws.conn.connected,
		show: func() {
			allowForeground(ws.conn.livePID(), logf)
			svc.Show()
		},
		runtime: webviewRuntime,
		start: func() error {
			if err := ws.listen(); err != nil {
				logf("the window cannot be offered: %v", err)
				return fmt.Errorf("Commander cannot listen for its window: %w", err)
			}
			if exe == "" {
				return errNoExe
			}
			logf("starting the window")
			return startWindow(exe, logf)
		},
		tell: func(text string) {
			messageBox(text, windows.MB_ICONINFORMATION|windows.MB_SETFOREGROUND|windows.MB_TOPMOST)
		},
	}
	// The accept loop uses ws.ctl, so it starts only now.
	if err := ws.listen(); err != nil {
		logf("the window cannot be offered yet: %v", err)
	}
	return ws
}

// listen makes sure the pipe is listened on and its accept loop runs; it
// does nothing when that is already so.
func (ws *windowServer) listen() error {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	if ws.ln != nil {
		return nil
	}
	if ws.stopped {
		return errors.New("Commander is stopping")
	}
	name, err := pipe.Name()
	if err != nil {
		return err
	}
	ln, err := pipe.Listen(name)
	if err != nil {
		return err
	}
	ws.ln = ln
	ws.loops.Add(1)
	go ws.serve(ln)
	return nil
}

// serve accepts window connections on ln until it is closed, serving one at
// a time; a second one while a window is connected is closed at once.
func (ws *windowServer) serve(ln *pipe.Listener) {
	defer ws.loops.Done()
	for ws.acceptOne(ln) {
	}
}

// acceptOne accepts and starts serving one connection; false means ln is
// closed. A panic is logged and the loop goes on accepting.
func (ws *windowServer) acceptOne(ln *pipe.Listener) (more bool) {
	defer func() {
		if v := recover(); v != nil {
			ws.logf("window accept loop: panic: %v\n%s", v, debug.Stack())
			more = true
		}
	}()
	c, err := ln.Accept()
	if errors.Is(err, pipe.ErrClosed) {
		return false
	}
	if err != nil {
		ws.logf("window connection not accepted: %v", err)
		time.Sleep(acceptRetry)
		return true
	}
	sc := &servedConn{ReadWriteCloser: c, wc: &ws.conn, pid: clientPID(c)}
	if !ws.conn.take(sc) {
		ws.logf("a second window connection was closed: a window is already connected")
		c.Close()
		return true
	}
	// Events queued while no window was connected are stale.
	ws.svc.DropEvents()
	ws.ctl.attached()
	ws.logf("window connected (process %d)", sc.pid)
	go ws.serveConn(sc)
	return true
}

// serveConn serves one window until its connection ends. bridge.Serve
// guards the handlers it runs on its own goroutines; this guards Serve
// itself, so a panic there closes the connection instead of Commander.
func (ws *windowServer) serveConn(sc *servedConn) {
	defer sc.Close()
	defer func() {
		if v := recover(); v != nil {
			ws.logf("window connection: panic: %v\n%s", v, debug.Stack())
		}
	}()
	if err := bridge.Serve(context.Background(), sc, ws.svc.Handle, ws.svc.Events()); err != nil {
		ws.logf("window connection ended: %v", err)
		return
	}
	ws.logf("window disconnected")
}

// stop closes the listener and the live connection, so the window process
// sees its connection end and closes, and stops the control service. It does
// not wait for calls still running (a question box may be open).
func (ws *windowServer) stop() {
	ws.mu.Lock()
	ws.stopped = true
	ln := ws.ln
	ws.mu.Unlock()
	if ln != nil {
		if err := ln.Close(); err != nil {
			ws.logf("closing the window pipe: %v", err)
		}
	}
	ws.loops.Wait()
	ws.conn.closeAll()
	ws.svc.Close()
}

// controlDeps is what the window's calls use; the settings stay in step with
// the tray menu, which reads them anew every time it opens.
func controlDeps(ctx context.Context, exe, dir string, u *updater, sup *supervisor.Supervisor, logf func(string, ...any)) control.Deps {
	return control.Deps{
		Version: version,
		Dir:     dir,
		Load:    layout.Load,
		Autostart: func() (bool, error) {
			if exe == "" {
				return false, errNoExe
			}
			return autostart.Enabled(exe)
		},
		SetAutostart: func(on bool) error { return setAutostart(exe, dir, on, logf) },
		CheckUpdates: appdata.LoadCheckUpdates,
		SetCheckUpdates: func(on bool) error {
			// The check runs on its own: the call answers at once.
			return setCheckUpdates(on, func() { go u.check(ctx, false) }, logf)
		},
		Theme:    appdata.LoadTheme,
		SetTheme: appdata.SaveTheme,
		CheckNow: func(context.Context) error {
			logf("Check now chosen in the window")
			return u.manualCheck(ctx)
		},
		Offered: func() (tag, url string, ok bool) {
			rel, ok := u.offered()
			return rel.Tag, rel.Page, ok
		},
		Install: func(context.Context) {
			logf("Update chosen in the window")
			u.installOffer(ctx)
		},
		Open:    shellOpen,
		Reload:  sup.Reload,
		Apply:   sup.Apply,
		LogPath: appdata.LogPath(),
		Logf:    logf,
	}
}
