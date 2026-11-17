// SPDX-License-Identifier: GPL-2.0-or-later

// Command kb7screen puts your own buttons on the touchscreen of the Turtle
// Beach Command Series KB7 and carries out their actions on this computer.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
	"github.com/Bananenbaas/kb7-commander/screen/internal/apply"
	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/keyboard"
	"github.com/Bananenbaas/kb7-commander/screen/internal/keys"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/ledger"
	"github.com/Bananenbaas/kb7-commander/screen/internal/light"
	"github.com/Bananenbaas/kb7-commander/screen/internal/procs"
	"github.com/Bananenbaas/kb7-commander/screen/internal/run"
	"github.com/Bananenbaas/kb7-commander/screen/internal/runlock"
)

const ledgerName = ".kb7screen-state.json"

func main() {
	os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr))
}

// realMain runs the command line and returns the exit code: 0 on success, 1
// on an error.
func realMain(args []string, stdout, stderr io.Writer) int {
	c, err := parseArgs(args)
	if errors.Is(err, errHelp) {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	if err == nil {
		err = execute(c, stdout, stderr)
	}
	if err != nil {
		if errors.Is(err, keyboard.ErrUploadLeftOpen) {
			fmt.Fprintln(stderr, "")
			fmt.Fprintln(stderr, "!! The picture upload was interrupted. The keyboard may be waiting for the")
			fmt.Fprintln(stderr, "!! rest of the picture and will not work as a keyboard until you unplug it")
			fmt.Fprintln(stderr, "!! and plug it back in. Do that now.")
			fmt.Fprintln(stderr, "")
		}
		fmt.Fprintf(stderr, "kb7screen: %v\n", err)
		return 1
	}
	return 0
}

func execute(c command, stdout, stderr io.Writer) error {
	switch c.name {
	case "check":
		return doCheck(stdout)
	case "read":
		return doRead(c, stdout)
	case "preview":
		return doPreview(c, stdout)
	case "light-preview":
		return doLightPreview(c, stdout)
	case "apply":
		return doApply(c, stdout)
	case "restore":
		return doRestore(c, stdout)
	case "run":
		return doRun(c, stderr)
	case "keytest":
		return doKeytest(c, stdout)
	}
	return fmt.Errorf("unknown command %q", c.name)
}

// swarmRunning returns the names of the Swarm II programs that run.
func swarmRunning() ([]string, error) {
	m, err := procs.Running(procs.Swarm)
	if err != nil {
		return nil, fmt.Errorf("cannot tell whether Swarm II runs: %w", err)
	}
	var names []string
	for _, n := range procs.Swarm {
		if m[n] {
			names = append(names, n)
		}
	}
	return names, nil
}

// refuseWhileSwarm returns an error when Swarm II or its device service runs.
func refuseWhileSwarm(what string) error {
	names, err := swarmRunning()
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("%s is running; close it first, then %s again (it would overwrite the buttons and receive the taps)", strings.Join(names, " and "), what)
	}
	return nil
}

func openKeyboard() (*keyboard.KB, error) {
	dev, err := device.Open()
	if err != nil {
		return nil, err
	}
	return keyboard.New(dev), nil
}

func doCheck(stdout io.Writer) error {
	names, err := swarmRunning()
	if err != nil {
		return err
	}
	if len(names) > 0 {
		fmt.Fprintf(stdout, "Swarm II is running (%s).\n", strings.Join(names, ", "))
		return errors.New("close Swarm II before using the keyboard from kb7screen")
	}
	fmt.Fprintln(stdout, "Swarm II is not running.")
	kb, err := openKeyboard()
	if err != nil {
		return err
	}
	defer kb.Close()
	fmt.Fprintln(stdout, "Keyboard found.")
	profiles, err := kb.ProfileNames()
	if err != nil {
		return err
	}
	for i, n := range profiles {
		fmt.Fprintf(stdout, "profile %d: %s\n", i, n)
	}
	return nil
}

func doRead(c command, stdout io.Writer) error {
	if err := refuseWhileSwarm("read"); err != nil {
		return err
	}
	kb, err := openKeyboard()
	if err != nil {
		return err
	}
	defer kb.Close()
	p := byte(c.profile)
	t29, err := kb.ReadButtons(p)
	if err != nil {
		return err
	}
	t25, err := kb.ReadTypes(p)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "profile %d\n", c.profile)
	fmt.Fprintln(stdout, "pos  0x29: mod key  kind slot label        0x25: type value")
	for pos := 0; pos < 12; pos++ {
		e := t29.Entry(pos)
		t := t25.Type(pos)
		fmt.Fprintf(stdout, "%3d        %02x  %02x   %02x   %02x   %-12q       %02x   %02x\n",
			pos, e.Mod, e.Key, e.Kind, e.Slot, e.Label, t.Type, t.Value)
	}
	return nil
}

func doPreview(c command, stdout io.Writer) error {
	cfg, err := layout.Load(c.config)
	if err != nil {
		return err
	}
	out := c.out
	if out == "" {
		out = filepath.Join(cfg.Dir, "previews")
	}
	return writePreviews(cfg, out, stdout)
}

// loadLedger loads the ledger of a config folder and returns a function that
// saves it again.
func loadLedger(dir string) (*ledger.Ledger, func() error, error) {
	path := filepath.Join(dir, ledgerName)
	l, err := ledger.Load(path)
	if err != nil {
		return nil, nil, err
	}
	return l, func() error { return l.Save(path) }, nil
}

// errRunHoldsKeyboard is reported by apply and restore while run is active.
var errRunHoldsKeyboard = errors.New("Commander or kb7screen run is running; stop it first (its lighting stream and the picture uploads share the keyboard's interface 1)")

// refuseWhileRun returns an error when a run loop of this user, or run for
// dir, holds the keyboard.
func refuseWhileRun(dir string) error {
	held, err := runlock.UserHeld()
	if err != nil {
		return err
	}
	if held {
		return errRunHoldsKeyboard
	}
	held, err = runlock.Held(dir)
	if err != nil {
		return err
	}
	if held {
		return errRunHoldsKeyboard
	}
	return nil
}

func doApply(c command, stdout io.Writer) error {
	if err := refuseWhileSwarm("apply"); err != nil {
		return err
	}
	cfg, err := layout.Load(c.config)
	if err != nil {
		return err
	}
	if err := refuseWhileRun(cfg.Dir); err != nil {
		return err
	}
	l, save, err := loadLedger(cfg.Dir)
	if err != nil {
		return err
	}
	ctx, stop := stopOnSignal()
	defer stop()
	kb, err := openKeyboard()
	if err != nil {
		return err
	}
	defer kb.Close()

	running := func(name string) bool {
		m, err := procs.Running([]string{name})
		return err == nil && m[name]
	}
	rep, err := apply.Apply(ctx, kb, cfg, l, c.full, running, save)
	return finish(stdout, rep, err, save, errApplyStopped)
}

func doRestore(c command, stdout io.Writer) error {
	if err := refuseWhileSwarm("restore"); err != nil {
		return err
	}
	dir, err := filepath.Abs(c.config)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a folder", c.config)
	}
	if err := refuseWhileRun(dir); err != nil {
		return err
	}
	l, save, err := loadLedger(dir)
	if err != nil {
		return err
	}
	ctx, stop := stopOnSignal()
	defer stop()
	kb, err := openKeyboard()
	if err != nil {
		return err
	}
	defer kb.Close()

	rep, err := apply.Restore(ctx, kb, l, byte(c.profile), save)
	return finish(stdout, rep, err, save, errRestoreStopped)
}

// The errors reported when apply or restore was stopped and the ledger was
// saved.
var (
	errApplyStopped   = errors.New("stopped after the current picture; the ledger is saved")
	errRestoreStopped = errors.New("stopped before anything was changed; the ledger is saved")
)

// stopOnSignal returns a context that Ctrl+C or a request to terminate
// cancels; on Windows, closing the console window is such a request. Until
// stop is called these signals no longer end the program, so apply and
// restore can stop where nothing is left half done, and the ledger is saved.
func stopOnSignal() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// finish saves the ledger once more, whether or not the work failed, prints
// the report and returns the error to report: the work's, the save's, or both.
// Work stopped by a signal is reported as stopped once the ledger is saved.
func finish(stdout io.Writer, rep apply.Report, workErr error, save func() error, stopped error) error {
	saveErr := save()
	printReport(stdout, rep)
	switch {
	case workErr != nil && saveErr != nil:
		return errors.Join(workErr, fmt.Errorf("saving the ledger: %w", saveErr))
	case saveErr != nil:
		return fmt.Errorf("saving the ledger: %w", saveErr)
	case errors.Is(workErr, apply.ErrStopped):
		return stopped
	}
	return workErr
}

func printReport(w io.Writer, rep apply.Report) {
	list := func(title string, owners []ledger.Owner) {
		if len(owners) == 0 {
			return
		}
		fmt.Fprintf(w, "%s:\n", title)
		for _, o := range owners {
			fmt.Fprintf(w, "  profile %d page %d position %d\n", o.Profile, o.Page, o.Position)
		}
	}
	list("uploaded", rep.Uploaded)
	list("skipped (picture already on the keyboard)", rep.Skipped)
	list("restored", rep.Restored)
	for _, n := range rep.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
	if len(rep.Uploaded)+len(rep.Skipped)+len(rep.Restored)+len(rep.Notes) == 0 {
		fmt.Fprintln(w, "nothing to report")
	}
}

func doRun(c command, stderr io.Writer) error {
	cfg, err := layout.Load(c.config)
	if err != nil {
		return err
	}
	releaseUser, err := runlock.AcquireUser()
	if errors.Is(err, runlock.ErrUserHeld) {
		return errors.New("Commander or kb7screen run is already running for another folder")
	}
	if err != nil {
		return err
	}
	defer releaseUser()
	releaseLock, err := runlock.Acquire(cfg.Dir)
	if errors.Is(err, runlock.ErrHeld) {
		return errors.New("kb7screen run is already running for this folder")
	}
	if err != nil {
		return err
	}
	defer releaseLock()
	inj, err := action.NewInjector()
	if err != nil {
		return err
	}
	if cl, ok := inj.(io.Closer); ok {
		defer cl.Close()
	}
	exec := &action.Executor{In: inj, L: action.NewLauncher(), Sleep: time.Sleep}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	deps := run.Deps{
		Open: func() (run.Keyboard, error) {
			kb, err := openKeyboard()
			if err != nil {
				return nil, err
			}
			return kb, nil
		},
		Running: procs.Running,
		Exec:    exec,
		Logf: func(format string, args ...any) {
			fmt.Fprintf(stderr, "%s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
		},
	}
	stacks := map[byte]*light.Stack{}
	for _, p := range cfg.Profiles {
		if p.Light != nil {
			stacks[p.Number] = p.Light
		}
	}
	if len(stacks) > 0 {
		deps.Light = light.NewEngine(stacks, time.Now())
		keyCtx, endKeys := context.WithCancel(ctx)
		defer endKeys()
		ch, err := keys.Listen(keyCtx)
		if err != nil {
			deps.Logf("ripple and key toggles are off: %v", err)
		} else {
			deps.Keys = ch
		}
	}
	return run.Run(ctx, cfg, deps)
}

// doKeytest prints the keys pressed on the KB7 for the given time. Nothing is
// saved.
func doKeytest(c command, stdout io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.seconds)*time.Second)
	defer cancel()
	ch, err := keys.Listen(ctx)
	if errors.Is(err, keys.ErrUnsupported) {
		return errors.New("key input is not available on this system")
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Press keys on the KB7; listening for %d seconds.\n", c.seconds)
	for e := range ch {
		state := "up"
		if e.Down {
			state = "down"
		}
		fmt.Fprintf(stdout, "%s %s\n", action.KeyName(e.Key), state)
	}
	return nil
}
