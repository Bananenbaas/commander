// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeWindow records what windowCtl does.
type fakeWindow struct {
	mu        sync.Mutex
	connected bool
	runtime   string
	rtErr     error
	startErr  error
	shows     int
	starts    int
	tells     []string
	now       time.Time
}

func (f *fakeWindow) ctl() *windowCtl {
	return &windowCtl{
		connected: func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.connected },
		show:      func() { f.mu.Lock(); f.shows++; f.mu.Unlock() },
		runtime:   func() (string, error) { return f.runtime, f.rtErr },
		start: func() error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.starts++
			return f.startErr
		},
		tell: func(text string) { f.mu.Lock(); f.tells = append(f.tells, text); f.mu.Unlock() },
		now:  func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now },
	}
}

func (f *fakeWindow) advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}

func TestWindowOpen(t *testing.T) {
	cases := []struct {
		name      string
		fake      *fakeWindow
		starting  bool
		shows     int
		starts    int
		tellStart string // the start of the one message box, "" for none
	}{
		{name: "connected shows only", fake: &fakeWindow{connected: true, runtime: "120.0"}, shows: 1},
		{name: "connected while a start is under way shows", fake: &fakeWindow{connected: true, runtime: "120.0"}, starting: true, shows: 1},
		{name: "start under way does nothing", fake: &fakeWindow{runtime: "120.0"}, starting: true},
		{name: "runtime error tells, no start", fake: &fakeWindow{rtErr: errors.New("not found")}, tellStart: webviewMissingText},
		{name: "empty runtime tells, no start", fake: &fakeWindow{}, tellStart: webviewMissingText},
		{name: "start error tells", fake: &fakeWindow{runtime: "120.0", startErr: errors.New("access denied")}, starts: 1, tellStart: "Commander's window could not be started: access denied"},
		{name: "normal starts", fake: &fakeWindow{runtime: "120.0"}, starts: 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := c.fake
			f.now = time.Unix(1000, 0)
			w := f.ctl()
			if c.starting {
				w.starting, w.startedAt = true, f.now
			}
			w.open()
			if f.shows != c.shows || f.starts != c.starts {
				t.Errorf("shows %d, starts %d; want %d, %d", f.shows, f.starts, c.shows, c.starts)
			}
			switch {
			case c.tellStart == "" && len(f.tells) != 0:
				t.Errorf("told %q, want nothing", f.tells)
			case c.tellStart != "" && (len(f.tells) != 1 || !strings.HasPrefix(f.tells[0], c.tellStart)):
				t.Errorf("told %q, want one starting with %q", f.tells, c.tellStart)
			}
		})
	}
}

func TestWindowMissingText(t *testing.T) {
	want := "Commander's window needs the Microsoft Edge WebView2 Runtime, which is not installed. " +
		"Download it from https://developer.microsoft.com/microsoft-edge/webview2/ and open Commander again."
	if webviewMissingText != want {
		t.Errorf("webviewMissingText = %q, want %q", webviewMissingText, want)
	}
	if notRunningText != "Commander is not running. Start Commander first." {
		t.Errorf("notRunningText = %q", notRunningText)
	}
}

func TestWindowOpenStartsOnce(t *testing.T) {
	f := fakeWindow{runtime: "120.0", now: time.Unix(1000, 0)}
	w := f.ctl()
	w.open()
	f.advance(startWait - time.Second)
	w.open() // within 15 s and not connected: still starting
	if f.starts != 1 {
		t.Fatalf("starts = %d after two opens within %v, want 1", f.starts, startWait)
	}
	f.advance(2 * time.Second)
	w.open() // the start never connected: try again
	if f.starts != 2 {
		t.Fatalf("starts = %d once %v passed, want 2", f.starts, startWait)
	}
}

func TestWindowOpenAfterConnectAndClose(t *testing.T) {
	f := fakeWindow{runtime: "120.0", now: time.Unix(1000, 0)}
	w := f.ctl()
	w.open()
	// The window connects and is closed again within the 15 s.
	w.attached()
	f.advance(time.Second)
	w.open()
	if f.starts != 2 {
		t.Fatalf("starts = %d, want 2: a window that connected ends the start", f.starts)
	}
}

func TestWindowOpenConcurrent(t *testing.T) {
	f := fakeWindow{runtime: "120.0", now: time.Unix(1000, 0)}
	w := f.ctl()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); w.open() }()
	}
	wg.Wait()
	if f.starts != 1 {
		t.Fatalf("starts = %d after concurrent opens, want 1", f.starts)
	}
}

func TestWindowOpenTellsOnce(t *testing.T) {
	f := fakeWindow{now: time.Unix(1000, 0)}
	release := make(chan struct{})
	told := make(chan struct{}, 8)
	w := f.ctl()
	w.tell = func(string) { told <- struct{}{}; <-release }
	done := make(chan struct{})
	go func() { w.open(); close(done) }()
	<-told
	w.open() // the first message box is still open: nothing more
	close(release)
	<-done
	if n := len(told); n != 0 {
		t.Fatalf("%d more message boxes while one was open, want 0", n)
	}
}

type fakeConn struct {
	closed int
}

func (c *fakeConn) Read([]byte) (int, error)  { return 0, nil }
func (c *fakeConn) Write([]byte) (int, error) { return 0, nil }
func (c *fakeConn) Close() error              { c.closed++; return nil }

func TestWindowConnOneAtATime(t *testing.T) {
	var wc windowConn
	a := &servedConn{ReadWriteCloser: &fakeConn{}, wc: &wc}
	if !wc.take(a) || !wc.connected() {
		t.Fatal("the first connection was not taken")
	}
	if wc.take(&servedConn{ReadWriteCloser: &fakeConn{}, wc: &wc}) {
		t.Fatal("a second connection was taken while one is live")
	}
	a.Close() // the window went away
	if wc.connected() {
		t.Fatal("still connected after the live connection was closed")
	}
	b := &servedConn{ReadWriteCloser: &fakeConn{}, wc: &wc}
	if !wc.take(b) {
		t.Fatal("a new connection was not taken after the first one ended")
	}
	a.Close() // a late second close of the old one changes nothing
	if !wc.connected() {
		t.Fatal("closing the old connection again dropped the new one")
	}
}

func TestWindowConnCloseAll(t *testing.T) {
	var wc windowConn
	inner := &fakeConn{}
	c := &servedConn{ReadWriteCloser: inner, wc: &wc}
	wc.take(c)
	wc.closeAll()
	if inner.closed != 1 || wc.connected() {
		t.Fatalf("closeAll: closed %d times, connected %v", inner.closed, wc.connected())
	}
	if wc.take(&servedConn{ReadWriteCloser: &fakeConn{}, wc: &wc}) {
		t.Fatal("a connection was taken after closeAll")
	}
}
