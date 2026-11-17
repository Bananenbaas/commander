// SPDX-License-Identifier: GPL-2.0-or-later

// Command commander runs the loop of kb7screen run for one configuration
// folder with an icon in the Windows notification area, whose menu reloads
// the configuration, applies the buttons, switches starting with Windows and
// quits cleanly. It is built only for Windows, as a program without a console
// window:
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
	"github.com/Bananenbaas/kb7-commander/screen/internal/runlock"
	"github.com/Bananenbaas/kb7-commander/screen/internal/supervisor"
)

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
)

// errNoFolder means Commander was not told which folder to use.
var errNoFolder = errors.New("no configuration folder")

// folderFrom returns the configuration folder: the one argument, else the
// folder the settings remember (read with saved only then). fromArg tells
// which. Without either, or with more than one argument, the error is
// errNoFolder.
func folderFrom(args []string, saved func() (string, error)) (dir string, fromArg bool, err error) {
	switch {
	case len(args) > 1:
		return "", false, errNoFolder
	case len(args) == 1:
		if args[0] == "" {
			return "", false, errNoFolder
		}
		return args[0], true, nil
	}
	dir, err = saved()
	if err != nil {
		return "", false, err
	}
	if dir == "" {
		return "", false, errNoFolder
	}
	return dir, false, nil
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
		case st.Run.Waiting != "":
			what = "waiting for " + st.Run.Waiting
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
// of an Apply. A report that only carries the loop's new status shows
// nothing, nor does Stopped: Commander is ending.
func balloonsFor(prev, st supervisor.State) []balloon {
	if st.Phase == supervisor.Stopped {
		return nil
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
