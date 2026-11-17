// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux

package runlock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUserLockPathUsesTheRuntimeDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	got, err := userLockPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "commander-kb7.lock"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestUserLockPathFallsBackToAPrivateCacheFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", home)
	got, err := userLockPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".cache", "commander", "commander-kb7.lock")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	fi, err := os.Stat(filepath.Dir(got))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode = %v, want 0700", fi.Mode().Perm())
	}
}
