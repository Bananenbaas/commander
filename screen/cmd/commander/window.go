// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"io"
	"sync"
	"time"
)

// startWait is how long a started window process has to connect before
// Open Commander starts another one.
const startWait = 15 * time.Second

// Texts of the window's message boxes.
const (
	webviewMissingText = "Commander's window needs the Microsoft Edge WebView2 Runtime, which is not installed. " +
		"Download it from https://developer.microsoft.com/microsoft-edge/webview2/ and open Commander again."
	startFailedText = "Commander's window could not be started: "
	// openFailedText and webviewRepairText are the window process's box
	// when WebView2 is installed but the window still cannot be opened.
	openFailedText    = "Commander's window could not be opened: "
	webviewRepairText = "If this keeps happening, install the Microsoft Edge WebView2 Runtime again from " +
		"https://developer.microsoft.com/microsoft-edge/webview2/ and open Commander again."
	notRunningText = "Commander is not running. Start Commander first."
)

// windowCtl decides what Open Commander and the left click on the tray icon
// do: bring the connected window to the front, or start the window process.
// open may be called concurrently and more than once for one click.
type windowCtl struct {
	connected func() bool            // a window is connected to the pipe
	show      func()                 // send "show"
	runtime   func() (string, error) // WebView2 runtime version; "" or error = missing
	start     func() error           // start commander.exe --window
	tell      func(text string)      // message box
	now       func() time.Time       // time.Now when nil

	mu        sync.Mutex
	starting  bool      // a start is under way (until it connects or startWait passed)
	startedAt time.Time // when that start was made
	telling   bool      // a message box of open is showing
}

func (w *windowCtl) clock() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

// open is what Open Commander and the left click run.
func (w *windowCtl) open() {
	w.mu.Lock()
	if w.connected() {
		w.starting = false
		w.mu.Unlock()
		w.show()
		return
	}
	if w.telling || (w.starting && w.clock().Sub(w.startedAt) < startWait) {
		w.mu.Unlock()
		return
	}
	w.starting = false
	var text string
	if v, err := w.runtime(); err != nil || v == "" {
		text = webviewMissingText
	} else if err := w.start(); err != nil {
		text = startFailedText + err.Error()
	} else {
		w.starting, w.startedAt = true, w.clock()
	}
	if text == "" {
		w.mu.Unlock()
		return
	}
	// The box waits for the user; the lock is not held meanwhile, and
	// further clicks do nothing until it is closed.
	w.telling = true
	w.mu.Unlock()
	w.tell(text)
	w.mu.Lock()
	w.telling = false
	w.mu.Unlock()
}

// attached records that a window has connected: the start is over.
func (w *windowCtl) attached() {
	w.mu.Lock()
	w.starting = false
	w.mu.Unlock()
}

// windowConn holds the one window connection Commander serves at a time.
type windowConn struct {
	mu     sync.Mutex
	live   io.Closer // the connection being served, or nil
	closed bool      // closeAll was called: no new connection is taken
}

// take makes c the live connection; false means one is already live or
// Commander is stopping, and the caller closes c.
func (wc *windowConn) take(c io.Closer) bool {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	if wc.closed || wc.live != nil {
		return false
	}
	wc.live = c
	return true
}

// drop forgets c when it is the live connection.
func (wc *windowConn) drop(c io.Closer) {
	wc.mu.Lock()
	if wc.live == c {
		wc.live = nil
	}
	wc.mu.Unlock()
}

// connected reports whether a window connection is live.
func (wc *windowConn) connected() bool {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	return wc.live != nil
}

// livePID is the process ID of the live connection's window, 0 when none
// is live or the ID could not be read.
func (wc *windowConn) livePID() uint32 {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	if sc, ok := wc.live.(*servedConn); ok {
		return sc.pid
	}
	return 0
}

// closeAll closes the live connection and refuses any later one.
func (wc *windowConn) closeAll() {
	wc.mu.Lock()
	live := wc.live
	wc.closed = true
	wc.mu.Unlock()
	if live != nil {
		live.Close()
	}
}

// servedConn is a window connection that leaves wc as soon as it is
// closed, which bridge.Serve does the moment the connection ends, before it
// waits for calls still running. A new window can connect meanwhile.
type servedConn struct {
	io.ReadWriteCloser
	wc   *windowConn
	pid  uint32 // the window process, 0 when unknown
	once sync.Once
}

func (s *servedConn) Close() error {
	err := s.ReadWriteCloser.Close()
	s.once.Do(func() { s.wc.drop(s) })
	return err
}
