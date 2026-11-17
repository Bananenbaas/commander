// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bananenbaas/kb7-commander/screen/internal/apply"
)

func TestParseArgs(t *testing.T) {
	ok := []struct {
		args []string
		want command
	}{
		{[]string{"check"}, command{name: "check"}},
		{[]string{"read", "--profile", "2"}, command{name: "read", profile: 2}},
		{[]string{"read", "-profile=0"}, command{name: "read"}},
		{[]string{"preview", "cfg"}, command{name: "preview", config: "cfg"}},
		{[]string{"preview", "cfg", "--out", "o"}, command{name: "preview", config: "cfg", out: "o"}},
		{[]string{"preview", "--out", "o", "cfg"}, command{name: "preview", config: "cfg", out: "o"}},
		{[]string{"apply", "cfg"}, command{name: "apply", config: "cfg"}},
		{[]string{"apply", "cfg", "--full"}, command{name: "apply", config: "cfg", full: true}},
		{[]string{"apply", "--full", "cfg"}, command{name: "apply", config: "cfg", full: true}},
		{[]string{"restore", "cfg", "--profile", "4"}, command{name: "restore", config: "cfg", profile: 4}},
		{[]string{"restore", "--profile", "1", "cfg"}, command{name: "restore", config: "cfg", profile: 1}},
		{[]string{"run", "cfg"}, command{name: "run", config: "cfg"}},
	}
	for _, c := range ok {
		got, err := parseArgs(c.args)
		if err != nil || got != c.want {
			t.Errorf("%v: got %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}

	bad := [][]string{
		nil,
		{"frobnicate"},
		{"check", "cfg"},
		{"check", "--full"},
		{"read"},
		{"read", "--profile", "5"},
		{"read", "--profile", "-1"},
		{"read", "--profile", "x"},
		{"read", "--profile", "1", "cfg"},
		{"preview"},
		{"preview", "a", "b"},
		{"preview", "cfg", "--profile", "1"},
		{"apply"},
		{"apply", "cfg", "--out", "o"},
		{"restore", "cfg"},
		{"restore", "--profile", "1"},
		{"run", "cfg", "--full"},
		{"run", "cfg", "--nope"},
	}
	for _, args := range bad {
		if c, err := parseArgs(args); err == nil {
			t.Errorf("%v: accepted as %+v", args, c)
		}
	}

	for _, args := range [][]string{{"-h"}, {"--help"}, {"help"}, {"apply", "--help"}, {"run", "-h"}} {
		if _, err := parseArgs(args); !errors.Is(err, errHelp) {
			t.Errorf("%v: err = %v, want help", args, err)
		}
	}
}

func TestHelpAndErrorsExitCodes(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := realMain([]string{"--help"}, &out, &errOut); code != 0 {
		t.Fatalf("help exit code %d", code)
	}
	if !strings.Contains(out.String(), "Usage:") || errOut.Len() != 0 {
		t.Fatalf("help output: %q, stderr %q", out.String(), errOut.String())
	}
	out.Reset()
	if code := realMain([]string{"bogus"}, &out, &errOut); code != 1 {
		t.Fatalf("bad command exit code %d", code)
	}
	if !strings.HasPrefix(errOut.String(), "kb7screen: ") {
		t.Fatalf("stderr: %q", errOut.String())
	}
}

func TestFinishReportsAStop(t *testing.T) {
	stoppedErr := fmt.Errorf("%w (%w)", apply.ErrStopped, context.Canceled)
	saved := func() error { return nil }
	disk := errors.New("disk full")
	full := func() error { return disk }
	var out bytes.Buffer

	if err := finish(&out, apply.Report{}, stoppedErr, saved, errApplyStopped); err != errApplyStopped {
		t.Fatalf("stopped and saved: %v, want %v", err, errApplyStopped)
	}
	if err := finish(&out, apply.Report{}, stoppedErr, saved, errRestoreStopped); err != errRestoreStopped {
		t.Fatalf("restore stopped and saved: %v, want %v", err, errRestoreStopped)
	}
	// Without a saved ledger the stop is not reported as a clean one.
	err := finish(&out, apply.Report{}, stoppedErr, full, errApplyStopped)
	if !errors.Is(err, apply.ErrStopped) || !errors.Is(err, disk) || strings.Contains(err.Error(), "the ledger is saved") {
		t.Fatalf("stopped, save failed: %v", err)
	}
	other := errors.New("keyboard gone")
	if err := finish(&out, apply.Report{}, other, saved, errApplyStopped); err != other {
		t.Fatalf("other error: %v", err)
	}
	if err := finish(&out, apply.Report{}, nil, saved, errApplyStopped); err != nil {
		t.Fatalf("success: %v", err)
	}
}

const testLayout = `
profile = 1

[[button]]
page = 1
position = 0
title = "Alpha"
action = ["F5"]

[[button]]
page = 1
position = 4
title = "Beta"
color = "#FFAA00"
action = ["LCtrl+C"]
`

func testConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "game.toml"), []byte(testLayout), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPreview(t *testing.T) {
	dir := testConfig(t)
	var out, errOut bytes.Buffer
	if code := realMain([]string{"preview", dir}, &out, &errOut); code != 0 {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	prev := filepath.Join(dir, "previews")
	entries, err := os.ReadDir(prev)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if got, want := strings.Join(names, ","), "1-1-0.png,1-1-4.png,panel-1.png"; got != want {
		t.Fatalf("files %s, want %s", got, want)
	}

	f, err := os.Open(filepath.Join(prev, "1-1-0.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 142 || b.Dy() != 148 {
		t.Fatalf("button picture is %v", b)
	}
	// The picture keeps its transparency: the corner outside the frame is clear.
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Fatalf("corner alpha %d, want 0", a)
	}

	pf, err := os.Open(filepath.Join(prev, "panel-1.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer pf.Close()
	panel, err := png.Decode(pf)
	if err != nil {
		t.Fatal(err)
	}
	if b := panel.Bounds(); b.Dx() != 3*142+4*9 || b.Dy() != 4*148+5*9 {
		t.Fatalf("panel is %v", b)
	}
	dark := func(x, y int) bool {
		r, g, b, a := panel.At(x, y).RGBA()
		return r>>8 == 10 && g>>8 == 10 && b>>8 == 14 && a>>8 == 255
	}
	// Position 1 is empty: the middle of its cell stays dark; the gap too.
	if !dark(9+142+9+70, 9+74) || !dark(2, 2) {
		t.Fatal("empty position or gap is not the dark background")
	}
	// Position 4 (second row, middle column) is drawn: its frame is not dark.
	x0, y0 := 9+142+9, 9+148+9
	found := false
	for y := y0; y < y0+148 && !found; y++ {
		for x := x0; x < x0+142; x++ {
			if !dark(x, y) {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("position 4 is not on the panel")
	}
}

func TestPreviewOutFolderAndErrors(t *testing.T) {
	dir := testConfig(t)
	out := filepath.Join(t.TempDir(), "elsewhere")
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"preview", dir, "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(out, "panel-1.png")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "previews")); err == nil {
		t.Fatal("default folder created although --out was given")
	}

	stderr.Reset()
	if code := realMain([]string{"preview", t.TempDir()}, &stdout, &stderr); code != 1 || !strings.HasPrefix(stderr.String(), "kb7screen: ") {
		t.Fatalf("empty folder: code %d, stderr %q", code, stderr.String())
	}
}
