// SPDX-License-Identifier: GPL-2.0-or-later

package request

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func held(v bool) func(string) (bool, error) {
	return func(string) (bool, error) { return v, nil }
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	for _, w := range []Word{Stop, Reload} {
		if err := Write(dir, w); err != nil {
			t.Fatal(err)
		}
		if n := names(t, dir); len(n) != 1 || n[0] != FileName {
			t.Fatalf("folder holds %v, want only %s", n, FileName)
		}
		got, ok, err := Take(dir)
		if err != nil || !ok || got != w {
			t.Fatalf("Take = %q, %v, %v; want %q", got, ok, err, w)
		}
		if n := names(t, dir); len(n) != 0 {
			t.Fatalf("folder holds %v after Take", n)
		}
	}
}

func TestTakeNothing(t *testing.T) {
	got, ok, err := Take(t.TempDir())
	if got != "" || ok || err != nil {
		t.Fatalf("Take = %q, %v, %v", got, ok, err)
	}
}

func TestTakeTrimsAndRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(" reload\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok, err := Take(dir); got != Reload || !ok || err != nil {
		t.Fatalf("Take = %q, %v, %v", got, ok, err)
	}
	if err := os.WriteFile(path, []byte("explode"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Take(dir)
	if got != "" || ok || err != nil {
		t.Fatalf("garbage: Take = %q, %v, %v", got, ok, err)
	}
	if n := names(t, dir); len(n) != 0 {
		t.Fatalf("garbage not removed: %v", n)
	}
}

func TestWriteOverExistingAndClear(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, Stop); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, Reload); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := Take(dir); got != Reload {
		t.Fatalf("got %q", got)
	}
	if err := Clear(dir); err != nil {
		t.Fatalf("Clear with no file: %v", err)
	}
	if err := Write(dir, Stop); err != nil {
		t.Fatal(err)
	}
	if err := Clear(dir); err != nil {
		t.Fatal(err)
	}
	if n := names(t, dir); len(n) != 0 {
		t.Fatalf("folder holds %v", n)
	}
}

func TestSendRefusesWhenNothingRuns(t *testing.T) {
	dir := t.TempDir()
	taken, err := Send(dir, Stop, time.Second, held(false))
	if err == nil || taken {
		t.Fatalf("Send = %v, %v", taken, err)
	}
	if n := names(t, dir); len(n) != 0 {
		t.Fatalf("wrote %v", n)
	}
}

func TestSendHeldError(t *testing.T) {
	boom := errors.New("boom")
	_, err := Send(t.TempDir(), Stop, time.Second, func(string) (bool, error) { return false, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestSendTaken(t *testing.T) {
	dir := t.TempDir()
	go func() {
		time.Sleep(300 * time.Millisecond)
		Take(dir)
	}()
	taken, err := Send(dir, Reload, 5*time.Second, held(true))
	if err != nil || !taken {
		t.Fatalf("Send = %v, %v", taken, err)
	}
}

func TestSendTimeoutWithdraws(t *testing.T) {
	dir := t.TempDir()
	taken, err := Send(dir, Stop, 200*time.Millisecond, held(true))
	if err != nil || taken {
		t.Fatalf("Send = %v, %v", taken, err)
	}
	if n := names(t, dir); len(n) != 0 {
		t.Fatalf("request left behind: %v", n)
	}
}

func TestWithdraw(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	// Gone already: the loop took it.
	if taken, err := withdraw(path, Stop); !taken || err != nil {
		t.Fatalf("missing file: %v, %v", taken, err)
	}
	// Still ours: removed, not taken.
	if err := Write(dir, Stop); err != nil {
		t.Fatal(err)
	}
	if taken, err := withdraw(path, Stop); taken || err != nil {
		t.Fatalf("own file: %v, %v", taken, err)
	}
	if n := names(t, dir); len(n) != 0 {
		t.Fatalf("left %v", n)
	}
	// Someone else's request stays.
	if err := Write(dir, Reload); err != nil {
		t.Fatal(err)
	}
	if taken, err := withdraw(path, Stop); taken || err != nil {
		t.Fatalf("other file: %v, %v", taken, err)
	}
	if n := names(t, dir); len(n) != 1 {
		t.Fatalf("other request removed: %v", n)
	}
}
