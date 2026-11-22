// SPDX-License-Identifier: GPL-2.0-or-later

package gui

import (
	"embed"
	"io/fs"
	"net/url"
	"path"
	"strings"
)

// AppOrigin is the origin the page is served from. The .invalid top-level
// domain never resolves, so no request for it can leave the machine.
const AppOrigin = "https://commander.invalid"

const appHost = "commander.invalid"

//go:embed web
var embedded embed.FS

// files are the served files, rooted at web/. Tests replace it.
var files fs.FS = mustSub(embedded, "web")

func mustSub(f fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(f, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

// contentTypes are the only kinds of file served; any other is 404.
var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
	".png":  "image/png",
	".ttf":  "font/ttf",
	".txt":  "text/plain; charset=utf-8",
	".ico":  "image/x-icon",
}

const textPlain = "text/plain; charset=utf-8"

// Resource answers a request URL: status, content type and body. URLs
// outside AppOrigin get 403; unknown paths 404; "/" serves index.html.
func Resource(rawURL string) (status int, contentType string, body []byte) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Host != appHost || u.User != nil || u.Opaque != "" {
		return 403, textPlain, []byte("forbidden")
	}
	name, ok := fileName(u.Path)
	if !ok {
		return 404, textPlain, []byte("not found")
	}
	ctype, ok := contentTypes[path.Ext(name)]
	if !ok {
		return 404, textPlain, []byte("not found")
	}
	if st, err := fs.Stat(files, name); err != nil || !st.Mode().IsRegular() {
		return 404, textPlain, []byte("not found")
	}
	b, err := fs.ReadFile(files, name)
	if err != nil {
		return 404, textPlain, []byte("not found")
	}
	return 200, ctype, b
}

// fileName turns the unescaped URL path into a name in files. A path with a
// ".." segment, a backslash or a NUL is refused outright, so nothing can
// climb out of the root however it is spelt.
func fileName(p string) (string, bool) {
	if strings.ContainsAny(p, "\\\x00") {
		return "", false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return "", false
		}
	}
	p = path.Clean("/" + p)
	if p == "/" {
		return "index.html", true
	}
	name := strings.TrimPrefix(p, "/")
	if !fs.ValidPath(name) {
		return "", false
	}
	return name, true
}

// reasonPhrase is the HTTP reason for the statuses Resource gives.
func reasonPhrase(status int) string {
	switch status {
	case 200:
		return "OK"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	}
	return "Error"
}

// responseHeaders are the headers of every answer, in the CRLF-separated
// form WebView2 takes.
func responseHeaders(contentType string) string {
	return strings.Join([]string{
		"Content-Type: " + contentType,
		"Cache-Control: no-store",
		"X-Content-Type-Options: nosniff",
	}, "\r\n")
}
