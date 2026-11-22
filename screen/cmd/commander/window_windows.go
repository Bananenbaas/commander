// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
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
)

// acceptRetry is the wait after a failed Accept before the next one.
const acceptRetry = time.Second

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
		messageBox("Commander's window could not be opened:\n\n"+err.Error(), windows.MB_ICONWARNING)
	}
	return 1
}

// windowServer is the tray process's side of the window: the pipe it
// listens on, the connection it serves and the control service behind it.
type windowServer struct {
	ln   *pipe.Listener // nil when listening failed
	err  error          // why ln is nil
	svc  *control.Service
	conn windowConn
	ctl  *windowCtl
	logf func(string, ...any)
	done chan struct{} // closed when the accept loop has returned
}

// newWindowServer listens on the user's pipe. A failure is logged and kept:
// the tray goes on, and Open Commander says why there is no window.
func newWindowServer(svc *control.Service, exe string, logf func(string, ...any)) *windowServer {
	ws := &windowServer{svc: svc, logf: logf, done: make(chan struct{})}
	name, err := pipe.Name()
	if err == nil {
		ws.ln, err = pipe.Listen(name)
	}
	if err != nil {
		ws.err = err
		logf("the window cannot be offered: %v", err)
	}
	ws.ctl = &windowCtl{
		connected: ws.conn.connected,
		show:      svc.Show,
		runtime:   webviewRuntime,
		start: func() error {
			if ws.err != nil {
				return fmt.Errorf("Commander cannot listen for its window: %w", ws.err)
			}
			if exe == "" {
				return errors.New("Commander cannot find its own program file")
			}
			logf("starting the window")
			return startProgram(exe, []string{"--window"})
		},
		tell: func(text string) { messageBox(text, windows.MB_ICONINFORMATION) },
	}
	return ws
}

// serve accepts window connections until stop, serving one at a time; a
// second one while a window is connected is closed at once.
func (ws *windowServer) serve() {
	defer close(ws.done)
	if ws.ln == nil {
		return
	}
	for {
		c, err := ws.ln.Accept()
		if errors.Is(err, pipe.ErrClosed) {
			return
		}
		if err != nil {
			ws.logf("window connection not accepted: %v", err)
			time.Sleep(acceptRetry)
			continue
		}
		sc := &servedConn{ReadWriteCloser: c, wc: &ws.conn}
		if !ws.conn.take(sc) {
			ws.logf("a second window connection was closed: a window is already connected")
			c.Close()
			continue
		}
		ws.ctl.attached()
		ws.logf("window connected")
		go func() {
			err := bridge.Serve(context.Background(), sc, ws.svc.Handle, ws.svc.Events())
			sc.Close()
			if err != nil {
				ws.logf("window connection ended: %v", err)
			} else {
				ws.logf("window disconnected")
			}
		}()
	}
}

// stop closes the listener and the live connection, so the window process
// sees its connection end and closes, and stops the control service. It does
// not wait for calls still running (a question box may be open).
func (ws *windowServer) stop() {
	if ws.ln != nil {
		if err := ws.ln.Close(); err != nil {
			ws.logf("closing the window pipe: %v", err)
		}
		<-ws.done
	}
	ws.conn.closeAll()
	ws.svc.Close()
}

// controlDeps is what the window's calls use; the settings stay in step with
// the tray menu, which reads them anew every time it opens.
func controlDeps(ctx context.Context, exe, dir string, u *updater, logf func(string, ...any)) control.Deps {
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
		CheckNow: func(context.Context) {
			logf("Check now chosen in the window")
			u.check(ctx, true)
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
		LogPath: appdata.LogPath(),
		Logf:    logf,
	}
}
