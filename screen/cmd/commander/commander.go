// SPDX-License-Identifier: GPL-2.0-or-later

// Command commander runs the loop of kb7screen run for one configuration
// folder with an icon in the Windows notification area, whose menu reloads
// the configuration, applies the buttons, switches starting with Windows,
// offers and installs updates from the project's releases and quits cleanly.
// It is built only for Windows, as a program without a console window:
//
//	go build -ldflags=-H=windowsgui ./cmd/commander
//
// This file holds the parts that do not need Windows.
package main

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"

	"github.com/Bananenbaas/kb7-commander/screen/internal/keyboard"
	"github.com/Bananenbaas/kb7-commander/screen/internal/run"
	"github.com/Bananenbaas/kb7-commander/screen/internal/runlock"
	"github.com/Bananenbaas/kb7-commander/screen/internal/supervisor"
)

// version is set at release builds with -ldflags "-X main.version=...".
var version = "dev"

const (
	// appTitle is the title of Commander's message boxes.
	appTitle = "Commander"
	// usageText says how to give Commander its folder.
	usageText = "Start Commander once with your configuration folder: commander.exe <folder>"
	// tipPrefix starts every tooltip.
	tipPrefix = "Commander — KB7: "
	// maxTip is the tooltip limit of the notification area, in UTF-16
	// units without the terminating zero.
	maxTip = 127
	// failedTitle is the balloon title when no loop runs.
	failedTitle = "KB7 stopped: choose Reload configuration once fixed"
	// unplugTitle is the balloon title after an interrupted upload.
	unplugTitle = "Unplug the keyboard and plug it back in."
	// replugTitle and replugText make the balloon shown when the keyboard
	// stops answering.
	replugTitle = "Keyboard not answering"
	replugText  = "Unplug the keyboard and plug it back in."
	// replugTip is the tooltip while the keyboard does not answer.
	replugTip = "the keyboard does not answer — unplug and replug it"
)

// errNoFolder means Commander was not told which folder to use.
var errNoFolder = errors.New("no configuration folder")

// errBadArgs means the arguments are not a folder with an optional
// --updated-from version: a second folder, an unknown flag or a flag without
// its value.
var errBadArgs = errors.New("unusable arguments")

// tooManyArgsText is the message box text for errBadArgs: most likely a
// folder path with spaces was given without quotes.
const tooManyArgsText = usageText + "\n\nPut a folder path that has spaces in quotes, for example:\n" +
	`commander.exe "D:\My KB7 config"`

// startArgs is what the command line says.
type startArgs struct {
	// dir is the configuration folder; fromArg tells whether it came from
	// the arguments rather than from the settings.
	dir     string
	fromArg bool
	// updatedFrom is the version this Commander replaced, set only by an
	// update's restart (--updated-from).
	updatedFrom string
}

// parseStart reads the arguments: an optional folder and an optional
// --updated-from <version>, in either order. Without a folder it uses the one
// the settings remember (read with saved only then). A second folder, an
// unknown flag or --updated-from without a value gives errBadArgs; no folder
// from either gives errNoFolder.
func parseStart(args []string, saved func() (string, error)) (startArgs, error) {
	var st startArgs
	folders := 0
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--updated-from":
			if i+1 >= len(args) || st.updatedFrom != "" {
				return startArgs{}, errBadArgs
			}
			i++
			st.updatedFrom = args[i]
			if st.updatedFrom == "" {
				return startArgs{}, errBadArgs
			}
		case strings.HasPrefix(a, "--"):
			return startArgs{}, errBadArgs
		default:
			folders++
			if folders > 1 {
				return startArgs{}, errBadArgs
			}
			if a == "" {
				return startArgs{}, errNoFolder
			}
			st.dir, st.fromArg = a, true
		}
	}
	if st.fromArg {
		return st, nil
	}
	dir, err := saved()
	if err != nil {
		return startArgs{}, err
	}
	if dir == "" {
		return startArgs{}, errNoFolder
	}
	st.dir = dir
	return st, nil
}

// heldMessage is the message box text when supervisor.New found the keyboard
// taken by another Commander or kb7screen run; ok is false for other errors.
func heldMessage(err error) (msg string, ok bool) {
	switch {
	case errors.Is(err, runlock.ErrHeld):
		return "Commander is already running.\n\nCommander or kb7screen run already runs for this configuration folder.", true
	case errors.Is(err, runlock.ErrUserHeld):
		return "Commander is already running.\n\nCommander or kb7screen run already runs for another configuration folder, and only one of them can use the keyboard.", true
	}
	return "", false
}

// tipFor is the tooltip for st, cut to the tooltip limit.
func tipFor(st supervisor.State) string {
	var what string
	switch st.Phase {
	case supervisor.Starting:
		what = "starting"
	case supervisor.Running:
		switch {
		case st.Run.Waiting == "keyboard":
			what = "waiting for the keyboard"
		case st.Run.Waiting == "Swarm II":
			what = "waiting for Swarm II to close"
		case st.Run.Waiting == run.WaitReplug:
			what = replugTip
		case st.Run.Waiting != "":
			what = "waiting for " + st.Run.Waiting
		case st.Run.Name != "":
			what = st.Run.Name
		case st.Run.Profile >= 0:
			what = fmt.Sprintf("profile %d", st.Run.Profile)
		default:
			what = "starting"
		}
	case supervisor.Applying:
		what = "applying buttons…"
	case supervisor.Failed:
		what = "error"
		if st.Err != "" {
			what += ": " + st.Err
		}
	case supervisor.Stopped:
		what = "stopped"
	default:
		what = st.Phase.String()
	}
	return cutTip(tipPrefix + what)
}

// cutTip cuts s to maxTip UTF-16 units, ending it with an ellipsis when it
// was cut. A character is never split.
func cutTip(s string) string {
	if len(utf16.Encode([]rune(s))) <= maxTip {
		return s
	}
	used := 0
	for i, r := range s {
		w := utf16.RuneLen(r)
		if w < 0 {
			w = 1 // invalid runes become U+FFFD, one unit
		}
		if used+w > maxTip-1 {
			return s[:i] + "…"
		}
		used += w
	}
	return s
}

// balloon is a notification next to the tray icon.
type balloon struct{ title, text string }

// balloonsFor returns the balloons to show for the change from prev to st:
// an error when no loop runs, a reload or Apply that went wrong, the outcome
// of an Apply, and the keyboard no longer answering, once each time the loop
// starts to report it. Any other report that only carries the loop's new
// status shows nothing, nor does Stopped: Commander is ending.
func balloonsFor(prev, st supervisor.State) []balloon {
	if st.Phase == supervisor.Stopped {
		return nil
	}
	// The loop is started anew, without an outcome to report, only by the
	// first start (from Starting), after an Apply (from Applying) and by a
	// reload that worked: from Running, or from Failed once fixed. Only
	// the supervisor reports a Run of {Profile: -1} without waiting.
	restarted := st.Phase == supervisor.Running && st.Run == (run.Status{Profile: -1}) && st.Err == "" && st.Note == ""
	if restarted && (prev.Phase == supervisor.Running || prev.Phase == supervisor.Failed) {
		return []balloon{{appTitle, "Configuration reloaded"}}
	}
	if st.Phase == supervisor.Running && st.Run.Waiting == run.WaitReplug && prev.Run.Waiting != run.WaitReplug {
		return []balloon{{replugTitle, replugText}}
	}
	statusOnly := st.Phase == prev.Phase && st.Err == prev.Err && st.Note == "" && st.Run != prev.Run
	if statusOnly {
		return nil
	}
	switch {
	case st.Err != "" && strings.Contains(st.Err, keyboard.ErrUploadLeftOpen.Error()):
		return []balloon{{unplugTitle, withNote(st.Err, st.Note)}}
	case st.Phase == supervisor.Failed:
		msg := st.Err
		if msg == "" {
			msg = "the loop is not running"
		}
		return []balloon{{failedTitle, msg}}
	case st.Err != "":
		return []balloon{{appTitle, withNote(st.Err, st.Note)}}
	case st.Note != "":
		return []balloon{{"Buttons applied", st.Note}}
	}
	return nil
}

// withNote adds the outcome of an Apply to an error, in brackets.
func withNote(msg, note string) string {
	if note == "" {
		return msg
	}
	return msg + " (" + note + ")"
}
