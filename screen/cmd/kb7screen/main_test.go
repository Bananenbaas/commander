// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
	"github.com/Bananenbaas/kb7-commander/screen/internal/apply"
	"github.com/Bananenbaas/kb7-commander/screen/internal/light"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
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
		{[]string{"light-preview", "cfg", "--profile", "1"}, command{name: "light-preview", config: "cfg", profile: 1}},
		{[]string{"light-preview", "--out", "o.gif", "cfg", "--profile", "3"}, command{name: "light-preview", config: "cfg", profile: 3, out: "o.gif"}},
		{[]string{"apply", "cfg"}, command{name: "apply", config: "cfg"}},
		{[]string{"apply", "cfg", "--full"}, command{name: "apply", config: "cfg", full: true}},
		{[]string{"apply", "--full", "cfg"}, command{name: "apply", config: "cfg", full: true}},
		{[]string{"restore", "cfg", "--profile", "4"}, command{name: "restore", config: "cfg", profile: 4}},
		{[]string{"restore", "--profile", "1", "cfg"}, command{name: "restore", config: "cfg", profile: 1}},
		{[]string{"run", "cfg"}, command{name: "run", config: "cfg"}},
		{[]string{"keytest"}, command{name: "keytest", seconds: 20}},
		{[]string{"keytest", "--seconds", "1"}, command{name: "keytest", seconds: 1}},
		{[]string{"keytest", "--seconds=120"}, command{name: "keytest", seconds: 120}},
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
		{"light-preview", "cfg"},
		{"light-preview", "--profile", "1"},
		{"light-preview", "cfg", "--profile", "1", "--full"},
		{"light-preview", "a", "b", "--profile", "1"},
		{"apply"},
		{"apply", "cfg", "--out", "o"},
		{"restore", "cfg"},
		{"restore", "--profile", "1"},
		{"run", "cfg", "--full"},
		{"run", "cfg", "--nope"},
		{"keytest", "--seconds", "0"},
		{"keytest", "--seconds", "121"},
		{"keytest", "cfg"},
		{"check", "--seconds", "5"},
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

// A button that cannot be rendered stops preview before any file is written.
func TestPreviewRendersBeforeWriting(t *testing.T) {
	dir := testConfig(t)
	bad := "profile = 2\n\n[[button]]\npage = 1\nposition = 1\ntitle = \"A title far too long to fit on one button at any size\"\naction = [\"F6\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "z.toml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "previews")
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"preview", dir, "--out", out}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "z.toml: page 1 position 1") {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("output folder after the failure: %v, want none", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("files reported: %q", stdout.String())
	}
}

// lightPreviewLayout holds the example light block of the lighting design
// verbatim.
const lightPreviewLayout = `profile = 1

[light]
fps = 30                         # optional, 10–30

[[light.layer]]                  # bottom first
effect = "wave"
colors = ["#0040FF", "#00C0FF", "#8000FF"]
speed = 0.5                      # cycles per second
direction = "right"

[[light.layer]]
effect = "ripple"
color = "#FFFFFF"                # or colors = [...] to cycle per press
speed = 12                       # keys per second
width = 1.5                      # keys

[[light.layer]]
effect = "state"
name = "gear"
toggle = "N"                     # the key that flips it
start = "on"                     # state when the game starts
keys = ["N"]
color = "#30E070"

[[light.layer]]
effect = "state"
name = "quantum"
toggle = "B"
keys = ["B"]
color = "#00AAFF"
blink = 0                        # Hz; 0 is steady

[[light.layer]]
effect = "fixed"
keys = ["W", "A", "S", "D"]
color = "#FFFFFF"

[[button]]
page = 1
position = 1
state = "gear"
`

// A state without a toggle is flipped by the preview itself.
const lightFlipLayout = `profile = 1

[light]

[[light.layer]]
effect = "state"
name = "caps"
keys = ["N"]
color = "#30E070"

[[button]]
page = 1
position = 1
state = "caps"
`

func lightConfigWith(t *testing.T, layout string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sc.toml"), []byte(layout), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func lightConfig(t *testing.T) string {
	t.Helper()
	return lightConfigWith(t, lightPreviewLayout)
}

func TestLightPreview(t *testing.T) {
	dir := lightConfig(t)
	var stdout, stderr bytes.Buffer
	begin := time.Now()
	if code := realMain([]string{"light-preview", dir, "--profile", "1"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if d := time.Since(begin); d > 5*time.Second {
		t.Errorf("rendering took %v, want well under 5 s", d)
	}
	path := filepath.Join(dir, "previews", "light-1.gif")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 80 {
		t.Errorf("%d frames, want 80", len(g.Image))
	}
	if len(g.Delay) > 0 && g.Delay[0] != 5 {
		t.Errorf("delay %d, want 5", g.Delay[0])
	}
	// The fixed layer makes W white from the start; the effects move on.
	cv := newLightCanvas()
	led, _ := light.LEDOfKey(action.W)
	pt, _ := light.Pos(led)
	x, y := cv.pixel(pt)
	if r, gr, b, _ := g.Image[0].At(x, y).RGBA(); r>>8 < 0xF0 || gr>>8 < 0xF0 || b>>8 < 0xF0 {
		t.Errorf("W in frame 0 is %02x%02x%02x, want white", r>>8, gr>>8, b>>8)
	}
	if bytes.Equal(g.Image[0].Pix, g.Image[70].Pix) {
		t.Error("frame 0 and frame 70 are identical")
	}
	if !strings.Contains(stdout.String(), path) {
		t.Errorf("stdout %q lacks the path", stdout.String())
	}
}

func TestLightCanvasHoldsEveryLED(t *testing.T) {
	cv := newLightCanvas()
	n := 0
	for i := 0; i < light.NumLEDs; i++ {
		pt, ok := light.Pos(i)
		if !ok {
			continue
		}
		n++
		x, y := cv.pixel(pt)
		half := lightSquare / 2
		if x-half < 0 || y-half < 0 || x+half > cv.w || y+half > cv.h {
			t.Errorf("LED %d at %d,%d does not fit in %dx%d", i, x, y, cv.w, cv.h)
		}
	}
	if n < 90 {
		t.Errorf("only %d mapped LEDs", n)
	}
}

func TestLightPreviewFlipState(t *testing.T) {
	out := filepath.Join(t.TempDir(), "flip.gif")
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"light-preview", lightConfigWith(t, lightFlipLayout), "--profile", "1", "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Image) != 80 {
		t.Errorf("%d frames, want 80", len(g.Image))
	}
	if bytes.Equal(g.Image[0].Pix, g.Image[79].Pix) {
		t.Error("the flip changed nothing")
	}
}

func TestLedColourMatchesSetLED(t *testing.T) {
	var pl proto.LightPayload
	for _, i := range []int{0, 15, 16, 30, 47, 93} {
		proto.SetLED(&pl, i, byte(i+1), byte(i+2), byte(i+3))
		r, g, b := ledColour(&pl, i)
		if r != byte(i+1) || g != byte(i+2) || b != byte(i+3) {
			t.Errorf("LED %d: %d %d %d", i, r, g, b)
		}
	}
}

func TestLightPreviewOut(t *testing.T) {
	dir := lightConfig(t)
	out := filepath.Join(t.TempDir(), "x.gif")
	var stdout, stderr bytes.Buffer
	if code := realMain([]string{"light-preview", dir, "--profile", "1", "--out", out}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "previews")); err == nil {
		t.Error("default folder created although --out was given")
	}
}

func TestLightPreviewWithoutLight(t *testing.T) {
	dir := testConfig(t)
	var stdout, stderr bytes.Buffer
	code := realMain([]string{"light-preview", dir, "--profile", "1"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "profile 1") {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
	code = realMain([]string{"light-preview", lightConfig(t), "--profile", "4"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "profile 4") {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
}

// TestReadmeListsEveryEffect checks that the README has a subsection for each
// registered effect, headed by its name, which names every one of its settings.
func TestReadmeListsEveryEffect(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := string(data)
	for _, spec := range light.Specs() {
		if strings.HasPrefix(spec.Name, "test-") {
			continue
		}
		heading := "#### `" + spec.Name + "`"
		i := strings.Index(readme, heading)
		if i < 0 {
			t.Errorf("README has no subsection %q", heading)
			continue
		}
		section := readme[i+len(heading):]
		if j := strings.Index(section, "\n#"); j >= 0 {
			section = section[:j]
		}
		for _, st := range spec.Settings {
			if !strings.Contains(section, "`"+st.Name+"`") {
				t.Errorf("README section of %q does not list the setting %q", spec.Name, st.Name)
			}
		}
	}
}

func TestParseStopReload(t *testing.T) {
	for _, name := range []string{"stop", "reload"} {
		got, err := parseArgs([]string{name, "cfg"})
		if err != nil || got != (command{name: name, config: "cfg"}) {
			t.Errorf("%s: got %+v, %v", name, got, err)
		}
		for _, args := range [][]string{{name}, {name, "a", "b"}, {name, "cfg", "--full"}, {name, "cfg", "--profile", "1"}} {
			if c, err := parseArgs(args); err == nil {
				t.Errorf("%v: accepted as %+v", args, c)
			}
		}
	}
}

func TestStopWithNothingRunning(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if code := realMain([]string{"stop", dir}, &out, &errOut); code != 1 {
		t.Fatalf("exit code %d", code)
	}
	if !strings.Contains(errOut.String(), "no kb7screen run or Commander is running for") {
		t.Fatalf("stderr: %q", errOut.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".kb7screen-request")); err == nil {
		t.Fatal("request file written")
	}
	if code := realMain([]string{"reload", filepath.Join(dir, "missing")}, &out, &errOut); code != 1 {
		t.Fatalf("missing folder: exit code %d", code)
	}
}
