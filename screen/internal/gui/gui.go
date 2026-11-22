// SPDX-License-Identifier: GPL-2.0-or-later

// Package gui is Commander's window: a separate process (commander.exe
// --window) that shows the web page in web/ in a WebView2 control and
// relays the page's calls to Commander over the named pipe. Keeping it in
// its own process means a WebView2 failure, which ends the process, never
// takes the keyboard loop down with it.
//
// The page posts calls as JSON strings, {"id": n, "call": "...", "args":
// ...}, and gets back {"id": n, "result": ...} or {"id": n, "error": "..."}.
// Events from Commander arrive as {"event": "...", "data": ...}. The call
// "__theme" with args "dark" or "light" is answered by the window itself: it
// sets the colour of the title bar and is never forwarded.
//
// The pages never open windows and never load anything from outside the
// embedded files, and the window process refuses both: every new window
// request (window.open, target=_blank, modified clicks) is dropped, every
// request is answered from the embedded files (403 for any other origin),
// and every permission request is denied. Files dropped onto the window are
// refused, and only messages whose source is a page of AppOrigin are
// relayed.
//
// Note: the WebView2 binding echoes every message the page posts back to the
// page, so the page must ignore incoming messages that carry "call".
package gui

// WebView2Error is an error of Run that comes from WebView2 itself, not
// from the pipe or the window around it.
type WebView2Error struct{ Err error }

func (e *WebView2Error) Error() string { return e.Err.Error() }
func (e *WebView2Error) Unwrap() error { return e.Err }

// Options are the settings of the window.
type Options struct {
	DataDir  string // WebView2 user data, %LOCALAPPDATA%\Commander\WebView2
	DevTools bool   // only for development builds (version "dev")
	Icon     []byte // ICO for the window
	Logf     func(string, ...any)
}

func (o Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}
