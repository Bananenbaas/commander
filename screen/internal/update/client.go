// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	// ReleasesURL lists the newest releases of the project.
	ReleasesURL = "https://api.github.com/repos/Bananenbaas/commander/releases?per_page=20"
	// MaxSize is the largest body Commander accepts for a download.
	MaxSize = 64 << 20

	listTimeout     = 15 * time.Second
	downloadTimeout = 5 * time.Minute
	maxListBody     = 4 << 20
	maxSumsBody     = 1 << 20
)

// Client talks to GitHub. The zero value of HTTP is a client without a timeout
// of its own; every call sets its own deadline on the context it is given.
type Client struct {
	HTTP      *http.Client
	ListURL   string
	UserAgent string
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{}
}

// get starts a GET and returns the response, which the caller must close. A
// status outside 2xx is an error.
func (c *Client) get(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		resp.Body.Close()
		return nil, fmt.Errorf("server answered %s", resp.Status)
	}
	return resp, nil
}

// Releases fetches the release list.
func (c *Client) Releases(ctx context.Context) ([]Release, error) {
	url := c.ListURL
	if url == "" {
		url = ReleasesURL
	}
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	resp, err := c.get(ctx, url, "application/vnd.github+json")
	if err != nil {
		return nil, fmt.Errorf("fetching the release list: %w", err)
	}
	defer resp.Body.Close()
	var rels []Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxListBody)).Decode(&rels); err != nil {
		return nil, fmt.Errorf("reading the release list: %w", err)
	}
	return rels, nil
}

// Download fetches SumsAsset, then streams ExeAsset of rel into dst and checks
// it against the sum. dst must not exist; on any failure it is removed again.
func (c *Client) Download(ctx context.Context, rel Release, dst string) error {
	exe, ok := rel.Asset(ExeAsset)
	if !ok {
		return fmt.Errorf("release %s has no %s", rel.Tag, ExeAsset)
	}
	sumsAsset, ok := rel.Asset(SumsAsset)
	if !ok {
		return fmt.Errorf("release %s has no %s", rel.Tag, SumsAsset)
	}

	sums, err := c.fetchSums(ctx, sumsAsset.URL)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", SumsAsset, err)
	}
	want, err := SumFor(sums, ExeAsset)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o755)
	if err != nil {
		return fmt.Errorf("creating %s: %w", dst, err)
	}
	err = c.fetchExe(ctx, exe.URL, f, want)
	if cerr := f.Close(); err == nil && cerr != nil {
		err = fmt.Errorf("writing %s: %w", dst, cerr)
	}
	if err != nil {
		os.Remove(dst)
		return err
	}
	return nil
}

func (c *Client) fetchSums(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, listTimeout)
	defer cancel()
	resp, err := c.get(ctx, url, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSumsBody))
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (c *Client) fetchExe(ctx context.Context, url string, w io.Writer, want [32]byte) error {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	resp, err := c.get(ctx, url, "")
	if err != nil {
		return fmt.Errorf("downloading %s: %w", ExeAsset, err)
	}
	defer resp.Body.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(resp.Body, MaxSize+1))
	if err != nil {
		return fmt.Errorf("downloading %s: %w", ExeAsset, err)
	}
	if n > MaxSize {
		return fmt.Errorf("downloading %s: larger than %d bytes", ExeAsset, int64(MaxSize))
	}
	if subtle.ConstantTimeCompare(h.Sum(nil), want[:]) != 1 {
		return fmt.Errorf("checksum of %s does not match %s", ExeAsset, SumsAsset)
	}
	return nil
}
