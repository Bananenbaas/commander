// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/Bananenbaas/commander/screen/internal/appdata"
	"github.com/Bananenbaas/commander/screen/internal/autostart"
	"github.com/Bananenbaas/commander/screen/internal/icon"
	"github.com/Bananenbaas/commander/screen/internal/run"
	"github.com/Bananenbaas/commander/screen/internal/supervisor"
	"github.com/Bananenbaas/commander/screen/internal/tray"
)

// endSessionWait is the longest wait for the clean stop when the Windows
// session ends.
const endSessionWait = 5 * time.Second

// crashWait is the longest wait for the tray icon to go after a panic.
const crashWait = 2 * time.Second

func main() {
	os.Exit(commander(os.Args[1:]))
}

// commander runs Commander and returns the exit code: 0 after a clean stop,
// 1 when it could not start or not stop cleanly.
func commander(args []string) int {
	logw, logErr := appdata.OpenLog()
	out := io.Discard
	if logErr == nil {
		defer logw.Close()
		out = logw
	}
	// The log writer puts the date and time in front of every line.
	logf := log.New(out, "", 0).Printf
	logf("Commander %s starts", version)

	parsed, err := parseStart(args, appdata.LoadFolder)
	dir, fromArg := parsed.dir, parsed.fromArg
	if errors.Is(err, errBadArgs) {
		logf("unusable arguments: %q", args)
		messageBox(tooManyArgsText, windows.MB_ICONINFORMATION)
		return 1
	}
	if errors.Is(err, errNoFolder) {
		logf("no usable configuration folder in the arguments %q or the settings", args)
		messageBox(usageText, windows.MB_ICONINFORMATION)
		return 1
	}
	if err != nil {
		logf("cannot read the settings: %v", err)
		messageBox("Commander cannot read its settings:\n\n"+err.Error(), windows.MB_ICONWARNING)
		return 1
	}

	// The tray exists before Serve starts, and OnState is only called by
	// Serve. Its calls never overlap, so prev needs no lock.
	var tr *tray.Tray
	var cancel context.CancelFunc
	var crashOnce sync.Once
	// crash ends Commander after a panic: it logs it with the stack, takes
	// the tray icon away if it can and exits with 1. It is for panics on the
	// goroutine that runs Serve, where the process cannot go on; OnState
	// handles its own panics.
	crash := func(v any) {
		crashOnce.Do(func() {
			logf("Commander failed: %v\n%s", v, debug.Stack())
			if cancel != nil {
				cancel()
			}
			if tr != nil {
				select {
				case <-tr.Done():
				case <-time.After(crashWait):
				}
			}
			os.Exit(1)
		})
	}
	prev := supervisor.State{Phase: supervisor.Starting, Run: run.Status{Profile: -1}}
	opts := supervisor.Options{
		Logf: logf,
		OnState: func(st supervisor.State) {
			defer func() {
				if v := recover(); v != nil {
					// OnState can run on the loop goroutine, so ending the
					// process here would skip the clean stop. Drop the update.
					logf("tray state update dropped after a panic: %v\n%s", v, debug.Stack())
				}
			}()
			// SetTip, SetFailed and Balloon only post to the tray's thread.
			tr.SetTip(tipFor(st))
			tr.SetFailed(st.Phase == supervisor.Failed)
			for _, b := range balloonsFor(prev, st) {
				tr.Balloon(b.title, b.text)
			}
			prev = st
		},
	}
	// After an update the old Commander may still be giving back the locks.
	sup, err := newWithRetry(context.Background(), func() (*supervisor.Supervisor, error) {
		return supervisor.New(dir, opts)
	}, lockWait, parsed.updatedFrom != "")
	if err != nil {
		logf("not started: %v", err)
		if msg, ok := heldMessage(err); ok {
			messageBox(msg, windows.MB_ICONINFORMATION)
		} else {
			messageBox("Commander cannot start:\n\n"+err.Error(), windows.MB_ICONWARNING)
		}
		return 1
	}
	dir = sup.Dir()
	logf("configuration folder: %s", dir)

	exe, err := os.Executable()
	if err != nil {
		logf("cannot find commander.exe itself, so Start with Windows cannot be changed and updates cannot be installed: %v", err)
		exe = ""
	}
	if exe != "" {
		// Files left by an update. After a restart the old program may still
		// be ending, so its .old is left to the retry once the tray is up.
		for _, err := range cleanLeftovers(exe, parsed.updatedFrom != "") {
			logf("leftover of an update not removed: %v", err)
		}
	}
	if fromArg {
		if err := appdata.SaveFolder(dir); err != nil {
			logf("the folder was not remembered: %v", err)
		}
		// When Windows starts Commander, it is with the folder given last.
		if exe != "" {
			if on, err := autostart.Enabled(exe); err == nil && on {
				if err := autostart.Enable(exe, dir); err != nil {
					logf("Start with Windows still uses the previous folder: %v", err)
				}
			}
		}
	}

	normal, err := icon.ICO(icon.Sizes, false)
	var failed []byte
	if err == nil {
		failed, err = icon.ICO(icon.Sizes, true)
	}
	if err != nil {
		sup.Close()
		logf("cannot draw the icon: %v", err)
		messageBox("Commander cannot draw its icon:\n\n"+err.Error(), windows.MB_ICONWARNING)
		return 1
	}

	served := make(chan struct{}) // closed when Serve has returned
	var serveErr error
	var sessionEnd atomic.Bool
	var ctx context.Context
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	// updCtx ends checks and downloads once the loop has stopped.
	updCtx, updCancel := context.WithCancel(context.Background())
	defer updCancel()
	// trayUp is closed once tr is set; a menu click could come before.
	trayUp := make(chan struct{})
	upd := newUpdater(exe, dir, func(title, text string, onClick func()) {
		<-trayUp
		tr.BalloonAction(title, text, onClick)
	}, sup.Stop, logf)
	tr, err = tray.Run(ctx, tray.Options{
		Icon:       normal,
		FailedIcon: failed,
		Tip:        tipFor(prev),
		Items:      menu(sup, dir, exe, logf, updateItems(updCtx, upd, logf)),
		OnEndSession: func() {
			// The Windows session ends: stop cleanly, but not longer than
			// Windows waits. An upload in progress is not cut short; when
			// the session goes on after all, Commander has stopped.
			sessionEnd.Store(true)
			logf("the Windows session ends: stopping")
			sup.Stop()
			select {
			case <-served:
			case <-time.After(endSessionWait):
				logf("not stopped within %v", endSessionWait)
			}
		},
	})
	if err != nil {
		sup.Close()
		logf("cannot show the tray icon: %v", err)
		messageBox("Commander cannot show its icon in the notification area:\n\n"+err.Error(), windows.MB_ICONWARNING)
		return 1
	}
	close(trayUp)
	if logErr != nil {
		tr.Balloon(appTitle, "The log cannot be written: "+logErr.Error())
	}
	if parsed.updatedFrom != "" {
		logf("updated from %s to %s", parsed.updatedFrom, version)
		if exe != "" {
			go removeOld(updCtx, exe+".old", logf)
		}
		tr.Balloon(appTitle, "Commander updated to "+version)
	}
	go scheduleChecks(updCtx, upd, logf)

	go func() {
		// Serve has stopped the loop and given back the locks before a
		// panic gets here.
		defer func() {
			if v := recover(); v != nil {
				crash(v)
			}
		}()
		defer close(served)
		serveErr = sup.Serve(context.Background())
	}()
	select {
	case <-served:
	case <-tr.Done():
		// The tray's window has ended without ctx: the Windows session
		// ended (OnEndSession has asked for the stop, and Windows may end
		// the process before it is done), or the window was closed from
		// outside. Either way Commander stops.
		if !sessionEnd.Load() {
			logf("the tray window was closed: stopping")
		}
		sup.Stop()
		<-served
	}
	// Serve has given back the locks by now, so for a moment another
	// Commander or kb7screen run can take the keyboard while this icon is
	// still in the notification area. That is harmless: this one no longer
	// touches the keyboard and only removes its icon.
	updCancel()
	upd.waitSwap()
	cancel()
	<-tr.Done()

	// An installed update starts the new Commander now that the locks are
	// free and the icon is gone.
	if restart := upd.pendingRestart(); restart != nil {
		logf("starting the new Commander")
		if err := restart(); err != nil {
			logf("restart after the update: %v", err)
		}
	}

	if sessionEnd.Load() {
		logf("stopped for the end of the Windows session; if the session went on, start Commander again")
	}
	if serveErr != nil {
		logf("Commander ends: %v", serveErr)
		return 1
	}
	logf("Commander ends")
	return 0
}

// menu builds the tray menu; updates are the items for updates, put after
// Start with Windows.
func menu(sup *supervisor.Supervisor, dir, exe string, logf func(string, ...any), updates []tray.Item) []tray.Item {
	items := []tray.Item{
		{Label: "Reload configuration", OnClick: func() {
			logf("Reload configuration chosen")
			sup.Reload()
		}},
		{Label: "Apply buttons", OnClick: func() {
			logf("Apply buttons chosen")
			sup.Apply()
		}},
		{Separator: true},
		{
			Label: "Start with Windows",
			Checked: func() bool {
				on, err := autostart.Enabled(exe)
				if err != nil {
					logf("Start with Windows: %v", err)
				}
				return on
			},
			OnClick: func() { toggleAutostart(exe, dir, logf) },
		},
	}
	items = append(items, updates...)
	return append(items, []tray.Item{
		{Label: "Open configuration folder", OnClick: func() { openPath(dir, logf) }},
		{Label: "Open log", OnClick: func() { openPath(appdata.LogPath(), logf) }},
		{Separator: true},
		{Label: "Quit", OnClick: func() {
			logf("Quit chosen")
			sup.Stop()
		}},
	}...)
}

// toggleAutostart switches starting with Windows on or off.
func toggleAutostart(exe, dir string, logf func(string, ...any)) {
	if exe == "" {
		messageBox("Start with Windows cannot be changed: Commander cannot find its own program file.", windows.MB_ICONWARNING)
		return
	}
	on, err := autostart.Enabled(exe)
	if err == nil {
		if on {
			err = autostart.Disable()
		} else {
			err = autostart.Enable(exe, dir)
		}
	}
	if err != nil {
		logf("Start with Windows not changed: %v", err)
		messageBox("Start with Windows was not changed:\n\n"+err.Error(), windows.MB_ICONWARNING)
		return
	}
	if on {
		logf("Start with Windows switched off")
	} else {
		logf("Start with Windows switched on: %s", autostart.Command(exe, dir))
	}
}

// openPath opens a folder or file the way Explorer does; a failure is shown.
func openPath(path string, logf func(string, ...any)) {
	if err := shellOpen(path); err != nil {
		logf("cannot open %s: %v", path, err)
		messageBox("Cannot open "+path+":\n\n"+err.Error(), windows.MB_ICONWARNING)
	}
}

// shellOpen runs the shell's "open" verb on path. The shell may use COM for
// it, so the call is made from a thread of its own with COM set up.
func shellOpen(path string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err {
	case nil, syscall.Errno(windows.S_FALSE):
		defer windows.CoUninitialize()
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, nil, windows.SW_SHOWNORMAL)
}

// messageBox shows text in a message box titled Commander, with an OK
// button and the icon in style (MB_ICONINFORMATION or MB_ICONWARNING), and
// waits for it to be closed.
func messageBox(text string, style uint32) {
	t, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	title, _ := windows.UTF16PtrFromString(appTitle)
	windows.MessageBox(0, t, title, windows.MB_OK|style)
}
