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
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/Bananenbaas/kb7-commander/screen/internal/appdata"
	"github.com/Bananenbaas/kb7-commander/screen/internal/autostart"
	"github.com/Bananenbaas/kb7-commander/screen/internal/icon"
	"github.com/Bananenbaas/kb7-commander/screen/internal/run"
	"github.com/Bananenbaas/kb7-commander/screen/internal/supervisor"
	"github.com/Bananenbaas/kb7-commander/screen/internal/tray"
)

// endSessionWait is the longest wait for the clean stop when the Windows
// session ends.
const endSessionWait = 5 * time.Second

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
	logf("Commander starts")

	dir, fromArg, err := folderFrom(args, appdata.LoadFolder)
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
	prev := supervisor.State{Phase: supervisor.Starting, Run: run.Status{Profile: -1}}
	sup, err := supervisor.New(dir, supervisor.Options{
		Logf: logf,
		OnState: func(st supervisor.State) {
			// SetTip, SetFailed and Balloon only post to the tray's thread.
			tr.SetTip(tipFor(st))
			tr.SetFailed(st.Phase == supervisor.Failed)
			for _, b := range balloonsFor(prev, st) {
				tr.Balloon(b.title, b.text)
			}
			prev = st
		},
	})
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
	if fromArg {
		if err := appdata.SaveFolder(dir); err != nil {
			logf("the folder was not remembered: %v", err)
		}
	}

	exe, err := os.Executable()
	if err != nil {
		logf("cannot find commander.exe itself, so Start with Windows is off: %v", err)
		exe = ""
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr, err = tray.Run(ctx, tray.Options{
		Icon:       normal,
		FailedIcon: failed,
		Tip:        tipFor(prev),
		Items:      menu(sup, dir, exe, logf),
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
	if logErr != nil {
		tr.Balloon(appTitle, "The log cannot be written: "+logErr.Error())
	}

	go func() {
		defer close(served)
		serveErr = sup.Serve(context.Background())
	}()
	select {
	case <-served:
	case <-tr.Done():
		// The session has really ended; OnEndSession asked for the stop.
		// Windows may end the process before it is done.
		sup.Stop()
		<-served
	}
	cancel()
	<-tr.Done()

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

// menu builds the tray menu.
func menu(sup *supervisor.Supervisor, dir, exe string, logf func(string, ...any)) []tray.Item {
	return []tray.Item{
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
		{Label: "Open configuration folder", OnClick: func() { openPath(dir, logf) }},
		{Label: "Open log", OnClick: func() { openPath(appdata.LogPath(), logf) }},
		{Separator: true},
		{Label: "Quit", OnClick: func() {
			logf("Quit chosen")
			sup.Stop()
		}},
	}
}

// toggleAutostart switches starting with Windows on or off.
func toggleAutostart(exe, dir string, logf func(string, ...any)) {
	if exe == "" {
		messageBox("Start with Windows cannot be switched on: Commander cannot find its own program file.", windows.MB_ICONWARNING)
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
