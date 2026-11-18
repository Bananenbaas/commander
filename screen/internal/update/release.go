// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// The files a release must carry to be installable.
const (
	ExeAsset  = "commander-windows-amd64.exe"
	SumsAsset = "SHA256SUMS.txt"
)

// Asset is one file attached to a release.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Release is one entry of GitHub's release list.
type Release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Page       string  `json:"html_url"`
	Assets     []Asset `json:"assets"`
}

// Asset returns the attached file with the given name.
func (r Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// Pick returns the newest release that is newer than current and can be
// installed: not a draft, tagged with a valid version and carrying both ExeAsset
// and SumsAsset. ok is false when there is none. err is set when current is
// not a version, for example a development build.
func Pick(current string, releases []Release) (rel Release, ok bool, err error) {
	cur, err := ParseVersion(current)
	if err != nil {
		return Release{}, false, err
	}
	var best Version
	for _, r := range releases {
		if r.Draft {
			continue
		}
		v, perr := ParseVersion(r.Tag)
		if perr != nil || v.Compare(cur) <= 0 {
			continue
		}
		if _, has := r.Asset(ExeAsset); !has {
			continue
		}
		if _, has := r.Asset(SumsAsset); !has {
			continue
		}
		if !ok || v.Compare(best) > 0 {
			rel, best, ok = r, v, true
		}
	}
	return rel, ok, nil
}

// SumFor finds the SHA-256 of the named file in the text of a SHA256SUMS.txt:
// lines of 64 hex digits, spaces and the name, as sha256sum writes them. A *
// before the name (binary mode) and upper-case digits are accepted.
func SumFor(sums []byte, name string) ([32]byte, error) {
	var sum [32]byte
	for _, line := range strings.Split(string(sums), "\n") {
		line = strings.TrimRight(line, "\r")
		digest, rest, found := strings.Cut(line, " ")
		if !found {
			continue
		}
		rest = strings.TrimLeft(rest, " ")
		rest = strings.TrimPrefix(rest, "*")
		if rest != name {
			continue
		}
		if len(digest) != 64 {
			return sum, fmt.Errorf("checksum line for %s is not 64 hex digits", name)
		}
		b, err := hex.DecodeString(digest)
		if err != nil {
			return sum, fmt.Errorf("checksum line for %s is not hex: %w", name, err)
		}
		copy(sum[:], b)
		return sum, nil
	}
	return sum, fmt.Errorf("no checksum for %s in %s", name, SumsAsset)
}
