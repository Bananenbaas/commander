// SPDX-License-Identifier: GPL-2.0-or-later

// Package startenv records environment variables as they were when the
// program started and can put them back.
//
// The WebView2 binding (go-webview2's webviewloader) sets five WEBVIEW2_*
// variables in its package init, in every process that links it, so that
// no outside setting or registry override reaches the window's browser:
//
//	WEBVIEW2_PIPE_FOR_SCRIPT_DEBUGGER     ""
//	WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS ""
//	WEBVIEW2_RELEASE_CHANNEL_PREFERENCE   "0"
//	WEBVIEW2_BROWSER_EXECUTABLE_FOLDER    ""
//	WEBVIEW2_USER_DATA_FOLDER             ""
//
// The window process wants that. The tray process shows no browser, but
// every program it starts from a KB7 button would inherit the variables and
// a WebView2 program among them would lose its own settings. So the tray
// puts them back as they were with Restore before it starts anything.
//
// This package imports nothing that imports the binding, and its import
// path sorts before the binding's, so Go's package initialization order
// (since Go 1.21: by import path among the packages whose imports are ready)
// runs its init first: start holds the values from before the binding's
// init.
package startenv

import (
	"errors"
	"os"
)

// Names are the variables the binding's init sets.
var Names = []string{
	"WEBVIEW2_PIPE_FOR_SCRIPT_DEBUGGER",
	"WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS",
	"WEBVIEW2_RELEASE_CHANNEL_PREFERENCE",
	"WEBVIEW2_BROWSER_EXECUTABLE_FOLDER",
	"WEBVIEW2_USER_DATA_FOLDER",
}

// Value is a variable's value; Set is false when the variable is not there
// at all, which differs from one set to "".
type Value struct {
	Text string
	Set  bool
}

// Change is one step of a restore: set Name to Text, or remove it.
type Change struct {
	Name  string
	Text  string
	Unset bool
}

var lookup = os.LookupEnv

// start holds Names as they were when the program started.
var start = record(Names, lookup)

// At returns the variable name as it was at the start; only Names are
// recorded.
func At(name string) (Value, bool) {
	v, ok := start[name]
	return v, ok
}

// Restore puts Names back as they were at the start.
func Restore() error {
	var errs []error
	for _, c := range changes(Names, start, record(Names, lookup)) {
		if c.Unset {
			errs = append(errs, os.Unsetenv(c.Name))
		} else {
			errs = append(errs, os.Setenv(c.Name, c.Text))
		}
	}
	return errors.Join(errs...)
}

// record reads names with look.
func record(names []string, look func(string) (string, bool)) map[string]Value {
	out := make(map[string]Value, len(names))
	for _, n := range names {
		t, ok := look(n)
		out[n] = Value{Text: t, Set: ok}
	}
	return out
}

// changes are the steps that turn now back into before, in the order of
// names; a variable that did not change needs none.
func changes(names []string, before, now map[string]Value) []Change {
	var out []Change
	for _, n := range names {
		b, c := before[n], now[n]
		if b == c {
			continue
		}
		if !b.Set {
			out = append(out, Change{Name: n, Unset: true})
		} else {
			out = append(out, Change{Name: n, Text: b.Text})
		}
	}
	return out
}
