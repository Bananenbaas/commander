// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Bananenbaas/commander/screen/internal/keyboard"
	"github.com/Bananenbaas/commander/screen/internal/run"
	"github.com/Bananenbaas/commander/screen/internal/runlock"
	"github.com/Bananenbaas/commander/screen/internal/supervisor"
)

var none = run.Status{Profile: -1}

func TestTipFor(t *testing.T) {
	cases := []struct {
		st   supervisor.State
		want string
	}{
		{supervisor.State{Phase: supervisor.Starting, Run: none}, "Commander: starting"},
		{supervisor.State{Phase: supervisor.Running, Run: none}, "Commander: starting"},
		{supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 3}}, "Commander: profile 3"},
		{supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 0}}, "Commander: profile 0"},
		{supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 1, Name: "SC"}}, "Commander: SC"},
		{supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: -1, Waiting: "keyboard"}}, "Commander: waiting for the keyboard"},
		{supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: -1, Waiting: "Swarm II"}}, "Commander: waiting for Swarm II to close"},
		{supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: -1, Waiting: "lunch"}}, "Commander: waiting for lunch"},
		{supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: -1, Waiting: run.WaitReplug}}, "Commander: the keyboard does not answer, unplug and replug it"},
		// A reload that failed keeps the old configuration running; the
		// balloon says so, the tip keeps showing the loop.
		{supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 2}, Err: "not reloaded"}, "Commander: profile 2"},
		{supervisor.State{Phase: supervisor.Applying, Run: none}, "Commander: putting buttons on the KB7…"},
		{supervisor.State{Phase: supervisor.Failed, Run: none, Err: "layout.toml: line 3: bad key"}, "Commander: error: layout.toml: line 3: bad key"},
		{supervisor.State{Phase: supervisor.Failed, Run: none}, "Commander: error"},
		{supervisor.State{Phase: supervisor.Stopped, Run: none}, "Commander: stopped"},
	}
	for _, c := range cases {
		if got := tipFor(c.st); got != c.want {
			t.Errorf("tipFor(%+v) = %q, want %q", c.st, got, c.want)
		}
	}
}

func units(s string) int { return len(utf16.Encode([]rune(s))) }

func TestTipForLongError(t *testing.T) {
	long := strings.Repeat("the configuration cannot be loaded ", 10)
	got := tipFor(supervisor.State{Phase: supervisor.Failed, Run: none, Err: long})
	if n := units(got); n != 127 {
		t.Errorf("tip of %d UTF-16 units, want 127: %q", n, got)
	}
	if !strings.HasPrefix(got, "Commander: error: the configuration cannot be loaded") || !strings.HasSuffix(got, "…") {
		t.Errorf("tip %q not the start of the error with an ellipsis", got)
	}

	// A character outside the BMP takes two units and is never split.
	emoji := strings.Repeat("\U0001F600", 80)
	got = tipFor(supervisor.State{Phase: supervisor.Failed, Run: none, Err: emoji})
	if n := units(got); n > 127 {
		t.Errorf("tip of %d UTF-16 units, want at most 127", n)
	}
	if strings.ContainsRune(got, '\uFFFD') || !strings.HasSuffix(got, "\U0001F600…") {
		t.Errorf("tip split a character: %q", got)
	}

	// Exactly 127 units is kept whole.
	prefix := "Commander: error: "
	exact := strings.Repeat("x", 127-units(prefix))
	if got := tipFor(supervisor.State{Phase: supervisor.Failed, Run: none, Err: exact}); got != prefix+exact {
		t.Errorf("a tip of exactly 127 units was cut: %q", got)
	}
}

func TestBalloonsFor(t *testing.T) {
	running := supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 1}}
	failed := supervisor.State{Phase: supervisor.Failed, Run: none, Err: "bad layout"}
	notAnswering := supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: -1, Waiting: run.WaitReplug}}
	upload := fmt.Errorf("buttons not applied: %w (frame 7 of 1065: timeout)", keyboard.ErrUploadLeftOpen).Error()
	cases := []struct {
		name       string
		prev, next supervisor.State
		want       []balloon
	}{
		{"start", supervisor.State{Phase: supervisor.Starting, Run: none}, running, nil},
		{"profile change", running, supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 2}}, nil},
		{"fails at start", supervisor.State{Phase: supervisor.Starting, Run: none}, failed,
			[]balloon{{failedTitle, "bad layout"}}},
		{"reload fails again", failed, failed, []balloon{{failedTitle, "bad layout"}}},
		{"loop ends", running, supervisor.State{Phase: supervisor.Failed, Run: none, Err: "the loop ended unexpectedly"},
			[]balloon{{failedTitle, "the loop ended unexpectedly"}}},
		{"start without a status yet", supervisor.State{Phase: supervisor.Starting, Run: none},
			supervisor.State{Phase: supervisor.Running, Run: none}, nil},
		{"reload fixed", failed, supervisor.State{Phase: supervisor.Running, Run: none},
			[]balloon{{appTitle, "Configuration reloaded"}}},
		{"reloaded", running, supervisor.State{Phase: supervisor.Running, Run: none},
			[]balloon{{appTitle, "Configuration reloaded"}}},
		{"reloaded before the loop reported", supervisor.State{Phase: supervisor.Running, Run: none},
			supervisor.State{Phase: supervisor.Running, Run: none},
			[]balloon{{appTitle, "Configuration reloaded"}}},
		{"reloaded after a refused reload",
			supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 1}, Err: "not reloaded: x"},
			supervisor.State{Phase: supervisor.Running, Run: none},
			[]balloon{{appTitle, "Configuration reloaded"}}},
		{"waiting for the keyboard after a reload", supervisor.State{Phase: supervisor.Running, Run: none},
			supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: -1, Waiting: "keyboard"}}, nil},
		{"reload refused while running", running,
			supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 1}, Err: "not reloaded, the previous configuration stays: x"},
			[]balloon{{"Commander", "not reloaded, the previous configuration stays: x"}}},
		{"status while the reload error stands",
			supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 1}, Err: "not reloaded: x"},
			supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 2}, Err: "not reloaded: x"}, nil},
		{"same reload error again",
			supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 1}, Err: "not reloaded: x"},
			supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: 1}, Err: "not reloaded: x"},
			[]balloon{{"Commander", "not reloaded: x"}}},
		{"applying", running, supervisor.State{Phase: supervisor.Applying, Run: none},
			[]balloon{{"Applying buttons and lighting", "Keep the keyboard plugged in; it does not type while pictures upload."}}},
		{"still applying", supervisor.State{Phase: supervisor.Applying, Run: none}, supervisor.State{Phase: supervisor.Applying, Run: none}, nil},
		{"applied", supervisor.State{Phase: supervisor.Applying, Run: none},
			supervisor.State{Phase: supervisor.Running, Run: none, Note: "3 pictures uploaded, 9 unchanged"},
			[]balloon{{"Buttons applied", "3 pictures uploaded, 9 unchanged"}}},
		{"apply refused", supervisor.State{Phase: supervisor.Applying, Run: none},
			supervisor.State{Phase: supervisor.Running, Run: none, Err: "buttons not applied: Swarm II is running"},
			[]balloon{{"Commander", "buttons not applied: Swarm II is running"}}},
		{"apply failed half way", supervisor.State{Phase: supervisor.Applying, Run: none},
			supervisor.State{Phase: supervisor.Running, Run: none, Err: "buttons not applied: x", Note: "1 picture uploaded, 0 unchanged"},
			[]balloon{{"Commander", "buttons not applied: x (1 picture uploaded, 0 unchanged)"}}},
		{"upload left open", supervisor.State{Phase: supervisor.Applying, Run: none},
			supervisor.State{Phase: supervisor.Running, Run: none, Err: upload},
			[]balloon{{"Unplug the keyboard and plug it back in.", upload}}},
		{"stopped after apply", supervisor.State{Phase: supervisor.Applying, Run: none},
			supervisor.State{Phase: supervisor.Stopped, Run: none, Note: "done"}, nil},
		{"keyboard stops answering", running, notAnswering,
			[]balloon{{"Keyboard not answering", "Unplug the keyboard and plug it back in."}}},
		{"stops answering while waiting for it", supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: -1, Waiting: "keyboard"}}, notAnswering,
			[]balloon{{"Keyboard not answering", "Unplug the keyboard and plug it back in."}}},
		{"still not answering", notAnswering,
			supervisor.State{Phase: supervisor.Running, Run: notAnswering.Run, Err: "not reloaded, the previous configuration stays: x"},
			[]balloon{{"Commander", "not reloaded, the previous configuration stays: x"}}},
		{"answers again", notAnswering, running, nil},
		{"unplugged while not answering", notAnswering,
			supervisor.State{Phase: supervisor.Running, Run: run.Status{Profile: -1, Waiting: "keyboard"}}, nil},
	}
	for _, c := range cases {
		if got := balloonsFor(c.prev, c.next); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: balloonsFor = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseStart(t *testing.T) {
	saved := func(dir string, err error) func() (string, error) {
		return func() (string, error) { return dir, err }
	}
	broken := errors.New("broken settings")
	cases := []struct {
		args []string
		load func() (string, error)
		want startArgs
		err  error
	}{
		{[]string{`D:\cfg`}, saved(`E:\old`, nil), startArgs{dir: `D:\cfg`, fromArg: true}, nil},
		{[]string{`D:\cfg`}, saved("", broken), startArgs{dir: `D:\cfg`, fromArg: true}, nil},
		{nil, saved(`E:\old`, nil), startArgs{dir: `E:\old`}, nil},
		{nil, saved("", nil), startArgs{}, errNoFolder},
		{nil, saved("", broken), startArgs{}, broken},
		{[]string{`D:\My`, "config"}, saved(`E:\old`, nil), startArgs{}, errBadArgs},
		{[]string{""}, saved(`E:\old`, nil), startArgs{}, errNoFolder},
		{[]string{`D:\cfg`, "--updated-from", "v0.1.0-beta.1"}, saved("", broken), startArgs{dir: `D:\cfg`, fromArg: true, updatedFrom: "v0.1.0-beta.1"}, nil},
		{[]string{"--updated-from", "v0.1.0-beta.1", `D:\cfg`}, saved("", broken), startArgs{dir: `D:\cfg`, fromArg: true, updatedFrom: "v0.1.0-beta.1"}, nil},
		{[]string{"--updated-from", "v0.1.0"}, saved(`E:\old`, nil), startArgs{dir: `E:\old`, updatedFrom: "v0.1.0"}, nil},
		{[]string{"--updated-from"}, saved(`E:\old`, nil), startArgs{}, errBadArgs},
		{[]string{`D:\cfg`, "--updated-from"}, saved(`E:\old`, nil), startArgs{}, errBadArgs},
		{[]string{"--nope", "a"}, saved(`E:\old`, nil), startArgs{}, errBadArgs},
		{[]string{"--updated-from", "v1.0.0", "--updated-from", "v2.0.0"}, saved(`E:\old`, nil), startArgs{}, errBadArgs},
		// The window process needs no folder and never reads the settings.
		{[]string{"--window"}, saved("", broken), startArgs{window: true}, nil},
		{[]string{"--window", `D:\cfg`}, saved(`E:\old`, nil), startArgs{}, errBadArgs},
		{[]string{`D:\cfg`, "--window"}, saved(`E:\old`, nil), startArgs{}, errBadArgs},
		{[]string{"--window", "--updated-from", "v1.0.0"}, saved(`E:\old`, nil), startArgs{}, errBadArgs},
		{[]string{"--updated-from", "v1.0.0", "--window"}, saved(`E:\old`, nil), startArgs{}, errBadArgs},
		{[]string{"--window", "--window"}, saved(`E:\old`, nil), startArgs{}, errBadArgs},
	}
	for _, c := range cases {
		got, err := parseStart(c.args, c.load)
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("parseStart(%q) = %+v, %v; want %+v, %v", c.args, got, err, c.want, c.err)
		}
	}
}

func TestHeldMessage(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("already running for this folder: %w", runlock.ErrHeld),
		fmt.Errorf("already running for another folder: %w", runlock.ErrUserHeld),
	} {
		msg, ok := heldMessage(err)
		if !ok || !strings.HasPrefix(msg, "Commander is already running") {
			t.Errorf("heldMessage(%v) = %q, %v", err, msg, ok)
		}
	}
	if _, ok := heldMessage(errors.New(`D:\cfg is not a folder`)); ok {
		t.Error("heldMessage took an unrelated error")
	}
}

func TestTooManyArgsText(t *testing.T) {
	if !strings.HasPrefix(tooManyArgsText, usageText) || !strings.Contains(tooManyArgsText, "in quotes") {
		t.Errorf("tooManyArgsText = %q", tooManyArgsText)
	}
}
