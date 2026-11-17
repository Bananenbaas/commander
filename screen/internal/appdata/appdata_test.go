// SPDX-License-Identifier: GPL-2.0-or-later

package appdata

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func setDirs(t *testing.T) (roaming, local string) {
	t.Helper()
	roaming, local = t.TempDir(), t.TempDir()
	t.Setenv("APPDATA", roaming)
	t.Setenv("LOCALAPPDATA", local)
	return roaming, local
}

func TestLoadFolderMissing(t *testing.T) {
	setDirs(t)
	dir, err := LoadFolder()
	if err != nil || dir != "" {
		t.Fatalf("LoadFolder = %q, %v; want \"\", nil", dir, err)
	}
}

func TestSaveLoadFolder(t *testing.T) {
	roaming, _ := setDirs(t)
	want := `D:\My "Config" folder\kb7`
	if err := SaveFolder(want); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(roaming, "Commander", "settings.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("settings file: %v", err)
	}
	if !bytes.Contains(raw, []byte("folder")) {
		t.Errorf("settings file has no key folder: %s", raw)
	}
	got, err := LoadFolder()
	if err != nil || got != want {
		t.Fatalf("LoadFolder = %q, %v; want %q", got, err, want)
	}
	if err := SaveFolder(`E:\other`); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadFolder(); got != `E:\other` {
		t.Fatalf("after second save LoadFolder = %q", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("settings folder holds %d entries, want only settings.toml", len(entries))
	}
}

func TestLoadFolderBadFile(t *testing.T) {
	roaming, _ := setDirs(t)
	dir := filepath.Join(roaming, "Commander")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.toml"), []byte("folder = ["), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFolder(); err == nil {
		t.Fatal("no error for a broken settings file")
	}
}

func TestLogPath(t *testing.T) {
	_, local := setDirs(t)
	if got, want := LogPath(), filepath.Join(local, "Commander", "commander.log"); got != want {
		t.Fatalf("LogPath = %q, want %q", got, want)
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 2<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

func checkStamped(t *testing.T, lines []string) {
	t.Helper()
	for i, l := range lines {
		if len(l) < len(stampLayout) {
			t.Fatalf("line %d too short: %q", i, l)
		}
		if _, err := time.ParseInLocation("2006-01-02 15:04:05", l[:19], time.Local); err != nil || l[19] != ' ' {
			t.Fatalf("line %d does not start with a timestamp: %q", i, l)
		}
	}
}

func TestLogTimestampsEveryLine(t *testing.T) {
	setDirs(t)
	w, err := OpenLog()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(w, "one\ntwo\n")
	fmt.Fprint(w, "three, ")
	fmt.Fprint(w, "still three\n")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, LogPath())
	if len(lines) != 3 {
		t.Fatalf("%d lines, want 3: %q", len(lines), lines)
	}
	checkStamped(t, lines)
	if got := lines[2][20:]; got != "three, still three" {
		t.Errorf("line 3 = %q", got)
	}
}

func TestLogAppends(t *testing.T) {
	setDirs(t)
	for _, s := range []string{"first\n", "second\n"} {
		w, err := OpenLog()
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprint(w, s)
		w.Close()
	}
	lines := readLines(t, LogPath())
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "first") || !strings.HasSuffix(lines[1], "second") {
		t.Fatalf("lines = %q", lines)
	}
}

func TestLogRotates(t *testing.T) {
	setDirs(t)
	w, err := OpenLog()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	stamp := len(stampLayout) + 1 // timestamp and a space
	// Lines of exactly 1 KiB including stamp and newline: 1024 of them fill
	// the file to exactly 1 MiB, which is still allowed.
	body := strings.Repeat("x", 1024-stamp-1) + "\n"
	for i := 0; i < 1024; i++ {
		if _, err := fmt.Fprint(w, body); err != nil {
			t.Fatal(err)
		}
	}
	path := LogPath()
	if fi, err := os.Stat(path); err != nil || fi.Size() != 1<<20 {
		t.Fatalf("log size %v, %v; want exactly 1 MiB", fi.Size(), err)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatalf("rotated before passing 1 MiB: %v", err)
	}
	// The next line would pass 1 MiB: rotate first.
	fmt.Fprint(w, "after first rotation\n")
	old := readLines(t, path+".1")
	if len(old) != 1024 {
		t.Fatalf(".1 holds %d lines, want 1024", len(old))
	}
	checkStamped(t, old)
	cur := readLines(t, path)
	if len(cur) != 1 || !strings.HasSuffix(cur[0], "after first rotation") {
		t.Fatalf("new log = %q", cur)
	}
	checkStamped(t, cur)

	// Fill again: with the short line already there, the 1024th full line no
	// longer fits, so the second rotation happens before it and replaces .1.
	for i := 0; i < 1024; i++ {
		fmt.Fprint(w, body)
	}
	old = readLines(t, path+".1")
	if len(old) != 1024 || !strings.HasSuffix(old[0], "after first rotation") {
		t.Fatalf(".1 not replaced: %d lines, first %.40q", len(old), old[0])
	}
	cur = readLines(t, path)
	if len(cur) != 1 || cur[0][20:] != body[:len(body)-1] {
		t.Fatalf("new log holds %d lines, want the one full line", len(cur))
	}
}

func TestLogConcurrent(t *testing.T) {
	setDirs(t)
	w, err := OpenLog()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				fmt.Fprintf(w, "goroutine %d line %d\n", g, i)
			}
		}()
	}
	wg.Wait()
	w.Close()
	lines := readLines(t, LogPath())
	if len(lines) != 800 {
		t.Fatalf("%d lines, want 800", len(lines))
	}
	checkStamped(t, lines)
}
