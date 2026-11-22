// SPDX-License-Identifier: GPL-2.0-or-later

package gui

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestResourceServesIndex(t *testing.T) {
	for _, p := range []string{"/", "/index.html", "/?x=1", "/index.html#top"} {
		status, ctype, body := Resource(AppOrigin + p)
		if status != 200 || ctype != "text/html; charset=utf-8" {
			t.Errorf("%s: got %d %q, want 200 text/html", p, status, ctype)
		}
		if !strings.Contains(string(body), "Commander") {
			t.Errorf("%s: body does not look like the index page", p)
		}
	}
}

// withFiles swaps the served files for the test.
func withFiles(t *testing.T, fs fstest.MapFS) {
	t.Helper()
	old := files
	files = fs
	t.Cleanup(func() { files = old })
}

func TestResourceTypes(t *testing.T) {
	withFiles(t, fstest.MapFS{
		"index.html":      {Data: []byte("<!doctype html>")},
		"app.css":         {Data: []byte("a{}")},
		"app.js":          {Data: []byte("1")},
		"img/helmet.png":  {Data: []byte("png")},
		"fonts/inter.ttf": {Data: []byte("ttf")},
		"LICENSE.txt":     {Data: []byte("text")},
		"favicon.ico":     {Data: []byte("ico")},
	})
	cases := map[string]string{
		"/app.css":         "text/css; charset=utf-8",
		"/app.js":          "text/javascript; charset=utf-8",
		"/img/helmet.png":  "image/png",
		"/fonts/inter.ttf": "font/ttf",
		"/LICENSE.txt":     "text/plain; charset=utf-8",
		"/favicon.ico":     "image/x-icon",
	}
	for p, want := range cases {
		status, ctype, body := Resource(AppOrigin + p)
		if status != 200 || ctype != want || len(body) == 0 {
			t.Errorf("%s: got %d %q (%d bytes), want 200 %q", p, status, ctype, len(body), want)
		}
	}
}

func TestResourceRefusesOtherOrigins(t *testing.T) {
	for _, u := range []string{
		"https://example.org/x",
		"http://commander.invalid/",
		"https://commander.invalid.evil/",
		"file:///C:/x",
		"https://commander.invalid:443/",
		"https://user@commander.invalid/",
		"https://COMMANDER.invalid/",
		"",
		"::",
		"commander.invalid/index.html",
	} {
		if status, _, _ := Resource(u); status != 403 {
			t.Errorf("%q: got %d, want 403", u, status)
		}
	}
}

func TestResourceUnknownPath(t *testing.T) {
	withFiles(t, fstest.MapFS{
		"index.html":  {Data: []byte("<!doctype html>")},
		"img/a.png":   {Data: []byte("png")},
		"tool.exe":    {Data: []byte("MZ")},
		"noextension": {Data: []byte("x")},
	})
	for _, p := range []string{"/nope.html", "/img", "/img/", "/tool.exe", "/noextension", "/img/b.png"} {
		if status, _, _ := Resource(AppOrigin + p); status != 404 {
			t.Errorf("%s: got %d, want 404", p, status)
		}
	}
}

func TestResourceNoTraversal(t *testing.T) {
	for _, p := range []string{
		"/../go.mod",
		"/%2e%2e/go.mod",
		"/%2E%2E/gui.go",
		"/web/../../go.mod",
		"/..%2fgo.mod",
		"/..%5cgo.mod",
		"/%5c..%5cgo.mod",
		"/./index.html/..",
		"/gui.go",
		"/web/index.html",
	} {
		if status, _, _ := Resource(AppOrigin + p); status != 404 {
			t.Errorf("%s: got %d, want 404", p, status)
		}
	}
}

func TestResponseHeaders(t *testing.T) {
	h := responseHeaders("text/css; charset=utf-8")
	for _, want := range []string{
		"Content-Type: text/css; charset=utf-8",
		"Cache-Control: no-store",
		"X-Content-Type-Options: nosniff",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("headers %q lack %q", h, want)
		}
	}
	if strings.HasSuffix(h, "\r\n") || strings.Contains(h, "\n\n") {
		t.Errorf("headers %q have a stray line break", h)
	}
}
