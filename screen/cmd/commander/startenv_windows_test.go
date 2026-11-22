// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package main

import (
	"os"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/startenv"
)

// TestStartEnvRecordedBeforeWebView2Init checks that startenv's init runs
// before the WebView2 binding's, which this test binary links too: the
// binding sets WEBVIEW2_RELEASE_CHANNEL_PREFERENCE to "0", startenv must have
// seen the value from before.
func TestStartEnvRecordedBeforeWebView2Init(t *testing.T) {
	const name = "WEBVIEW2_RELEASE_CHANNEL_PREFERENCE"
	at, ok := startenv.At(name)
	if !ok {
		t.Fatalf("%s not recorded", name)
	}
	if at.Set && at.Text == "0" {
		t.Skipf("%s was already 0 when the test started", name)
	}
	if v, set := os.LookupEnv(name); !set || v != "0" {
		t.Fatalf("%s = %q, %v: the binding's init did not set it", name, v, set)
	}
	if err := startenv.Restore(); err != nil {
		t.Fatal(err)
	}
	for _, n := range startenv.Names {
		at, _ := startenv.At(n)
		if v, set := os.LookupEnv(n); v != at.Text || set != at.Set {
			t.Errorf("after Restore %s = %q, %v, want %q, %v", n, v, set, at.Text, at.Set)
		}
	}
}
