// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestSwapIn(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "commander.exe")
	write(t, exe, "old")
	write(t, exe+".new", "new")
	if _, err := swapIn(exe); err != nil {
		t.Fatal(err)
	}
	if read(t, exe) != "new" || read(t, exe+".old") != "old" || exists(exe+".new") {
		t.Errorf("after swapIn: exe %q, .old %q, .new exists %v", read(t, exe), read(t, exe+".old"), exists(exe+".new"))
	}
}

func TestSwapInWithoutNew(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "commander.exe")
	write(t, exe, "old")
	if _, err := swapIn(exe); err == nil {
		t.Fatal("swapIn without .new succeeded")
	}
	if read(t, exe) != "old" || exists(exe+".old") {
		t.Errorf("exe %q, .old exists %v", read(t, exe), exists(exe+".old"))
	}
}

func TestSwapUndo(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "commander.exe")
	write(t, exe, "old")
	write(t, exe+".new", "new")
	undo, err := swapIn(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if read(t, exe) != "old" || read(t, exe+".new") != "new" || exists(exe+".old") {
		t.Errorf("after undo: exe %q, .new %q, .old exists %v", read(t, exe), read(t, exe+".new"), exists(exe+".old"))
	}
}

func TestCleanLeftovers(t *testing.T) {
	for _, c := range []struct{ old, new bool }{{true, true}, {true, false}, {false, true}, {false, false}} {
		exe := filepath.Join(t.TempDir(), "commander.exe")
		write(t, exe, "exe")
		if c.old {
			write(t, exe+".old", "o")
		}
		if c.new {
			write(t, exe+".new", "n")
		}
		if errs := cleanLeftovers(exe, false); len(errs) != 0 {
			t.Errorf("%+v: errors %v", c, errs)
		}
		if exists(exe+".old") || exists(exe+".new") || read(t, exe) != "exe" {
			t.Errorf("%+v: leftovers remain or exe changed", c)
		}
	}
}

func TestCheckWritable(t *testing.T) {
	dir := t.TempDir()
	if err := checkWritable(dir); err != nil {
		t.Errorf("temp dir: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("checkWritable left %d files", len(entries))
	}
	file := filepath.Join(dir, "file")
	write(t, file, "x")
	if err := checkWritable(file); err == nil {
		t.Error("a path that is a file was writable")
	}
}
