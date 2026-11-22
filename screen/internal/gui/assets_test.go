// SPDX-License-Identifier: GPL-2.0-or-later

package gui

import (
	"regexp"
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

// pageFiles are the files the window's pages are made of.
var pageFiles = []string{
	"index.html",
	"app.css",
	"app.js",
	"BarlowCondensed-SemiBold.ttf",
	"OFL.txt",
	"helmet-256.png",
}

// textPages are the pages written by hand, checked for their content.
var textPages = []string{"index.html", "app.css", "app.js"}

func page(t *testing.T, name string) string {
	t.Helper()
	status, _, body := Resource(AppOrigin + "/" + name)
	if status != 200 {
		t.Fatalf("%s: got %d, want 200", name, status)
	}
	return string(body)
}

func TestPagesExist(t *testing.T) {
	for _, name := range pageFiles {
		status, _, body := Resource(AppOrigin + "/" + name)
		if status != 200 || len(body) == 0 {
			t.Errorf("%s: got %d (%d bytes), want 200 with a body", name, status, len(body))
		}
	}
}

func TestPagesLoadNothingExternal(t *testing.T) {
	url := regexp.MustCompile(`https?://[^\s"'<>)]*`)
	for _, name := range textPages {
		for _, u := range url.FindAllString(page(t, name), -1) {
			if strings.HasPrefix(u, AppOrigin) {
				continue
			}
			t.Errorf("%s: refers to %q", name, u)
		}
	}
}

const wantCSP = "default-src 'none'; img-src 'self' data:; style-src 'self'; script-src 'self'; font-src 'self'; connect-src 'none'; base-uri 'none'; form-action 'none'"

func TestPagesHaveCSP(t *testing.T) {
	html := page(t, "index.html")
	if !strings.Contains(html, `http-equiv="Content-Security-Policy" content="`+wantCSP+`"`) {
		t.Errorf("index.html lacks the content security policy %q", wantCSP)
	}
	if strings.Contains(html, "<style") || strings.Contains(html, " style=") {
		t.Errorf("index.html has inline style")
	}
	if regexp.MustCompile(`<script(\s[^>]*)?>[^<]`).MatchString(html) {
		t.Errorf("index.html has an inline script")
	}
	if regexp.MustCompile(`\son[a-z]+=`).MatchString(html) {
		t.Errorf("index.html has an inline event handler")
	}
	for _, want := range []string{`<link rel="stylesheet" href="app.css">`, `<script src="app.js"`} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html lacks %s", want)
		}
	}
}

func TestNoEmDashInPages(t *testing.T) {
	for _, name := range textPages {
		if strings.ContainsRune(page(t, name), '\u2014') {
			t.Errorf("%s contains an em dash", name)
		}
	}
}

func TestPagesHaveSPDX(t *testing.T) {
	want := map[string]string{
		"index.html": "<!-- SPDX-License-Identifier: GPL-2.0-or-later -->",
		"app.css":    "/* SPDX-License-Identifier: GPL-2.0-or-later */",
		"app.js":     "/* SPDX-License-Identifier: GPL-2.0-or-later */",
	}
	for name, line := range want {
		first, _, _ := strings.Cut(page(t, name), "\n")
		if strings.TrimRight(first, "\r") != line {
			t.Errorf("%s: first line is %q, want %q", name, first, line)
		}
	}
}

func TestPagesUseNoUnsafeDOM(t *testing.T) {
	js := page(t, "app.js")
	for _, bad := range []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "window.open", "eval(", "new Function", "_blank"} {
		if strings.Contains(js, bad) {
			t.Errorf("app.js uses %s", bad)
		}
	}
}
