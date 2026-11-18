// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/runlock"
	"github.com/Bananenbaas/kb7-commander/screen/internal/supervisor"
	"github.com/Bananenbaas/kb7-commander/screen/internal/update"
)

const (
	// firstCheck is how long after the start the first automatic check
	// waits, so that it never comes right after the start's own balloons.
	firstCheck = time.Minute
	// checkEvery is the time between automatic checks.
	checkEvery = 24 * time.Hour
	// lockWait is how long a Commander started by an update waits for the
	// old one to give back the locks.
	lockWait = 10 * time.Second
	// lockRetry is the time between two tries to take the locks.
	lockRetry = 250 * time.Millisecond
)

// Texts of the update balloons.
const (
	updateTitle    = "Commander update"
	checkFailTitle = "Update check failed"
	installFailed  = "Update not installed"
	busyText       = "An update check or install is already running."
)

// errUpdateBusy means another check, confirmation or install runs.
var errUpdateBusy = errors.New(busyText)

// updater checks for and installs Commander updates, one operation at a
// time. Its functions are set by the caller; the Windows ones talk to GitHub,
// show message boxes and balloons and start processes.
type updater struct {
	// releases fetches the release list; download fetches a release's
	// executable into a file that must not exist yet, checked against its
	// checksum, and removes the file on failure.
	releases func(context.Context) ([]update.Release, error)
	download func(context.Context, update.Release, string) error
	// confirm asks the user; true means yes.
	confirm func(text string) bool
	// notify shows a balloon; a click on it runs onClick when that is set.
	notify func(title, text string, onClick func())
	logf   func(string, ...any)
	// start starts the program exe with args and does not wait for it.
	start func(exe string, args []string) error
	// stop asks the loop to stop cleanly (installOffer calls it).
	stop func()

	// current is the running version, exe the running program's real path
	// and dir the configuration folder the restarted Commander gets.
	current, exe, dir string

	mu      sync.Mutex
	busy    bool
	offer   update.Release
	hasOff  bool
	shown   map[string]bool // versions whose balloon was shown
	restart func() error    // set by installOffer after a good install
}

// begin takes the one-operation guard; false means another one runs.
func (u *updater) begin() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.busy {
		return false
	}
	u.busy = true
	return true
}

// end gives the guard back.
func (u *updater) end() {
	u.mu.Lock()
	u.busy = false
	u.mu.Unlock()
}

// offered returns the update found by the last check that found one.
func (u *updater) offered() (update.Release, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.offer, u.hasOff
}

// pendingRestart returns the restart of an installed update, or nil.
func (u *updater) pendingRestart() func() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.restart
}

// check looks for an update. A manual check always answers in a balloon;
// an automatic one only logs, except that a newly found version is offered
// in a balloon once per run.
func (u *updater) check(ctx context.Context, manual bool) {
	if !u.begin() {
		if manual {
			u.notify(updateTitle, busyText, nil)
		} else {
			u.logf("automatic update check skipped: another update operation runs")
		}
		return
	}
	defer u.end()

	if _, err := update.ParseVersion(u.current); err != nil {
		if manual {
			u.notify(updateTitle, "This is a development build of Commander ("+u.current+"); it does not look for updates.", nil)
		}
		return
	}
	rels, err := u.releases(ctx)
	if err != nil {
		u.logf("update check failed: %v", err)
		if manual {
			u.notify(checkFailTitle, err.Error(), nil)
		}
		return
	}
	rel, ok, err := update.Pick(u.current, rels)
	if err != nil {
		u.logf("update check failed: %v", err)
		if manual {
			u.notify(checkFailTitle, err.Error(), nil)
		}
		return
	}
	if !ok {
		u.logf("update check: Commander %s is up to date", u.current)
		if manual {
			u.notify(updateTitle, "Commander "+u.current+" is up to date.", nil)
		}
		return
	}
	u.logf("update check: Commander %s is available (running %s)", rel.Tag, u.current)
	u.mu.Lock()
	u.offer, u.hasOff = rel, true
	again := u.shown[rel.Tag]
	if u.shown == nil {
		u.shown = map[string]bool{}
	}
	u.shown[rel.Tag] = true
	u.mu.Unlock()
	if manual || !again {
		u.notify("Commander "+rel.Tag+" is available",
			"Click here, or choose Update to "+rel.Tag+" in the menu, to install it.",
			func() { u.installOffer(ctx) })
	}
}

// confirmText is the question asked before installing rel.
func (u *updater) confirmText(rel update.Release) string {
	return "Update Commander from " + u.current + " to " + rel.Tag + "?\n\n" +
		"Commander downloads the new version, checks it against its checksum, " +
		"puts it in place of this one and starts it again with the same configuration folder.\n\n" +
		"Release page:\n" + rel.Page
}

// install asks the user, downloads rel next to the running program as
// <exe>.new and swaps it in, keeping the old program as <exe>.old. A failure
// is notified and returned; the running Commander is left as it was. When
// the user declines, restart and err are both nil and the offer stays.
//
// After a good install the updater stays busy: Commander is about to end.
// restart is then called once the loop has stopped and the locks are given
// back. It starts the new program with the folder and --updated-from; if that
// fails, it puts the old program back and starts that with the folder only.
func (u *updater) install(ctx context.Context, rel update.Release) (restart func() error, err error) {
	if !u.begin() {
		u.notify(updateTitle, busyText, nil)
		return nil, errUpdateBusy
	}
	installed := false
	defer func() {
		if !installed {
			u.end()
		}
	}()
	fail := func(text string, err error) (func() error, error) {
		u.logf("update to %s not installed: %v", rel.Tag, err)
		u.notify(installFailed, text, nil)
		return nil, err
	}

	if !u.confirm(u.confirmText(rel)) {
		u.logf("update to %s declined", rel.Tag)
		return nil, nil
	}
	if u.exe == "" {
		err := errors.New("Commander cannot find its own program file")
		return fail(err.Error()+".", err)
	}
	if err := checkWritable(filepath.Dir(u.exe)); err != nil {
		return fail("Commander cannot update itself in this folder. "+
			"Move Commander to a folder of your own and start it from there. ("+err.Error()+")", err)
	}
	newFile := u.exe + ".new"
	if err := os.Remove(newFile); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fail("A file from an earlier update is in the way: "+err.Error(), err)
	}
	u.logf("downloading Commander %s", rel.Tag)
	if err := u.download(ctx, rel, newFile); err != nil {
		os.Remove(newFile)
		return fail(err.Error(), err)
	}
	undo, err := swapIn(u.exe)
	if err != nil {
		os.Remove(newFile)
		return fail(err.Error(), err)
	}
	installed = true
	u.mu.Lock()
	u.offer, u.hasOff = update.Release{}, false
	u.mu.Unlock()
	u.logf("Commander %s is in place; the old program is kept as %s", rel.Tag, u.exe+".old")
	return func() error { return u.restartWith(undo) }, nil
}

// restartWith starts the new program; if that fails it undoes the swap and
// starts the old one. The error says what went wrong, also when the old
// program could be started instead.
func (u *updater) restartWith(undo func() error) error {
	err := u.start(u.exe, []string{u.dir, "--updated-from", u.current})
	if err == nil {
		return nil
	}
	err = fmt.Errorf("starting the new Commander: %w", err)
	if uerr := undo(); uerr != nil {
		return errors.Join(err, uerr)
	}
	if serr := u.start(u.exe, []string{u.dir}); serr != nil {
		return errors.Join(err, fmt.Errorf("starting the old Commander again: %w", serr))
	}
	return fmt.Errorf("%w; the old Commander was put back and started", err)
}

// installOffer installs the offered update, if there is one, and on success
// remembers the restart and stops the loop. It is what the menu item and the
// offer's balloon run.
func (u *updater) installOffer(ctx context.Context) {
	rel, ok := u.offered()
	if !ok {
		return
	}
	restart, err := u.install(ctx, rel)
	if err != nil || restart == nil {
		return
	}
	u.mu.Lock()
	u.restart = restart
	u.mu.Unlock()
	u.logf("stopping to restart as Commander %s", rel.Tag)
	u.stop()
}

// newWithRetry calls newSup. When retry is set and it fails because another
// Commander or kb7screen run holds the locks, it tries again every lockRetry
// until wait has passed: after an update the old Commander may still be
// ending.
func newWithRetry(ctx context.Context, newSup func() (*supervisor.Supervisor, error), wait time.Duration, retry bool) (*supervisor.Supervisor, error) {
	deadline := time.Now().Add(wait)
	for {
		sup, err := newSup()
		if err == nil || !retry || !(errors.Is(err, runlock.ErrHeld) || errors.Is(err, runlock.ErrUserHeld)) {
			return sup, err
		}
		left := time.Until(deadline)
		if left <= 0 {
			return nil, err
		}
		t := time.NewTimer(min(lockRetry, left))
		select {
		case <-ctx.Done():
			t.Stop()
			return nil, err
		case <-t.C:
		}
	}
}
