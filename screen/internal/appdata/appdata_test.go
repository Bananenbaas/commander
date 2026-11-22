// SPDX-License-Identifier: GPL-2.0-or-later

package appdata

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
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

func writeSettings(t *testing.T, roaming, content string) string {
	t.Helper()
	dir := filepath.Join(roaming, "Commander")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "settings.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheckUpdatesDefaultsOff(t *testing.T) {
	roaming, _ := setDirs(t)
	if on, err := LoadCheckUpdates(); err != nil || on {
		t.Fatalf("no file: LoadCheckUpdates = %v, %v; want false, nil", on, err)
	}
	writeSettings(t, roaming, `folder = 'D:\kb7'`+"\n")
	if on, err := LoadCheckUpdates(); err != nil || on {
		t.Fatalf("folder only: LoadCheckUpdates = %v, %v; want false, nil", on, err)
	}
}

func TestCheckUpdatesKeepsFolder(t *testing.T) {
	setDirs(t)
	if err := SaveFolder(`D:\kb7`); err != nil {
		t.Fatal(err)
	}
	if err := SaveCheckUpdates(true); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadFolder(); err != nil || got != `D:\kb7` {
		t.Fatalf("LoadFolder = %q, %v; want the folder kept", got, err)
	}
	if on, err := LoadCheckUpdates(); err != nil || !on {
		t.Fatalf("LoadCheckUpdates = %v, %v; want true", on, err)
	}
	if err := SaveFolder(`E:\other`); err != nil {
		t.Fatal(err)
	}
	if got, _ := LoadFolder(); got != `E:\other` {
		t.Fatalf("LoadFolder = %q after SaveFolder", got)
	}
	if on, _ := LoadCheckUpdates(); !on {
		t.Fatal("SaveFolder reset check_updates")
	}
	if err := SaveCheckUpdates(false); err != nil {
		t.Fatal(err)
	}
	if on, _ := LoadCheckUpdates(); on {
		t.Fatal("check_updates still on after SaveCheckUpdates(false)")
	}
	if got, _ := LoadFolder(); got != `E:\other` {
		t.Fatalf("LoadFolder = %q after turning updates off", got)
	}
}

func TestSaveCheckUpdatesWithoutFile(t *testing.T) {
	setDirs(t)
	if err := SaveCheckUpdates(true); err != nil {
		t.Fatal(err)
	}
	if on, err := LoadCheckUpdates(); err != nil || !on {
		t.Fatalf("LoadCheckUpdates = %v, %v; want true", on, err)
	}
	if got, err := LoadFolder(); err != nil || got != "" {
		t.Fatalf("LoadFolder = %q, %v; want empty", got, err)
	}
}

func TestSaveCheckUpdatesBadFile(t *testing.T) {
	roaming, _ := setDirs(t)
	path := writeSettings(t, roaming, "folder = [")
	if err := SaveCheckUpdates(true); err == nil {
		t.Fatal("no error for a broken settings file")
	}
	if err := SaveFolder(`D:\kb7`); err == nil {
		t.Fatal("SaveFolder overwrote a broken settings file")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "folder = [" {
		t.Fatalf("settings file = %q, %v; want unchanged", raw, err)
	}
}

func TestLogPath(t *testing.T) {
	_, local := setDirs(t)
	if got, want := LogPath(), filepath.Join(local, "Commander", "commander.log"); got != want {
		t.Fatalf("LogPath = %q, want %q", got, want)
	}
}

func TestWindowPaths(t *testing.T) {
	_, local := setDirs(t)
	if got, want := WindowLogPath(), filepath.Join(local, "Commander", "window.log"); got != want {
		t.Errorf("WindowLogPath = %q, want %q", got, want)
	}
	if got, want := WebView2Dir(), filepath.Join(local, "Commander", "WebView2"); got != want {
		t.Errorf("WebView2Dir = %q, want %q", got, want)
	}
}

func TestOpenWindowLog(t *testing.T) {
	setDirs(t)
	w, err := OpenWindowLog()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(w, "window line")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, WindowLogPath())
	if len(lines) != 1 || !strings.HasSuffix(lines[0], " window line") {
		t.Fatalf("window log = %q", lines)
	}
	checkStamped(t, lines)
	if _, err := os.Stat(LogPath()); !os.IsNotExist(err) {
		t.Errorf("the window log wrote to commander.log too: %v", err)
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

// fillLog writes full 1 KiB lines until the log is exactly 1 MiB.
func fillLog(t *testing.T, w io.Writer) {
	t.Helper()
	body := strings.Repeat("x", 1024-len(stampLayout)-2) + "\n"
	fi, err := os.Stat(LogPath())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size()%1024 != 0 {
		t.Fatalf("log size %d is not whole lines of 1 KiB", fi.Size())
	}
	for n := fi.Size(); n < 1<<20; n += 1024 {
		if _, err := fmt.Fprint(w, body); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLogRotationRenameFails(t *testing.T) {
	setDirs(t)
	clock := time.Date(2026, 11, 17, 12, 0, 0, 0, time.Local)
	old := now
	now = func() time.Time { return clock }
	t.Cleanup(func() { now = old })

	path := LogPath()
	// A non-empty folder named commander.log.1 cannot be replaced by a rename.
	block := filepath.Join(filepath.Dir(path), "commander.log.1")
	if err := os.MkdirAll(block, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(block, "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := OpenLog()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	fillLog(t, w)
	if _, err := fmt.Fprint(w, "rename failed\n"); err != nil {
		t.Fatalf("write after a failed rotation: %v", err)
	}
	clock = clock.Add(30 * time.Second)
	if err := os.RemoveAll(block); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprint(w, "no retry yet\n"); err != nil {
		t.Fatal(err)
	}
	lines := readLines(t, path)
	if n := len(lines); n != 1026 || !strings.HasSuffix(lines[n-2], "rename failed") || !strings.HasSuffix(lines[n-1], "no retry yet") {
		t.Fatalf("log holds %d lines, want all 1026 appended to the current file", n)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatalf("rotation retried within a minute: %v", err)
	}
	clock = clock.Add(31 * time.Second)
	if _, err := fmt.Fprint(w, "retried\n"); err != nil {
		t.Fatal(err)
	}
	if got := readLines(t, path+".1"); len(got) != 1026 {
		t.Fatalf(".1 holds %d lines, want 1026", len(got))
	}
	if got := readLines(t, path); len(got) != 1 || !strings.HasSuffix(got[0], "retried") {
		t.Fatalf("new log = %q", got)
	}
}

func TestLogReopensAfterFailedOpen(t *testing.T) {
	setDirs(t)
	w, err := OpenLog()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	l := w.(*logFile)
	// As after a rename whose reopen failed: no file open.
	l.mu.Lock()
	l.f.Close()
	l.f = nil
	l.mu.Unlock()
	if _, err := fmt.Fprint(w, "after reopen\n"); err != nil {
		t.Fatalf("write did not reopen the log: %v", err)
	}
	if got := readLines(t, LogPath()); len(got) != 1 || !strings.HasSuffix(got[0], "after reopen") {
		t.Fatalf("log = %q", got)
	}
	w.Close()
	if _, err := fmt.Fprint(w, "after close\n"); err == nil {
		t.Fatal("write after Close did not fail")
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

func TestThemeDefaultAndSave(t *testing.T) {
	setDirs(t)
	if got, err := LoadTheme(); err != nil || got != "system" {
		t.Fatalf("LoadTheme = %q, %v; want \"system\", nil", got, err)
	}
	if err := SaveFolder(`C:\cfg`); err != nil {
		t.Fatal(err)
	}
	if err := SaveCheckUpdates(true); err != nil {
		t.Fatal(err)
	}
	for _, th := range []string{"dark", "light", "system"} {
		if err := SaveTheme(th); err != nil {
			t.Fatalf("SaveTheme(%q): %v", th, err)
		}
		if got, err := LoadTheme(); err != nil || got != th {
			t.Fatalf("LoadTheme = %q, %v; want %q", got, err, th)
		}
	}
	if err := SaveTheme("dark"); err != nil {
		t.Fatal(err)
	}
	if dir, _ := LoadFolder(); dir != `C:\cfg` {
		t.Errorf("folder = %q, want it kept", dir)
	}
	if on, _ := LoadCheckUpdates(); !on {
		t.Error("check_updates was lost")
	}
	path, _ := settingsPath()
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), `theme = "dark"`) {
		t.Errorf("settings file lacks theme = \"dark\":\n%s", raw)
	}
}

func TestThemeRefusesUnknown(t *testing.T) {
	setDirs(t)
	err := SaveTheme("purple")
	if err == nil || !strings.Contains(err.Error(), "purple") {
		t.Fatalf("SaveTheme(purple) = %v, want an error naming the value", err)
	}
	if got, _ := LoadTheme(); got != "system" {
		t.Errorf("LoadTheme = %q after a refused save, want system", got)
	}
}
