// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleasesSendsHeaders(t *testing.T) {
	var gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		fmt.Fprint(w, `[{"tag_name":"v0.2.0","assets":[{"name":"a","browser_download_url":"u","size":1}]},{"tag_name":"v0.1.0","prerelease":true}]`)
	}))
	defer srv.Close()

	c := &Client{ListURL: srv.URL, UserAgent: "Commander/v0.1.0-beta.1"}
	rels, err := c.Releases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotUA != "Commander/v0.1.0-beta.1" {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if len(rels) != 2 || rels[0].Tag != "v0.2.0" || !rels[1].Prerelease || len(rels[0].Assets) != 1 {
		t.Errorf("releases = %+v", rels)
	}
}

func TestReleasesErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"rate limit", 403, `{"message":"API rate limit exceeded"}`},
		{"bad gateway", 502, ``},
		{"html", 200, `<html><body>maintenance</body></html>`},
		{"truncated", 200, `[{"tag_name":"v0.2.0","assets":[{"na`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c := &Client{ListURL: srv.URL, UserAgent: "Commander/test"}
			rels, err := c.Releases(context.Background())
			if err == nil {
				t.Fatalf("no error, got %+v", rels)
			}
		})
	}
}

// downloadServer serves a sums file and an executable and returns a release
// pointing at them.
func downloadServer(t *testing.T, exe func(w http.ResponseWriter), sums string) Release {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/exe", func(w http.ResponseWriter, r *http.Request) { exe(w) })
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, sums) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return Release{Tag: "v0.2.0", Assets: []Asset{
		{Name: ExeAsset, URL: srv.URL + "/exe"},
		{Name: SumsAsset, URL: srv.URL + "/sums"},
	}}
}

func sumLine(b []byte, name string) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]) + "  " + name + "\n"
}

func TestDownloadGood(t *testing.T) {
	data := []byte("MZ pretend executable")
	rel := downloadServer(t, func(w http.ResponseWriter) { w.Write(data) }, sumLine(data, ExeAsset))
	dst := filepath.Join(t.TempDir(), "commander.exe.new")
	c := &Client{UserAgent: "Commander/test"}
	if err := c.Download(context.Background(), rel, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != string(data) {
		t.Errorf("dst = %q, %v", got, err)
	}
}

func TestDownloadMismatch(t *testing.T) {
	rel := downloadServer(t, func(w http.ResponseWriter) { w.Write([]byte("real")) }, sumLine([]byte("other"), ExeAsset))
	dst := filepath.Join(t.TempDir(), "x.new")
	err := (&Client{}).Download(context.Background(), rel, dst)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v", err)
	}
	if _, serr := os.Stat(dst); !os.IsNotExist(serr) {
		t.Errorf("dst still there: %v", serr)
	}
}

func TestDownloadNoSumLine(t *testing.T) {
	rel := downloadServer(t, func(w http.ResponseWriter) { w.Write([]byte("real")) }, sumLine([]byte("real"), "other.exe"))
	dst := filepath.Join(t.TempDir(), "x.new")
	if err := (&Client{}).Download(context.Background(), rel, dst); err == nil {
		t.Fatal("no error")
	}
	if _, serr := os.Stat(dst); !os.IsNotExist(serr) {
		t.Errorf("dst created: %v", serr)
	}
}

func TestDownloadTooLarge(t *testing.T) {
	rel := downloadServer(t, func(w http.ResponseWriter) {
		chunk := make([]byte, 1<<20)
		for n := int64(0); n < MaxSize+1; n += int64(len(chunk)) {
			k := int64(len(chunk))
			if rem := MaxSize + 1 - n; rem < k {
				k = rem
			}
			if _, err := w.Write(chunk[:k]); err != nil {
				return
			}
		}
	}, sumLine([]byte("irrelevant"), ExeAsset))
	dst := filepath.Join(t.TempDir(), "x.new")
	err := (&Client{}).Download(context.Background(), rel, dst)
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("err = %v, want the size-limit error", err)
	}
	if _, serr := os.Stat(dst); !os.IsNotExist(serr) {
		t.Errorf("dst still there: %v", serr)
	}
}

func TestDownloadDstExists(t *testing.T) {
	data := []byte("new")
	rel := downloadServer(t, func(w http.ResponseWriter) { w.Write(data) }, sumLine(data, ExeAsset))
	dst := filepath.Join(t.TempDir(), "x.new")
	if err := os.WriteFile(dst, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&Client{}).Download(context.Background(), rel, dst); err == nil {
		t.Fatal("no error")
	}
	got, _ := os.ReadFile(dst)
	if string(got) != "precious" {
		t.Errorf("existing file changed: %q", got)
	}
}
