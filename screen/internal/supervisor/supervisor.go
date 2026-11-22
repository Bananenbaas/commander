// SPDX-License-Identifier: GPL-2.0-or-later

// Package supervisor runs the loop of package run for one configuration
// folder so that it can be stopped, reloaded and set aside while the buttons
// are applied, and so that an error leaves it waiting for a reload instead of
// ending it.
//
// A Supervisor holds the per-user lock and the folder's run lock from New
// until Serve returns. Requests come from its methods (Reload, Apply, Stop)
// and from the request file in the folder, which Serve looks for at every
// poll; Serve carries them out one at a time, in order. The loop runs on its
// own goroutine with a context of its own, so that a stop or a reload ends it
// the way Ctrl+C ends kb7screen run: held keys released, stored lighting
// sent, keyboard closed.
//
// A loop started again in the same process (a reload, after an Apply) keeps
// the profile the previous one left the keyboard on, also one chosen on the
// keyboard by hand, unless a program of switch_when_running wants another
// (see run.Resume). After a disconnect, while the keyboard did not answer or
// after the loop ended by itself, nothing is kept.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/access"
	"github.com/Bananenbaas/commander/screen/internal/action"
	"github.com/Bananenbaas/commander/screen/internal/apply"
	"github.com/Bananenbaas/commander/screen/internal/audio"
	"github.com/Bananenbaas/commander/screen/internal/device"
	"github.com/Bananenbaas/commander/screen/internal/keys"
	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/light"
	"github.com/Bananenbaas/commander/screen/internal/procs"
	"github.com/Bananenbaas/commander/screen/internal/request"
	"github.com/Bananenbaas/commander/screen/internal/run"
	"github.com/Bananenbaas/commander/screen/internal/runlock"
)

// Phase is what the supervisor is doing.
type Phase int

// The phases.
const (
	// Starting: Serve has begun and loads the configuration.
	Starting Phase = iota
	// Running: the loop runs.
	Running
	// Applying: the loop is stopped while the buttons are applied.
	Applying
	// Failed: no loop runs, because the configuration could not be loaded
	// or the loop ended with an error. Reload tries again.
	Failed
	// Stopped: Serve is done.
	Stopped
)

func (p Phase) String() string {
	switch p {
	case Starting:
		return "starting"
	case Running:
		return "running"
	case Applying:
		return "applying"
	case Failed:
		return "failed"
	case Stopped:
		return "stopped"
	}
	return fmt.Sprintf("phase %d", int(p))
}

// State is reported to Options.OnState whenever it changes.
type State struct {
	Phase Phase
	// Run is the loop's last status; it means something only while
	// Running. Before the loop reports, and in the other phases, it is
	// {Profile: -1}.
	Run run.Status
	// Err is the last thing that went wrong, as one line for the user: the
	// load or loop error behind Failed, a reload that failed while the old
	// configuration keeps running, or an Apply that was refused or failed.
	// It is cleared by a successful start, reload or Apply.
	Err string
	// Note is the outcome of an Apply, such as "3 pictures uploaded, 9
	// unchanged". It is set only in the one State reported when that Apply
	// has finished.
	Note string
}

// Options configure a Supervisor. The zero value works: the real loop, load
// and apply, and a poll of 2 s.
type Options struct {
	// Logf reports what happens; nil discards it. It is called from more
	// than one goroutine, also concurrently.
	Logf func(format string, args ...any)
	// OnState is called with every change of the State, from Serve's
	// goroutine or the loop's; the calls never overlap and come in order.
	// It must return soon and must not wait for Serve to return: a user
	// interface should hand the State to its own thread without waiting.
	// Nil: no reports.
	OnState func(State)
	// Load loads the configuration folder (layout.Load when nil).
	Load func(dir string) (*layout.Config, error)
	// Loop runs the loop until ctx ends, reporting its status (when nil:
	// run.Run, with a keystroke injector and program launcher made once in
	// New, and the lighting engine and key reader made anew for every
	// start). resume is the profile the previous loop left the keyboard on,
	// nil when there is none (see run.Resume).
	Loop func(ctx context.Context, cfg *layout.Config, resume *run.Resume, status func(run.Status)) error
	// Apply applies the buttons of cfg and returns a one-line summary (when
	// nil: refuse while Swarm II runs, open the keyboard, run apply.Apply
	// with the folder's ledger and save it). It stops between two pictures
	// once ctx ends, never during one.
	Apply func(ctx context.Context, cfg *layout.Config) (string, error)
	// Poll is the time between two looks for a request file (2 s when zero
	// or less).
	Poll time.Duration
}

const defaultPoll = 2 * time.Second

// acquireUser takes the per-user lock; tests replace it.
var acquireUser = runlock.AcquireUser

// openAudio starts following the Windows audio; tests replace it.
var openAudio = func(logf func(string, ...any)) (run.Audio, io.Closer, error) {
	w, err := audio.New(logf)
	if err != nil {
		// Never wrap a nil *audio.Windows in the interface.
		return nil, nil, err
	}
	return w, w, nil
}

// noStatus is State.Run while no loop has reported.
var noStatus = run.Status{Profile: -1}

// op is a request queued for Serve.
type op int

const (
	opReload op = iota
	opApply
	opStop
)

// loopRun is a running loop.
type loopRun struct {
	cancel context.CancelFunc
	done   chan error // receives the loop's result
}

// applyResult is what an Apply on its goroutine gives back.
type applyResult struct {
	cfg     *layout.Config
	summary string
	err     error
}

// Supervisor runs the loop for one configuration folder. Create it with New
// and call Serve once.
type Supervisor struct {
	dir     string
	logf    func(string, ...any)
	onState func(State)
	load    func(string) (*layout.Config, error)
	loop    func(context.Context, *layout.Config, *run.Resume, func(run.Status)) error
	apply   func(context.Context, *layout.Config) (string, error)
	poll    time.Duration
	release func() error // gives back what New took
	// audio is the computer's sound for the volume mixer, made once in New
	// and kept across reloads and applies; nil: no mixer.
	audio run.Audio
	// trace keeps the keyboard's recent calls across loops and applies, for
	// the log when the keyboard stops answering.
	trace  *device.Trace
	served atomic.Bool

	qmu   sync.Mutex
	queue []op
	wake  chan struct{} // signalled when queue grows

	smu     sync.Mutex
	state   State
	lastRun run.Status // the last status of the loop that runs or ran last

	// resume is the profile the next loop is to keep, nil: none, and
	// resumeName its name. Used by Serve's goroutine only.
	resume     *run.Resume
	resumeName string

	// Used by Serve's goroutine only.
	cur       *loopRun         // nil while no loop runs
	applyDone chan applyResult // non-nil while an Apply runs
}

// New takes the per-user lock, then the run lock of dir, and removes a
// request file left in dir (by a crash: nothing could take it while the lock
// was free). A lock held by another process gives an error wrapping
// runlock.ErrUserHeld (a loop runs for another folder) or runlock.ErrHeld (a
// loop runs for dir). The locks are kept until Serve returns, or Close is
// called instead of Serve.
func New(dir string, o Options) (*Supervisor, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("%s is not a folder: %w", dir, err)
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", dir)
	}
	releaseUser, err := acquireUser()
	if errors.Is(err, runlock.ErrUserHeld) {
		if held, herr := runlock.Held(abs); herr == nil && held {
			return nil, fmt.Errorf("Commander or kb7screen run is already running for this folder: %w", runlock.ErrHeld)
		}
		return nil, fmt.Errorf("Commander or kb7screen run is already running for another folder: %w", err)
	}
	if err != nil {
		return nil, err
	}
	releaseDir, err := runlock.Acquire(abs)
	if err != nil {
		releaseUser()
		if errors.Is(err, runlock.ErrHeld) {
			return nil, fmt.Errorf("Commander or kb7screen run is already running for this folder: %w", err)
		}
		return nil, err
	}
	release := func() error { return errors.Join(releaseDir(), releaseUser()) }
	if err := request.Clear(abs); err != nil {
		release()
		return nil, fmt.Errorf("removing a stale request: %w", err)
	}

	s := &Supervisor{
		dir:     abs,
		logf:    o.Logf,
		onState: o.OnState,
		load:    o.Load,
		loop:    o.Loop,
		apply:   o.Apply,
		poll:    o.Poll,
		wake:    make(chan struct{}, 1),
		state:   State{Phase: Starting, Run: noStatus},
	}
	if s.logf == nil {
		s.logf = func(string, ...any) {}
	}
	s.trace = device.NewTrace(s.logf)
	if s.load == nil {
		s.load = layout.Load
	}
	if s.apply == nil {
		s.apply = s.applyButtons
	}
	if s.poll <= 0 {
		s.poll = defaultPoll
	}
	if s.loop == nil {
		inj, err := action.NewInjector()
		if err != nil {
			release()
			return nil, err
		}
		if cl, ok := inj.(io.Closer); ok {
			locks := release
			release = func() error { return errors.Join(cl.Close(), locks()) }
		}
		if a, cl, err := openAudio(s.logf); err != nil {
			if !errors.Is(err, audio.ErrUnsupported) { // off Windows there is no mixer to miss
				s.logf("the volume mixer is off: %v", err)
			}
		} else {
			s.audio = a
			locks := release
			release = func() error { return errors.Join(cl.Close(), locks()) }
		}
		s.loop = s.runLoop(&action.Executor{In: inj, L: action.NewLauncher(), Sleep: time.Sleep})
	}
	var once sync.Once
	var releaseErr error
	s.release = func() error {
		once.Do(func() { releaseErr = release() })
		return releaseErr
	}
	return s, nil
}

// Close gives back the locks and what else New took, when Serve was never
// called; Serve may not be called after it. While or after Serve runs it does
// nothing: Serve gives them back when it returns.
func (s *Supervisor) Close() error {
	if !s.served.CompareAndSwap(false, true) {
		return nil
	}
	return s.release()
}

// Dir returns the configuration folder, absolute.
func (s *Supervisor) Dir() string { return s.dir }

// Reload asks Serve to load the configuration again and, when that works,
// to restart the loop with it. When it fails, a running loop keeps running.
func (s *Supervisor) Reload() { s.push(opReload) }

// Apply asks Serve to load the configuration, stop the loop, apply the
// buttons and start the loop again with that configuration, also when the
// apply was refused or failed; not when the next request is a stop. Requests
// made meanwhile wait for it.
func (s *Supervisor) Apply() { s.push(opApply) }

// Stop asks Serve to stop the loop and return.
func (s *Supervisor) Stop() { s.push(opStop) }

func (s *Supervisor) push(o op) {
	s.qmu.Lock()
	s.queue = append(s.queue, o)
	s.qmu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// stopNext reports whether the next queued request is a stop.
func (s *Supervisor) stopNext() bool {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	return len(s.queue) > 0 && s.queue[0] == opStop
}

func (s *Supervisor) pop() (op, bool) {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	if len(s.queue) == 0 {
		return 0, false
	}
	o := s.queue[0]
	s.queue = s.queue[1:]
	return o, true
}

// update changes the state with fn and reports it when fn returns true.
func (s *Supervisor) update(fn func(*State) bool) {
	s.smu.Lock()
	defer s.smu.Unlock()
	if !fn(&s.state) {
		return
	}
	if s.onState != nil {
		s.onState(s.state)
	}
	s.state.Note = ""
}

// set replaces the state and reports it.
func (s *Supervisor) set(st State) {
	s.update(func(p *State) bool { *p = st; return true })
}

// status takes a status report of the loop.
func (s *Supervisor) status(rs run.Status) {
	s.update(func(st *State) bool {
		s.lastRun = rs
		if st.Phase != Running || st.Run == rs {
			return false
		}
		st.Run = rs
		return true
	})
}

// Serve loads the configuration, starts the loop and carries out the
// requests until ctx ends, Stop is called or a stop request file arrives.
// A running Apply is waited for in every case; once ctx ends, it stops after
// the current picture. Serve then stops the loop, reports Stopped, gives the
// locks back and returns nil, or the error of giving them back. The loop is
// stopped and the locks given back also when Serve panics. It may be called
// once, and not after Close.
func (s *Supervisor) Serve(ctx context.Context) (err error) {
	if !s.served.CompareAndSwap(false, true) {
		return errors.New("supervisor: Serve called twice or after Close")
	}
	defer func() {
		if rerr := s.release(); rerr != nil && err == nil {
			err = fmt.Errorf("giving back the locks: %w", rerr)
		}
	}()
	defer func() {
		s.stopLoop()
		s.update(func(st *State) bool {
			st.Phase, st.Run = Stopped, noStatus
			return true
		})
	}()
	s.set(State{Phase: Starting, Run: noStatus})
	s.reload(ctx)
	tick := time.NewTicker(s.poll)
	defer tick.Stop()
	for !s.runQueue(ctx) {
		var loopDone <-chan error
		if s.cur != nil {
			loopDone = s.cur.done
		}
		ctxDone := ctx.Done()
		if s.applyDone != nil {
			ctxDone = nil // the Apply sees ctx itself; wait for it
		}
		select {
		case <-ctxDone:
		case <-s.wake:
		case <-tick.C:
			s.takeRequest()
		case err := <-loopDone:
			s.loopEnded(ctx, err)
		case r := <-s.applyDone:
			s.applyEnded(ctx, r)
		}
	}
	return nil
}

// runQueue carries out the queued requests until the queue is empty or an
// Apply has begun. It returns true when Serve must stop: ctx has ended (and
// no Apply runs) or a stop was requested.
func (s *Supervisor) runQueue(ctx context.Context) bool {
	for s.applyDone == nil {
		if ctx.Err() != nil {
			return true
		}
		o, ok := s.pop()
		if !ok {
			return false
		}
		switch o {
		case opStop:
			s.logf("stopping")
			return true
		case opReload:
			s.reload(ctx)
		case opApply:
			s.startApply(ctx)
		}
	}
	return false
}

// takeRequest queues the request in the request file, if there is one.
func (s *Supervisor) takeRequest() {
	w, ok, err := request.Take(s.dir)
	if err != nil {
		s.logf("cannot read the request file: %v", err)
		return
	}
	if !ok {
		return
	}
	s.logf("%s requested", w)
	switch w {
	case request.Stop:
		s.push(opStop)
	case request.Reload:
		s.push(opReload)
	}
}

// reload loads the configuration and, when that works, restarts the loop
// with it. When it fails, a running loop keeps running; otherwise the state
// becomes Failed.
func (s *Supervisor) reload(ctx context.Context) {
	cfg, err := s.load(s.dir)
	if err != nil {
		msg := oneLine(err)
		if s.cur != nil {
			s.logf("the configuration was not reloaded, the previous one stays: %s", msg)
			s.update(func(st *State) bool {
				st.Err = "not reloaded, the previous configuration stays: " + msg
				return true
			})
			return
		}
		s.logf("the configuration cannot be loaded: %s", msg)
		s.set(State{Phase: Failed, Run: noStatus, Err: msg})
		return
	}
	if s.cur != nil {
		s.logf("reloading the configuration")
	}
	s.stopLoop()
	s.start(ctx, cfg, "", "")
}

// start starts the loop with cfg and reports Running with errMsg and note.
func (s *Supervisor) start(ctx context.Context, cfg *layout.Config, errMsg, note string) {
	// Running is reported first, so the loop's status reports are taken,
	// and so a panic in OnState leaves no loop behind.
	s.set(State{Phase: Running, Run: noStatus, Err: errMsg, Note: note})
	s.update(func(*State) bool { s.lastRun = noStatus; return false })
	resume := s.resume
	s.resume = nil
	if resume != nil {
		s.logf("keeping %s", profileName(resume.Profile, s.resumeName))
	}
	lctx, cancel := context.WithCancel(ctx)
	cur := &loopRun{cancel: cancel, done: make(chan error, 1)}
	s.cur = cur
	go func() { cur.done <- s.loop(lctx, cfg, resume, s.status) }()
}

// stopLoop stops the loop, if one runs, and waits for it to end. When the
// loop's last status showed the keyboard in use on a profile, the next loop
// resumes that profile, taken to be active (stopping a loop closes the
// keyboard without changing its profile) unless the keyboard did not
// confirm the switch to it. Otherwise (the keyboard was not
// connected, did not answer or was left to Swarm II) there is nothing to
// resume.
func (s *Supervisor) stopLoop() {
	if s.cur == nil {
		return
	}
	s.cur.cancel()
	if err := <-s.cur.done; err != nil {
		s.logf("the loop ended with an error: %v", err)
	}
	s.cur = nil
	s.resume = nil
	s.update(func(*State) bool {
		if r := s.lastRun; r.Profile >= 0 && r.Waiting == "" {
			s.resume = &run.Resume{Profile: byte(r.Profile), Active: !r.Unconfirmed}
			s.resumeName = r.Name
		}
		return false
	})
}

// profileName is profile p for the log: "profile 1 (SC)", or "profile 1"
// without a name.
func profileName(p byte, name string) string {
	if name != "" {
		return fmt.Sprintf("profile %d (%s)", p, name)
	}
	return fmt.Sprintf("profile %d", p)
}

// loopEnded takes the result of a loop that ended without being stopped.
func (s *Supervisor) loopEnded(ctx context.Context, err error) {
	s.cur.cancel()
	s.cur = nil
	s.resume = nil // in doubt: the loop did not end by a stop
	if ctx.Err() != nil {
		// Serve is stopping.
		if err != nil {
			s.logf("the loop ended with an error: %v", err)
		}
		return
	}
	msg := "the loop ended unexpectedly"
	if err != nil {
		msg = oneLine(err)
	}
	s.logf("the loop stopped: %s", msg)
	s.set(State{Phase: Failed, Run: noStatus, Err: msg})
}

// startApply loads the configuration, stops the loop and starts applying
// the buttons on a goroutine of its own. When the configuration cannot be
// loaded nothing is stopped.
func (s *Supervisor) startApply(ctx context.Context) {
	cfg, err := s.load(s.dir)
	if err != nil {
		msg := "buttons not applied: " + oneLine(err)
		s.logf("%s", msg)
		s.update(func(st *State) bool { st.Err = msg; return true })
		return
	}
	s.stopLoop()
	if s.resume != nil {
		// Apply switches profiles: the profile is chosen again, but the
		// keyboard is no longer known to be on it.
		s.resume.Active = false
	}
	s.logf("applying the buttons")
	s.set(State{Phase: Applying, Run: noStatus})
	ch := make(chan applyResult, 1)
	s.applyDone = ch
	go func() {
		summary, err := s.apply(ctx, cfg)
		ch <- applyResult{cfg: cfg, summary: summary, err: err}
	}()
}

// applyEnded reports the outcome of an Apply and starts the loop again,
// unless Serve is stopping: its ctx has ended or the next request is a stop.
func (s *Supervisor) applyEnded(ctx context.Context, r applyResult) {
	s.applyDone = nil
	errMsg := ""
	if r.summary != "" {
		s.logf("apply: %s", r.summary)
	}
	if r.err != nil {
		errMsg = "buttons not applied: " + oneLine(r.err)
		s.logf("%s", errMsg)
	}
	if ctx.Err() != nil || s.stopNext() {
		// Serve is stopping: the outcome goes with the Stopped state.
		s.update(func(st *State) bool {
			st.Err, st.Note = errMsg, r.summary
			return false
		})
		return
	}
	s.start(ctx, r.cfg, errMsg, r.summary)
}

// oneLine returns err's text on one line.
func oneLine(err error) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(err.Error(), "\n", "; ")), " ")
}

// runLoop returns the real loop: run.Run with the keyboard, the given
// executor, and lighting and key input when the configuration has [light].
func (s *Supervisor) runLoop(exec *action.Executor) func(context.Context, *layout.Config, *run.Resume, func(run.Status)) error {
	return func(ctx context.Context, cfg *layout.Config, resume *run.Resume, status func(run.Status)) error {
		deps := run.Deps{
			Open: func() (run.Keyboard, error) {
				kb, err := access.OpenKeyboardTraced(s.trace)
				if err != nil {
					return nil, err
				}
				return kb, nil
			},
			Running: procs.Running,
			Exec:    exec,
			Logf:    s.logf,
			Status:  status,
			Resume:  resume,
			Audio:   s.audio,
			Trace:   s.trace,
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
			ch, err := keys.Listen(keyCtx)
			if err != nil {
				endKeys()
				deps.Logf("ripple and key toggles are off: %v", err)
			} else {
				deps.Keys = ch
				defer stopKeys(endKeys, ch, s.logf)
			}
		}
		return run.Run(ctx, cfg, deps)
	}
}

// keyStopWait is the longest wait for the key reader to stop.
const keyStopWait = 2 * time.Second

// stopKeys ends the key reader and waits for its channel to close, so that
// the next start can listen again: only one reader may run at a time.
func stopKeys(end context.CancelFunc, ch <-chan keys.Event, logf func(string, ...any)) {
	end()
	timeout := time.After(keyStopWait)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			logf("the key reader did not stop within %v", keyStopWait)
			return
		}
	}
}

// applyButtons is the real Apply: it refuses while Swarm II runs, applies
// the buttons of cfg with the folder's ledger and saves the ledger, also
// when the apply failed.
func (s *Supervisor) applyButtons(ctx context.Context, cfg *layout.Config) (string, error) {
	if err := access.RefuseWhileSwarm("apply the buttons"); err != nil {
		return "", err
	}
	l, save, err := access.LoadLedger(cfg.Dir)
	if err != nil {
		return "", err
	}
	kb, err := access.OpenKeyboardTraced(s.trace)
	if err != nil {
		return "", err
	}
	defer kb.Close()
	rep, err := apply.Apply(ctx, kb, cfg, l, false, access.ProgramRunning, save)
	err = access.SaveAfter(err, save, access.ErrApplyStopped)
	for _, p := range rep.Lighting {
		s.logf("stored lighting written: profile %d", p)
	}
	for _, r := range rep.Renamed {
		s.logf("profile %d renamed to %s", r.Profile, r.Name)
	}
	for _, n := range rep.Notes {
		s.logf("note: %s", n)
	}
	return summary(rep), err
}

// summary says in one line what an Apply did.
func summary(rep apply.Report) string {
	count := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	parts := []string{
		count(len(rep.Uploaded), "picture", "pictures") + " uploaded",
		fmt.Sprintf("%d unchanged", len(rep.Skipped)),
	}
	if n := len(rep.Restored); n > 0 {
		parts = append(parts, count(n, "position", "positions")+" restored")
	}
	if n := len(rep.Notes); n > 0 {
		parts = append(parts, count(n, "note", "notes")+" in the log")
	}
	return strings.Join(parts, ", ")
}
