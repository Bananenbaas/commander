// SPDX-License-Identifier: GPL-2.0-or-later

// Package store is the only code that writes the configuration folder: layout
// files, game files and pictures. Every write is atomic, and a hand-written
// file is copied to backup/ before Commander overwrites it.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Version is the first 16 hex digits of the SHA-256 of data.
func Version(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}

// A Write is one file to put in the configuration folder.
type Write struct {
	Rel  string // relative to the folder, forward slashes
	Data []byte
}

// writer does the writes; tests replace its functions.
type writer struct {
	dir string
	now func() time.Time
	// writeFile and rename default to os.WriteFile and os.Rename.
	writeFile func(path string, data []byte) error
	rename    func(from, to string) error
}

func newWriter(dir string) *writer {
	return &writer{
		dir:       dir,
		now:       time.Now,
		writeFile: writeSynced,
		rename:    os.Rename,
	}
}

// writeSynced writes data to path and flushes it to disk before returning, so
// a power loss after the rename cannot leave an empty file.
func writeSynced(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (w *writer) abs(rel string) string { return filepath.Join(w.dir, filepath.FromSlash(rel)) }

// backup copies the file at rel to backup/ when encode(decode(its bytes))
// differs from its bytes; it returns the backup's path relative to the
// folder, or "" when no backup was needed or the file does not exist.
// lossless reports, for a file's bytes, whether Commander's encoding of it
// gives the same bytes.
func (w *writer) backup(rel string, lossless func([]byte) bool) (string, error) {
	data, err := os.ReadFile(w.abs(rel))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if lossless(data) {
		return "", nil
	}
	base := strings.ReplaceAll(strings.TrimSuffix(rel, path.Ext(rel)), "/", "-")
	stamp := w.now().Format("20060102-150405")
	if err := os.MkdirAll(w.abs("backup"), 0o755); err != nil {
		return "", err
	}
	for n := 1; ; n++ {
		name := fmt.Sprintf("backup/%s-%s.toml", base, stamp)
		if n > 1 {
			name = fmt.Sprintf("backup/%s-%s-%d.toml", base, stamp, n)
		}
		f, err := os.OpenFile(w.abs(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(data)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			os.Remove(w.abs(name))
			return "", errors.Join(werr, cerr)
		}
		return name, nil
	}
}

// put writes data to rel atomically (temporary file ".<base>.tmp-*" in the
// same folder, then rename); it creates the parent folder when needed and
// removes the temporary file when anything fails.
func (w *writer) put(rel string, data []byte) error {
	target := w.abs(rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	// CreateTemp makes the file 0600; saved files are 0644 like the others.
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := w.writeFile(name, data); err != nil {
		os.Remove(name)
		return err
	}
	if err := w.rename(name, target); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
