// SPDX-License-Identifier: GPL-2.0-or-later

// Package run carries out the buttons' actions while the keyboard is in use.
//
// Run reads the taps on the keyboard, follows the active profile, switches
// profiles when the configured programs start and stop, and leaves the
// keyboard alone while Swarm II runs. One goroutine owns the keyboard; the
// actions run one at a time, in the order of the taps, on a worker of their
// own. Reading the keyboard does not wait for an action, except when held
// keys must be released.
//
// Held keys are released, and the release waited for, before every profile
// switch and before notifications are enabled (the keyboard package drops
// the touch releases that arrive meanwhile), on every profile change, before
// the keyboard is closed and when Run stops. The release comes after the
// actions queued before it, so at those points reading waits until they have
// finished.
//
// Only touches of kind 0x05, the widget type of application buttons, start
// an action. Modifier and macro widgets have that type too, and a touch does
// not tell them apart. A touch of a managed position that the keyboard shows
// as another widget, such as a key, is ignored: the keyboard acts on it
// itself.
package run

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/procs"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// Keyboard holds the keyboard operations Run uses. The keyboard package's
// *KB provides them. Run calls them from one goroutine only.
type Keyboard interface {
	EnableNotifications() error
	SwitchProfile(p byte) error
	SwitchProfileFresh(p byte) error
	NextEvent(timeout time.Duration) (any, error)
	ReadButtons(p byte) (*proto.Table29, error)
	Close() error
}

// Deps is what Run works with.
type Deps struct {
	// Open opens the keyboard. It returns an error matching
	// device.ErrNotFound while no keyboard is connected. Required.
	Open func() (Keyboard, error)
	// Running reports which of the named programs run (procs.Running when
	// nil).
	Running func([]string) (map[string]bool, error)
	// Exec carries out the actions. Required.
	Exec *action.Executor
	// Now is the clock that schedules the polls (time.Now when nil). It must
	// follow real time: Run waits with the runtime's timers and only reads
	// Now to know when the next poll is due.
	Now func() time.Time
	// Poll is the time between two checks of the running programs (2 s when
	// zero or less).
	Poll time.Duration
	// Logf reports what happens; nil discards it. It is called from more
	// than one goroutine.
	Logf func(format string, args ...any)
}

const (
	defaultPoll = 2 * time.Second
	readTimeout = 100 * time.Millisecond // longest wait for one input event
	stopTimeout = 5 * time.Second        // longest wait for a running action when stopping
	page        = 1                      // the only page that is measured
)

// spot is a button's place: profile and position on page 1.
type spot struct{ profile, position byte }

// loop is the state of Run, used by its goroutine only.
type loop struct {
	cfg     *layout.Config
	open    func() (Keyboard, error)
	running func([]string) (map[string]bool, error)
	now     func() time.Time
	every   time.Duration
	logf    func(string, ...any)
	w       *worker

	names     []string         // programs checked at every poll: Swarm II first
	switchers []layout.Profile // profiles with switch_when_running, ascending
	managed   []layout.Profile // profiles with buttons, ascending
	buttons   map[spot]layout.Button
	chords    map[spot]action.Chord // the chord of every hold button

	kb       Keyboard              // nil while closed
	active   byte                  // the keyboard's active profile, while open
	expected byte                  // the profile last switched to
	held     map[spot]action.Chord // hold buttons pressed and not yet released
	paused   bool                  // Swarm II runs
	recheck  bool                  // compare the button tables once the keyboard is open again
	openMsg  string                // why the keyboard could not be opened, as last logged
	runMsg   string                // why the programs could not be checked, as last logged
}

// Run carries out the actions of the buttons of cfg until ctx is cancelled,
// then releases every held key, closes the keyboard and returns nil. It
// returns an error only when cfg or d is incomplete.
//
// Every poll it checks, in one call of d.Running, Swarm II and the programs
// named in switch_when_running. While Swarm II runs the keyboard stays
// closed. Otherwise the keyboard is opened when it is not, touch
// notifications are enabled and the expected profile is switched to: the
// lowest profile whose program runs, else the desktop profile. A change of
// the expected profile switches the keyboard. When Swarm II has closed, the
// managed profiles' button tables are read and differences from the layout
// are reported; nothing is written.
//
// A touch of an application button on a managed position of the active
// profile starts its action (tap) or presses its chord (hold); the touch's
// release releases the chord. Every other input is ignored. A keyboard error
// closes the keyboard; it is opened again at the next poll. A profile switch
// the keyboard does not confirm in time is assumed to have happened.
//
// If a call panics, Run still releases every held key and closes the
// keyboard before the panic goes on.
func Run(ctx context.Context, cfg *layout.Config, d Deps) error {
	l, err := newLoop(cfg, d)
	if err != nil {
		return err
	}
	l.w = startWorker(d.Exec, l.logf)
	defer l.stop() // also when a call panics: keys released, keyboard closed, worker ended
	next := l.now()
	for ctx.Err() == nil {
		if !l.now().Before(next) {
			l.poll(ctx)
			next = l.now().Add(l.every)
			continue
		}
		left := next.Sub(l.now())
		if l.kb == nil {
			sleep(ctx, left)
			continue
		}
		l.read(ctx, min(left, readTimeout))
	}
	return nil
}

func newLoop(cfg *layout.Config, d Deps) (*loop, error) {
	switch {
	case cfg == nil:
		return nil, errors.New("run: no configuration")
	case d.Open == nil:
		return nil, errors.New("run: no way to open the keyboard")
	case d.Exec == nil:
		return nil, errors.New("run: no executor")
	}
	l := &loop{
		cfg: cfg, open: d.Open, running: d.Running, now: d.Now, every: d.Poll, logf: d.Logf,
		buttons: map[spot]layout.Button{}, chords: map[spot]action.Chord{}, held: map[spot]action.Chord{},
	}
	if l.running == nil {
		l.running = procs.Running
	}
	if l.now == nil {
		l.now = time.Now
	}
	if l.every <= 0 {
		l.every = defaultPoll
	}
	if l.logf == nil {
		l.logf = func(string, ...any) {}
	}
	profiles := append([]layout.Profile(nil), cfg.Profiles...)
	sort.SliceStable(profiles, func(i, j int) bool { return profiles[i].Number < profiles[j].Number })
	l.names = append([]string(nil), procs.Swarm...)
	seen := map[string]bool{}
	for _, n := range procs.Swarm {
		seen[n] = true
	}
	for _, p := range profiles {
		if n := p.SwitchWhenRunning; n != "" {
			l.switchers = append(l.switchers, p)
			if !seen[n] {
				seen[n] = true
				l.names = append(l.names, n)
			}
		}
		found := false
		for _, b := range p.Buttons {
			if b.Page != page {
				continue
			}
			found = true
			s := spot{p.Number, byte(b.Position)}
			l.buttons[s] = b
			if b.Mode == layout.Hold {
				var c action.Chord
				ok := len(b.Action) == 1
				if ok {
					c, ok = b.Action[0].(action.Chord)
				}
				if !ok {
					return nil, fmt.Errorf("run: profile %d position %d: a hold button needs exactly one key chord", p.Number, b.Position)
				}
				l.chords[s] = c
			}
		}
		if found {
			l.managed = append(l.managed, p)
		}
	}
	return l, nil
}

// sleep waits for d or until ctx is cancelled.
func sleep(ctx context.Context, d time.Duration) {
	if d <= 0 {
		return
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// logOnce logs msg unless it is what *last says was logged last.
func (l *loop) logOnce(last *string, msg string) {
	if *last != msg {
		l.logf("%s", msg)
		*last = msg
	}
}

// poll checks the programs and acts on what changed.
func (l *loop) poll(ctx context.Context) {
	running, err := l.running(l.names)
	if err != nil {
		l.logOnce(&l.runMsg, fmt.Sprintf("checking the running programs: %v", err))
		return
	}
	l.runMsg = ""
	if ctx.Err() != nil {
		return
	}
	if anyOf(running, procs.Swarm) {
		if !l.paused {
			l.paused, l.recheck = true, true
			l.logf("Swarm II is running; leaving the keyboard to it until it closes")
			l.drop(ctx)
		}
		return
	}
	if l.paused {
		l.paused = false
		l.logf("Swarm II has closed; taking the keyboard back")
	}
	want := l.expect(running)
	if l.kb == nil {
		l.connect(ctx, want)
		return
	}
	if want == l.expected {
		return
	}
	if want == l.active {
		// The keyboard does not confirm a switch to the active profile.
		l.logf("profile %d is already active", want)
		l.expected = want
		return
	}
	l.logf("switching to profile %d", want)
	l.switchTo(ctx, want, false)
}

func anyOf(running map[string]bool, names []string) bool {
	for _, n := range names {
		if running[n] {
			return true
		}
	}
	return false
}

// expect returns the profile the keyboard should be on: the lowest one whose
// program runs, else the desktop profile.
func (l *loop) expect(running map[string]bool) byte {
	for _, p := range l.switchers {
		if running[p.SwitchWhenRunning] {
			return p.Number
		}
	}
	return l.cfg.DesktopProfile()
}

// connect opens the keyboard, enables notifications and switches to want.
// On failure the keyboard is closed again; the next poll retries.
func (l *loop) connect(ctx context.Context, want byte) {
	kb, err := l.open()
	if err != nil {
		msg := fmt.Sprintf("opening the keyboard: %v", err)
		if errors.Is(err, device.ErrNotFound) {
			msg = "waiting for the keyboard"
		}
		l.logOnce(&l.openMsg, msg)
		return
	}
	l.openMsg = ""
	l.kb = kb
	if !l.release(ctx) {
		return
	}
	if err := kb.EnableNotifications(); err != nil {
		l.logf("enabling touch notifications: %v", err)
		l.drop(ctx)
		return
	}
	if !l.switchTo(ctx, want, true) {
		return
	}
	l.logf("keyboard ready on profile %d", want)
	if l.recheck {
		l.check(ctx)
	}
}

// switchTo releases held keys and switches to p. With fresh, used while the
// active profile is not known, the switch goes through another profile
// first: the keyboard does not confirm a switch to the profile that is
// already active.
//
// When the keyboard does not confirm the switch in time, p is taken as
// active, as reopening would most likely only time out again. On any other
// failure the keyboard is closed; the next poll opens it again.
func (l *loop) switchTo(ctx context.Context, p byte, fresh bool) bool {
	if !l.release(ctx) {
		return false
	}
	switchProfile := l.kb.SwitchProfile
	if fresh {
		switchProfile = l.kb.SwitchProfileFresh
	}
	if err := switchProfile(p); err != nil {
		if !errors.Is(err, device.ErrTimeout) {
			l.logf("switching to profile %d: %v", p, err)
			l.drop(ctx)
			return false
		}
		l.logf("the keyboard did not confirm the switch to profile %d; assuming it is active", p)
	}
	l.active, l.expected = p, p
	return true
}

// drop releases held keys and closes the keyboard. If ctx is cancelled
// first, the keyboard stays open for stop to close after the release.
func (l *loop) drop(ctx context.Context) {
	if !l.release(ctx) || l.kb == nil {
		return
	}
	if err := l.kb.Close(); err != nil {
		l.logf("closing the keyboard: %v", err)
	}
	l.kb = nil
}

// release has the worker release every held key, after the actions queued
// before it, and waits until it has. It returns false if ctx is cancelled
// first.
func (l *loop) release(ctx context.Context) bool {
	clear(l.held)
	done := make(chan struct{})
	l.w.add(op{kind: opRelease, what: "releasing the held keys", done: done})
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}

// check compares the managed profiles' button tables with the layout and
// reports the positions that no longer show their button.
func (l *loop) check(ctx context.Context) {
	for _, p := range l.managed {
		t, err := l.kb.ReadButtons(p.Number)
		if err != nil {
			l.logf("reading the buttons of profile %d: %v", p.Number, err)
			if errors.Is(err, device.ErrDisconnected) || errors.Is(err, device.ErrClosed) {
				l.drop(ctx)
				return // checked once the keyboard is back
			}
			continue
		}
		if bad := stale(t, p.Buttons); len(bad) > 0 {
			l.logf("profile %d, %s: the keyboard's button table differs from the layout (Swarm II may have changed it); run kb7screen apply to put the buttons back",
				p.Number, positions(bad))
		}
	}
	l.recheck = false
}

// stale returns, in ascending order, the positions of the page-1 buttons
// whose 0x29 entry does not show them as apply leaves them: an application
// button with a picture slot and the button's label.
func stale(t *proto.Table29, buttons []layout.Button) []int {
	var bad []int
	for _, b := range buttons {
		if b.Page != page {
			continue
		}
		e := t.Entry(b.Position)
		if e.Kind != proto.KindApp || e.Slot == 0 || e.Label != b.Label {
			bad = append(bad, b.Position)
		}
	}
	sort.Ints(bad)
	return bad
}

func positions(ps []int) string {
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = strconv.Itoa(p)
	}
	if len(ps) == 1 {
		return "position " + s[0]
	}
	return "positions " + strings.Join(s, ", ")
}

// read waits up to timeout for one input event and acts on it.
func (l *loop) read(ctx context.Context, timeout time.Duration) {
	ev, err := l.kb.NextEvent(timeout)
	if err != nil {
		if errors.Is(err, device.ErrTimeout) {
			return
		}
		l.logf("reading the keyboard: %v; opening it again", err)
		l.drop(ctx)
		return
	}
	switch ev := ev.(type) {
	case proto.ProfileChanged:
		if !l.release(ctx) {
			return
		}
		l.active = ev.Profile
		l.logf("profile %d is active", ev.Profile)
	case proto.Touch:
		l.touch(ev)
	}
}

// touch queues the action of the button touched or released. A hold chord
// is pressed once per touch and released only if it was pressed.
//
// A touch counts only if its kind is proto.TypeApp, the widget type of an
// application button, which modifier and macro widgets share. Any other
// widget on a managed position (a key left there, or put back by Swarm II
// or restore) makes the keyboard send its own key, and acting as well would
// fire twice. The release of a pressed chord is not checked, so that it is
// always released.
func (l *loop) touch(ev proto.Touch) {
	s := spot{l.active, ev.Position}
	b, ok := l.buttons[s]
	if !ok || (ev.Down && ev.Kind != proto.TypeApp) {
		return
	}
	what := fmt.Sprintf("profile %d position %d", s.profile, s.position)
	switch {
	case b.Mode == layout.Tap:
		if ev.Down {
			l.w.add(op{kind: opTap, steps: b.Action, what: what})
		}
	case ev.Down:
		if _, held := l.held[s]; !held {
			l.held[s] = l.chords[s]
			l.w.add(op{kind: opHoldDown, chord: l.chords[s], what: what})
		}
	default:
		if c, held := l.held[s]; held {
			delete(l.held, s)
			l.w.add(op{kind: opHoldUp, chord: c, what: what})
		}
	}
}

// stop releases every held key, after the action that is running, and
// closes the keyboard. Actions that have not started are dropped.
func (l *loop) stop() {
	if !l.w.stop(stopTimeout) {
		l.logf("an action is still running; stopping without waiting for it, keys it holds may stay pressed")
	}
	if l.kb != nil {
		if err := l.kb.Close(); err != nil {
			l.logf("closing the keyboard: %v", err)
		}
		l.kb = nil
	}
}

type opKind int

const (
	opTap opKind = iota
	opHoldDown
	opHoldUp
	opRelease
)

// op is one call of the executor.
type op struct {
	kind  opKind
	steps []action.Step // opTap
	chord action.Chord  // opHoldDown, opHoldUp
	what  string        // for messages
	done  chan struct{} // opRelease: closed once the keys are released
}

// worker calls the executor for the queued ops, one at a time, in order.
// Adding to the queue never blocks.
type worker struct {
	exec *action.Executor
	logf func(string, ...any)

	mu     sync.Mutex
	queue  []op
	closed bool

	wake   chan struct{} // capacity 1: something was queued
	exited chan struct{} // closed when the worker has finished
}

func startWorker(e *action.Executor, logf func(string, ...any)) *worker {
	w := &worker{exec: e, logf: logf, wake: make(chan struct{}, 1), exited: make(chan struct{})}
	go w.run()
	return w
}

func (w *worker) add(o op) {
	w.mu.Lock()
	w.queue = append(w.queue, o)
	w.mu.Unlock()
	w.signal()
}

func (w *worker) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// next returns the next op, waiting for one; false once the worker is
// stopped and its queue is empty.
func (w *worker) next() (op, bool) {
	for {
		w.mu.Lock()
		if len(w.queue) > 0 {
			o := w.queue[0]
			w.queue[0] = op{}
			w.queue = w.queue[1:]
			w.mu.Unlock()
			return o, true
		}
		closed := w.closed
		w.mu.Unlock()
		if closed {
			return op{}, false
		}
		<-w.wake
	}
}

func (w *worker) run() {
	defer close(w.exited)
	for {
		o, ok := w.next()
		if !ok {
			return
		}
		w.do(o)
	}
}

func (w *worker) do(o op) {
	var err error
	switch o.kind {
	case opTap:
		err = w.exec.Tap(o.steps)
	case opHoldDown:
		err = w.exec.HoldDown(o.chord)
	case opHoldUp:
		err = w.exec.HoldUp(o.chord)
	case opRelease:
		err = w.exec.ReleaseAll()
		if o.done != nil {
			close(o.done)
		}
	}
	if err != nil {
		w.logf("%s: %v", o.what, err)
	}
}

// stop drops the queued ops, has every held key released after the op that
// is running, and waits up to timeout for the worker to finish. It reports
// whether it did.
func (w *worker) stop(timeout time.Duration) bool {
	w.mu.Lock()
	w.queue = []op{{kind: opRelease, what: "releasing the held keys"}}
	w.closed = true
	w.mu.Unlock()
	w.signal()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-w.exited:
		return true
	case <-t.C:
		return false
	}
}
