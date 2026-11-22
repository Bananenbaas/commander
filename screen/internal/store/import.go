// SPDX-License-Identifier: GPL-2.0-or-later

package store

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
)

// maxPictureSize is the largest file ImportPicture takes.
const maxPictureSize = 20 << 20

// ImportPicture copies the PNG file at path into the configuration folder, to
// pictures/ (kind "picture") or symbols/ (kind "symbol"), and returns its
// path relative to the folder, with slashes. The file keeps its base name in
// lower case with characters outside a-z, 0-9, "-", "_" and "." replaced by
// "-". A file already there with the same bytes is reused; with other bytes
// the copy gets -2, -3 and so on before ".png".
func ImportPicture(dir, path, kind string) (string, error) {
	var sub string
	switch kind {
	case "picture":
		sub = "pictures"
	case "symbol":
		sub = "symbols"
	default:
		return "", fmt.Errorf("unknown kind %q", kind)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "", errors.New("That file cannot be read.")
	}
	if fi.IsDir() {
		return "", errors.New("That is a folder, not a picture.")
	}
	if fi.Size() > maxPictureSize {
		return "", errors.New("That file is larger than 20 MB.")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("That file cannot be read.")
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		return "", errors.New("That file is not a PNG picture.")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	stem := cleanName(strings.TrimSuffix(strings.ToLower(filepath.Base(path)), ".png"))
	saveMu.Lock()
	defer saveMu.Unlock()
	for n := 1; ; n++ {
		name := stem + ".png"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.png", stem, n)
		}
		rel := sub + "/" + name
		existing, err := os.ReadFile(filepath.Join(abs, sub, name))
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := newWriter(abs).put(rel, data); err != nil {
				return "", err
			}
			return rel, nil
		case err != nil:
			return "", err
		case bytes.Equal(existing, data):
			return rel, nil
		}
	}
}

// cleanName keeps a-z, 0-9, "-", "_" and "." of a lower-case name and turns
// everything else into "-".
func cleanName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "picture"
	}
	return b.String()
}
