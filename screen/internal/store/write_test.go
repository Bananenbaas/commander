// SPDX-License-Identifier: GPL-2.0-or-later

package store

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/layout"
)

func fixedWriter(dir string) *writer {
	w := newWriter(dir)
	w.now = func() time.Time { return time.Date(2026, 11, 22, 22, 15, 30, 0, time.Local) }
	return w
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestPutReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "kb7-sc.toml")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := newWriter(dir).put("kb7-sc.toml", []byte("new")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "new" {
		t.Fatalf("target = %q", got)
	}
	for _, n := range names(t, dir) {
		if n != "kb7-sc.toml" {
			t.Errorf("unexpected file %q left behind", n)
		}
	}
}

func TestPutCreatesParentFolder(t *testing.T) {
	dir := t.TempDir()
	if err := newWriter(dir).put("games/sc.toml", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "games", "sc.toml")); string(got) != "x" {
		t.Fatalf("got %q", got)
	}
}

func TestPutCleansUpOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "kb7-sc.toml")
	mustWrite(t, target, []byte("old"))
	w := newWriter(dir)
	boom := errors.New("rename failed")
	w.rename = func(from, to string) error { return boom }
	if err := w.put("kb7-sc.toml", []byte("new")); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want rename error", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "old" {
		t.Fatalf("target = %q, want old bytes", got)
	}
	if n := names(t, dir); len(n) != 1 {
		t.Fatalf("files left: %v", n)
	}
}

func TestPutCleansUpOnWriteFailure(t *testing.T) {
	dir := t.TempDir()
	w := newWriter(dir)
	boom := errors.New("disk full")
	w.writeFile = func(path string, data []byte) error {
		if err := os.WriteFile(path, data[:1], 0o644); err != nil {
			return err
		}
		return boom
	}
	if err := w.put("a.toml", []byte("abc")); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if n := names(t, dir); len(n) != 0 {
		t.Fatalf("files left: %v", n)
	}
}

func TestBackupOnlyWhenLossy(t *testing.T) {
	dir := t.TempDir()
	w := fixedWriter(dir)

	hand := []byte("# my layout\nprofile = 0\n[[button]]\npage = 1\nposition = 1\nlabel = \"x\"\n")
	mustWrite(t, filepath.Join(dir, "kb7-sc.toml"), hand)
	got, err := w.backup("kb7-sc.toml", layoutLossless)
	if err != nil {
		t.Fatal(err)
	}
	want := "backup/kb7-sc-20261122-221530.toml"
	if got != want {
		t.Fatalf("backup = %q, want %q", got, want)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, filepath.FromSlash(want))); string(b) != string(hand) {
		t.Fatalf("backup content = %q", b)
	}

	got, err = w.backup("kb7-sc.toml", layoutLossless)
	if err != nil || got != "backup/kb7-sc-20261122-221530-2.toml" {
		t.Fatalf("second backup = %q, %v", got, err)
	}
	got, err = w.backup("kb7-sc.toml", layoutLossless)
	if err != nil || got != "backup/kb7-sc-20261122-221530-3.toml" {
		t.Fatalf("third backup = %q, %v", got, err)
	}

	// A file already in Commander's encoding needs no backup.
	f, err := layout.DecodeFile([]byte("profile = 0\n[[button]]\npage = 1\nposition = 1\nlabel = \"x\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "kb7-other.toml"), enc)
	if got, err := w.backup("kb7-other.toml", layoutLossless); err != nil || got != "" {
		t.Fatalf("lossless file: backup = %q, %v", got, err)
	}

	// A missing file needs none either.
	if got, err := w.backup("kb7-none.toml", layoutLossless); err != nil || got != "" {
		t.Fatalf("missing file: backup = %q, %v", got, err)
	}
}

func TestBackupOfGameFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "games"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "games", "sc.toml"), []byte("# hand written\nname = \"SC\"\n[devices]\nkb7 = 1\n"), 0o644)
	got, err := fixedWriter(dir).backup("games/sc.toml", gameLossless)
	if err != nil {
		t.Fatal(err)
	}
	if want := "backup/games-sc-20261122-221530.toml"; got != want {
		t.Fatalf("backup = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "backup", "games-sc-20261122-221530.toml")); err != nil {
		t.Fatal(err)
	}
}

func TestVersion(t *testing.T) {
	a, b := Version([]byte("one")), Version([]byte("two"))
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(a) {
		t.Fatalf("version %q is not 16 hex digits", a)
	}
	if a == b || a != Version([]byte("one")) {
		t.Fatalf("versions %q %q", a, b)
	}
}
