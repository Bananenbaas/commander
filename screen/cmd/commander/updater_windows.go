// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package main

import (
	"context"
	"net/http"
	"os/exec"
	"time"

	"golang.org/x/sys/windows"

	"github.com/Bananenbaas/commander/screen/internal/appdata"
	"github.com/Bananenbaas/commander/screen/internal/tray"
	"github.com/Bananenbaas/commander/screen/internal/update"
)

// newUpdater returns the updater that talks to GitHub and shows its
// questions and balloons; notify must not be called before the tray exists.
func newUpdater(exe, dir string, notify func(title, text string, onClick func()), stop func(), logf func(string, ...any)) *updater {
	client := &update.Client{HTTP: &http.Client{}, UserAgent: "Commander/" + version}
	return &updater{
		releases: client.Releases,
		download: client.Download,
		confirm:  askYesNo,
		notify:   notify,
		logf:     logf,
		start:    startProgram,
		stop:     stop,
		current:  version,
		exe:      exe,
		dir:      dir,
	}
}

// startProgram starts exe with args and lets it run on its own. Commander is
// a program without a console, so no console window appears.
func startProgram(exe string, args []string) error {
	cmd := exec.Command(exe, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// idYes is what MessageBox returns for the Yes button.
const idYes = 6

// askYesNo asks text in a message box with Yes and No; true means Yes.
func askYesNo(text string) bool {
	t, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return false
	}
	title, _ := windows.UTF16PtrFromString(appTitle)
	id, _ := windows.MessageBox(0, t, title, windows.MB_YESNO|windows.MB_ICONQUESTION|windows.MB_SETFOREGROUND|windows.MB_TOPMOST)
	return id == idYes
}

// autoCheck checks for an update when the settings allow it.
func autoCheck(ctx context.Context, u *updater, logf func(string, ...any)) {
	on, err := appdata.LoadCheckUpdates()
	if err != nil {
		logf("automatic update check skipped: cannot read the settings: %v", err)
		return
	}
	if on {
		u.check(ctx, false)
	}
}

// scheduleChecks runs autoCheck firstCheck after it is called and then every
// checkEvery, until ctx ends.
func scheduleChecks(ctx context.Context, u *updater, logf func(string, ...any)) {
	first := time.NewTimer(firstCheck)
	defer first.Stop()
	select {
	case <-ctx.Done():
		return
	case <-first.C:
	}
	autoCheck(ctx, u, logf)
	tick := time.NewTicker(checkEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			autoCheck(ctx, u, logf)
		}
	}
}

// updateItems are the tray menu items for updates.
func updateItems(ctx context.Context, u *updater, logf func(string, ...any)) []tray.Item {
	return []tray.Item{
		{
			Label: "Check for updates",
			Checked: func() bool {
				on, err := appdata.LoadCheckUpdates()
				if err != nil {
					logf("Check for updates: %v", err)
				}
				return on
			},
			OnClick: func() { toggleCheckUpdates(ctx, u, logf) },
		},
		{Label: "Check now", OnClick: func() {
			logf("Check now chosen")
			u.check(ctx, true)
		}},
		{
			LabelFunc: func() string {
				rel, _ := u.offered()
				return "Update to " + rel.Tag
			},
			Visible: func() bool {
				_, ok := u.offered()
				return ok
			},
			OnClick: func() {
				logf("Update chosen")
				u.installOffer(ctx)
			},
		},
	}
}

// toggleCheckUpdates switches automatic checks on or off; switching them on
// checks at once.
func toggleCheckUpdates(ctx context.Context, u *updater, logf func(string, ...any)) {
	on, err := appdata.LoadCheckUpdates()
	if err != nil {
		logf("Check for updates not changed: %v", err)
	} else {
		err = setCheckUpdates(!on, func() { u.check(ctx, false) }, logf)
	}
	if err != nil {
		messageBox("Check for updates was not changed:\n\n"+err.Error(), windows.MB_ICONWARNING)
	}
}

// setCheckUpdates switches automatic checks on or off; switching them on
// from off runs checkNow. The menu and the window both use it; the menu's
// tick reads the settings each time.
func setCheckUpdates(on bool, checkNow func(), logf func(string, ...any)) error {
	was, err := appdata.LoadCheckUpdates()
	if err == nil {
		err = appdata.SaveCheckUpdates(on)
	}
	if err != nil {
		logf("Check for updates not changed: %v", err)
		return err
	}
	switch {
	case on && !was:
		logf("Check for updates switched on")
		checkNow()
	case !on && was:
		logf("Check for updates switched off")
	}
	return nil
}
