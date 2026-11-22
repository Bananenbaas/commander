// SPDX-License-Identifier: GPL-2.0-or-later

// Command kb7screen puts your own buttons on the touchscreen of the Turtle
// Beach Command Series KB7 and carries out their actions on this computer.
package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // background images
	_ "image/png"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/access"
	"github.com/Bananenbaas/commander/screen/internal/action"
	"github.com/Bananenbaas/commander/screen/internal/apply"
	"github.com/Bananenbaas/commander/screen/internal/keyboard"
	"github.com/Bananenbaas/commander/screen/internal/keys"
	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/ledger"
	"github.com/Bananenbaas/commander/screen/internal/picture"
	"github.com/Bananenbaas/commander/screen/internal/request"
	"github.com/Bananenbaas/commander/screen/internal/runlock"
	"github.com/Bananenbaas/commander/screen/internal/supervisor"
)

// version is set at release builds with -ldflags "-X main.version=...".
var version = "dev"

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
	if errors.Is(err, errVersion) {
		fmt.Fprintf(stdout, "kb7screen %s\n", version)
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
	case "audio":
		return doAudio(stdout, stderr)
	case "stop", "reload":
		return doRequest(c, stdout)
	case "background":
		return doBackground(c, stdout)
	case "clear-launchers":
		return doClearLaunchers(c, stdout)
	}
	return fmt.Errorf("unknown command %q", c.name)
}

func doCheck(stdout io.Writer) error {
	names, err := access.SwarmRunning()
	if err != nil {
		return err
	}
	if len(names) > 0 {
		fmt.Fprintf(stdout, "Swarm II is running (%s).\n", strings.Join(names, ", "))
		return errors.New("close Swarm II before using the keyboard from kb7screen")
	}
	fmt.Fprintln(stdout, "Swarm II is not running.")
	kb, err := access.OpenKeyboard()
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
	if err := access.RefuseWhileSwarm("read"); err != nil {
		return err
	}
	kb, err := access.OpenKeyboard()
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
	if err := access.RefuseWhileSwarm("apply"); err != nil {
		return err
	}
	cfg, err := layout.Load(c.config)
	if err != nil {
		return err
	}
	if err := refuseWhileRun(cfg.Dir); err != nil {
		return err
	}
	l, save, err := access.LoadLedger(cfg.Dir)
	if err != nil {
		return err
	}
	ctx, stop := stopOnSignal()
	defer stop()
	kb, err := access.OpenKeyboard()
	if err != nil {
		return err
	}
	defer kb.Close()

	rep, err := apply.Apply(ctx, kb, cfg, l, c.full, access.ProgramRunning, save)
	return finish(stdout, rep, err, save, errApplyStopped)
}

// doBackground uploads an image as the screen background. c.config holds
// the image's path.
func doBackground(c command, stdout io.Writer) error {
	if err := access.RefuseWhileSwarm("background"); err != nil {
		return err
	}
	if held, err := runlock.UserHeld(); err != nil {
		return err
	} else if held {
		return errRunHoldsKeyboard
	}
	f, err := os.Open(c.config)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("%s: %w", c.config, err)
	}
	full, thumb, err := picture.Background(img)
	if err != nil {
		return err
	}
	ctx, stop := stopOnSignal()
	defer stop()
	kb, err := access.OpenKeyboard()
	if err != nil {
		return err
	}
	defer kb.Close()
	return uploadBackground(ctx, stdout, func() error { return kb.UploadBackground(full, thumb) })
}

// The errors reported when background was stopped.
var (
	errBackgroundNotStarted = errors.New("stopped before the background upload started; nothing was sent")
	errBackgroundStopped    = errors.New("stopped once the background upload was done; the new background is on the keyboard")
)

// uploadBackground runs upload, which must never be cut short: an upload
// left open stops the keyboard working as a keyboard until it is replugged.
// ctx is cancelled by Ctrl+C or a terminate request, which stopOnSignal keeps
// from ending the program. A stop asked before the upload sends nothing; a
// stop asked during it is reported once the upload is done.
func uploadBackground(ctx context.Context, stdout io.Writer, upload func() error) error {
	if ctx.Err() != nil {
		return errBackgroundNotStarted
	}
	fmt.Fprintln(stdout, "Uploading the background; this takes about 30 seconds. Do not unplug the keyboard; it does not type until the upload is done.")
	if err := upload(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return errBackgroundStopped
	}
	fmt.Fprintln(stdout, "Done. Choose the new background in the keyboard's own background settings.")
	return nil
}

// doClearLaunchers empties the launcher buttons Swarm II left on a profile.
func doClearLaunchers(c command, stdout io.Writer) error {
	if err := access.RefuseWhileSwarm("clear-launchers"); err != nil {
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
	l, _, err := access.LoadLedger(dir)
	if err != nil {
		return err
	}
	ctx, stop := stopOnSignal()
	defer stop()
	kb, err := access.OpenKeyboard()
	if err != nil {
		return err
	}
	defer kb.Close()
	cleared, err := apply.ClearLaunchers(ctx, kb, l, byte(c.profile))
	if err != nil {
		return err
	}
	if len(cleared) == 0 {
		fmt.Fprintf(stdout, "Profile %d has no application, website or third-party application buttons.\n", c.profile)
		return nil
	}
	for _, w := range cleared {
		fmt.Fprintf(stdout, "emptied profile %d %s\n", c.profile, w)
	}
	return nil
}

func doRestore(c command, stdout io.Writer) error {
	if err := access.RefuseWhileSwarm("restore"); err != nil {
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
	l, save, err := access.LoadLedger(dir)
	if err != nil {
		return err
	}
	ctx, stop := stopOnSignal()
	defer stop()
	kb, err := access.OpenKeyboard()
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
	errApplyStopped   = access.ErrApplyStopped
	errRestoreStopped = errors.New("stopped before anything was changed; the ledger is saved")
)

// stopOnSignal returns a context that Ctrl+C or a request to terminate
// cancels; on Windows, closing the console window is such a request. Until
// stop is called these signals no longer end the program, so apply, restore
// and background can stop where nothing is left half done.
func stopOnSignal() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// finish saves the ledger once more, whether or not the work failed, prints
// the report and returns the error to report: the work's, the save's, or both.
// Work stopped by a signal is reported as stopped once the ledger is saved.
func finish(stdout io.Writer, rep apply.Report, workErr error, save func() error, stopped error) error {
	err := access.SaveAfter(workErr, save, stopped)
	printReport(stdout, rep)
	return err
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
	for _, p := range rep.Lighting {
		fmt.Fprintf(w, "stored lighting written: profile %d\n", p)
	}
	for _, r := range rep.Renamed {
		fmt.Fprintf(w, "profile %d renamed to %s\n", r.Profile, r.Name)
	}
	for _, n := range rep.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
	if len(rep.Uploaded)+len(rep.Skipped)+len(rep.Restored)+len(rep.Lighting)+len(rep.Renamed)+len(rep.Notes) == 0 {
		fmt.Fprintln(w, "nothing to report")
	}
}

// doRun runs the loop for the config folder until Ctrl+C or a stop request.
// A configuration that cannot be loaded, or a loop that ends with an error,
// ends it with that error.
func doRun(c command, stderr io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	var failed string
	s, err := supervisor.New(c.config, supervisor.Options{
		Logf: func(format string, args ...any) {
			fmt.Fprintf(stderr, "%s %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
		},
		OnState: func(st supervisor.State) {
			if st.Phase == supervisor.Failed {
				mu.Lock()
				failed = st.Err
				mu.Unlock()
				cancel()
			}
		},
	})
	switch {
	case errors.Is(err, runlock.ErrUserHeld):
		return errors.New("Commander or kb7screen run is already running for another folder")
	case errors.Is(err, runlock.ErrHeld):
		return errors.New("Commander or kb7screen run is already running for this folder")
	case err != nil:
		return err
	}
	if err := s.Serve(ctx); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if failed != "" {
		return errors.New(failed)
	}
	return nil
}

// doRequest asks the loop running for the config folder to stop or reload.
func doRequest(c command, stdout io.Writer) error {
	dir, err := filepath.Abs(c.config)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a folder", c.config)
	}
	w := request.Word(c.name)
	taken, err := request.Send(dir, w, 10*time.Second, runlock.Held)
	if err != nil {
		return err
	}
	switch {
	case !taken:
		fmt.Fprintf(stdout, "%s requested, but nothing took it within 10 s; the request was withdrawn\n", c.name)
	case w == request.Stop:
		fmt.Fprintln(stdout, "stop request taken by the running loop")
	default:
		fmt.Fprintln(stdout, "reload request taken by the running loop; see its output or log for the result")
	}
	return nil
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
