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
//
// When profiles have [light], the same goroutine also streams the active
// profile's lighting at its frame rate and passes the KB7's key presses to
// the lighting engine; reading waits at most until the next frame is due.
// Without [light] nothing of this happens: no frame is sent and the
// brightness is not read.
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
	"unicode"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/keys"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/light"
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
	StreamLights(p *proto.LightPayload) error
	ReadLights(p byte) (*proto.LightPayload, int, error)
	Close() error
}

// Status is what the loop is doing, for a display such as a tray icon.
type Status struct {
	// Profile is the keyboard's active profile, -1 when unknown.
	Profile int
	// Name is the active profile's name as the keyboard stores it, "" when
	// unknown.
	Name string
	// Waiting says what the loop waits for: "" (nothing, the keyboard is in
	// use), "keyboard" (not connected), "Swarm II" (left to Swarm II) or
	// WaitReplug (the keyboard does not answer).
	Waiting string
	// Unconfirmed is set when the keyboard did not confirm the switch to
	// Profile: it is assumed active, not known to be.
	Unconfirmed bool
}

// WaitReplug is Status.Waiting while the keyboard does not answer: the last
// replugAfter attempts to connect or switch failed, for another reason than
// the keyboard being gone. Typing may still work; unplugging the keyboard and
// plugging it back in has been the only cure. Run keeps trying meanwhile.
// The status ends as soon as the keyboard answers: notifications enabled, a
// switch confirmed or any input read. Swarm II taking the keyboard, or the
// keyboard being gone, starts the count anew.
const WaitReplug = "replug"

// replugAfter is the number of failures in a row that make WaitReplug.
const replugAfter = 3

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
	// Now to know when the next poll or lighting frame is due and to time the
	// lighting.
	Now func() time.Time
	// Poll is the time between two checks of the running programs (2 s when
	// zero or less).
	Poll time.Duration
	// Logf reports what happens; nil discards it. It is called from more
	// than one goroutine.
	Logf func(format string, args ...any)
	// Light renders the lighting of the profiles with [light]. Nil, or no
	// profile with [light] in the configuration, means no lighting at all:
	// nothing is streamed and the brightness is not read. Once Run has
	// started, only Run uses it.
	Light *light.Engine
	// Keys brings the key presses of the KB7 (nil: none). It is read only
	// when there is lighting, and may be closed.
	Keys <-chan keys.Event
	// Status is called from Run's goroutine whenever the status differs from
	// the last one reported, first with {Profile: -1, Waiting: "keyboard"}
	// before the keyboard is opened for the first time. Nil: no reports.
	Status func(Status)
	// Resume is the profile to keep from an earlier Run in this process;
	// nil: none. It is used at the first attempt to open the keyboard only,
	// and dropped when Swarm II runs before that.
	Resume *Resume
	// Audio is shown on the keyboard's volume mixer page while the keyboard
	// is in use; nil: no mixer, nothing of it is written. See Run.
	Audio Audio
}

// Resume is a profile an earlier Run left the keyboard on.
//
// The first connect of Run goes to Profile instead of the expected profile,
// unless a program of switch_when_running runs whose profile differs: then
// that profile wins, as always. The expected profile itself stays what the
// running programs say, so the next poll does not switch away; later
// program starts and stops switch as always.
//
// With Active, the keyboard is known to be on Profile: the keyboard stayed
// open, or was closed only by the earlier Run's stop, in between. Then no
// switch is sent when Profile is chosen, and a switch to another profile
// needs no detour (see switchTo). Without Active, the switch goes through
// another profile first, as when nothing is known.
type Resume struct {
	Profile byte
	Active  bool
}

const (
	defaultPoll = 2 * time.Second
	readTimeout = 100 * time.Millisecond // longest wait for one input event
	stopTimeout = 5 * time.Second        // longest wait for a running action when stopping
)

// spot is a button's place: profile, page and position.
type spot struct{ profile, page, position byte }

func (s spot) String() string {
	return fmt.Sprintf("profile %d page %d position %d", s.profile, s.page, s.position)
}

// loop is the state of Run, used by its goroutine only.
type loop struct {
	cfg     *layout.Config
	open    func() (Keyboard, error)
	running func([]string) (map[string]bool, error)
	now     func() time.Time
	every   time.Duration
	logf    func(string, ...any)
	w       *worker
	status  func(Status)
	last    Status // the last status reported

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
	confirm  *pendingTap           // the touch of a confirm button that has not been released
	recheck  bool                  // compare the button tables once the keyboard is open again
	openMsg  string                // why the keyboard could not be opened, as last logged
	runMsg   string                // why the programs could not be checked, as last logged
	resume   *Resume               // used by the first connect, then nil
	failures int                   // connects and switches that failed in a row
	assumed  bool                  // the keyboard did not confirm the switch to the active profile

	// Lighting; eng is nil without it.
	eng        *light.Engine
	keys       <-chan keys.Event            // nil without key input, or once it is closed
	flips      map[spot]string              // the state a touch of the button flips itself
	wasRunning map[string]bool              // switch_when_running programs running at the last poll
	nextFrame  time.Time                    // when the next frame is due
	streamed   bool                         // a frame was sent since the keyboard was opened or the last rest frame
	shown      byte                         // the profile of the last frame sent
	ackLogged  bool                         // a missing acknowledgement was logged since the keyboard was opened
	brightMsg  string                       // why report 0x11 could not be read, as last logged
	profNames  [5]string                    // the profile names read when the keyboard was opened; "" when unknown
	stored     map[byte]*proto.LightPayload // each profile's 0x11 colours read since the keyboard was opened, unscaled

	mix mixer // the volume mixer page
}

// Run carries out the actions of the buttons of cfg until ctx is cancelled,
// then releases every held key, closes the keyboard and returns nil. It
// returns an error only when cfg or d is incomplete.
//
// Every poll it checks, in one call of d.Running, Swarm II and the programs
// named in switch_when_running. While Swarm II runs the keyboard stays
// closed. Otherwise the keyboard is opened when it is not, touch
// notifications are enabled and the expected profile is switched to: the
// lowest profile whose program runs, else the desktop profile (d.Resume can
// keep another one at the first connect, see Resume). A change of the
// expected profile switches the keyboard. When Swarm II has closed, the
// managed profiles' button tables are read and differences from the layout
// are reported; nothing is written.
//
// A touch of an application button on a managed position of the active
// profile starts its action (tap) or presses its chord (hold); the touch's
// release releases the chord. Every other input is ignored. A keyboard error
// closes the keyboard; it is opened again at the next poll. A profile switch
// the keyboard does not confirm in time is assumed to have happened.
//
// With lighting (d.Light, and a profile with [light]), the active profile's
// layers are streamed at its frame rate while the keyboard is open, starting
// 2.5 s after notifications are enabled; the key events of d.Keys reach the
// engine before every frame. After every profile change the profile's
// report 0x11 is read, its brightness and stored lighting in one (the last
// level stays when that fails), and brightness notifications are followed. A
// profile's states take their start values when its program starts. A touch
// of a button with a state flips it, unless the button's action sends the
// state's toggle, which flips it already. When the stream stops, because the
// active profile is not lit, Swarm II runs or Run stops, the active
// profile's stored lighting, scaled by the brightness, is sent after the held
// keys are released, as the keyboard keeps the last frame streamed; when it
// could not be read, a rest frame of the profile last shown is sent instead.
// Nothing is sent when the keyboard failed. After a frame the
// keyboard does not acknowledge, the next one waits 2.5 s; any other stream
// error, a write the keyboard does not take included, closes the keyboard.
//
// With d.Audio, the keyboard's volume mixer page shows the system volume and
// up to four programs, in the order they were first seen in this Run (those
// first seen together by ID). From 2.5 s after notifications are enabled,
// and again after every profile change and every change signal of d.Audio,
// the volumes are read: the list (report 0x28) is written when the shown
// names changed, and the a3 level frames that changed are sent, all of them
// after a new list, a reconnect or a profile change; a redraw of the main
// page sends the levels again. This happens between lighting sets, never
// inside one. A slider moved on the page sets the system volume or the
// volume of the program on its row; a row without a program is ignored. An
// error of d.Audio is logged once and the mixer waits for the next change; a
// mixer write the keyboard refused is logged once and tried again after 5 s;
// a stalled frame or a keyboard that is gone closes the keyboard. Buttons and
// lighting go on.
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
	l.report(Status{Profile: -1, Waiting: "keyboard"})
	next := l.now()
	for ctx.Err() == nil {
		l.drainKeys() // so a frame sent below shows every key event so far
		l.drainChanges()
		l.mixStep(ctx) // between two lighting sets, never inside one
		if !l.now().Before(next) {
			l.poll(ctx)
			next = l.now().Add(l.every)
			continue
		}
		left := next.Sub(l.now())
		if l.kb == nil {
			l.idle(ctx, left)
			continue
		}
		wait := min(left, readTimeout)
		if until, ok := l.confirmDue(); ok {
			wait = min(wait, until)
		}
		if due, ok := l.frameDue(); ok {
			until := due.Sub(l.now())
			if until <= 0 {
				l.frame(ctx)
				continue
			}
			wait = min(wait, until)
		}
		l.read(ctx, wait)
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
		cfg: cfg, open: d.Open, status: d.Status, running: d.Running, now: d.Now, every: d.Poll, logf: d.Logf,
		buttons: map[spot]layout.Button{}, chords: map[spot]action.Chord{}, held: map[spot]action.Chord{},
		flips: map[spot]string{}, wasRunning: map[string]bool{}, resume: d.Resume,
	}
	if d.Light != nil && anyLit(cfg.Profiles) {
		l.eng, l.keys = d.Light, d.Keys
	}
	if d.Audio != nil {
		l.mix.audio, l.mix.changes = d.Audio, d.Audio.Changes()
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
			found = true
			s := spot{p.Number, byte(b.Page), byte(b.Position)}
			l.buttons[s] = b
			if name := flipsState(p.Light, b); name != "" {
				l.flips[s] = name
			}
			if b.Mode == layout.Hold {
				var c action.Chord
				ok := len(b.Action) == 1
				if ok {
					c, ok = b.Action[0].(action.Chord)
				}
				if !ok {
					return nil, fmt.Errorf("run: %s: a hold button needs exactly one key chord", s)
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

// report passes s to the status callback unless it is what was reported last.
func (l *loop) report(s Status) {
	if l.status == nil || s == l.last {
		return
	}
	l.last = s
	l.status(s)
}

// ready reports the active profile, with the keyboard in use, unless the
// keyboard does not answer.
func (l *loop) ready() {
	if l.failures >= replugAfter {
		l.report(Status{Profile: -1, Waiting: WaitReplug})
		return
	}
	l.report(Status{Profile: int(l.active), Name: l.nameOf(l.active), Unconfirmed: l.assumed})
}

// waiting reports that the keyboard is not in use: not connected, or not
// answering.
func (l *loop) waiting() {
	if l.failures >= replugAfter {
		l.report(Status{Profile: -1, Waiting: WaitReplug})
		return
	}
	l.report(Status{Profile: -1, Waiting: "keyboard"})
}

// answered ends the failures when input was read from the open keyboard:
// it answers. When it was taken as not answering, the status says it is in
// use again.
func (l *loop) answered() {
	was := l.failures >= replugAfter
	l.failures = 0
	if was {
		l.ready()
	}
}

// failed counts a connect or switch that failed; at the replugAfter-th in a
// row the user is told what to do.
func (l *loop) failed() {
	l.failures++
	if l.failures == replugAfter {
		l.logf("the keyboard does not answer; unplug it and plug it back in")
	}
}

// nameOf is the stored name of profile p, "" when not known.
func (l *loop) nameOf(p byte) string {
	if int(p) < len(l.profNames) {
		return l.profNames[p]
	}
	return ""
}

// prof is profile p for the log: "profile 1 (SC)", or "profile 1" without a
// name.
func (l *loop) prof(p byte) string {
	if n := l.nameOf(p); n != "" {
		return fmt.Sprintf("profile %d (%s)", p, n)
	}
	return fmt.Sprintf("profile %d", p)
}

// readNames reads the profile names when the keyboard can tell them; a
// failure only leaves them unknown.
func (l *loop) readNames() {
	l.profNames = [5]string{}
	nk, ok := l.kb.(interface{ ProfileNames() ([5]string, error) })
	if !ok {
		return
	}
	names, err := nk.ProfileNames()
	if err != nil {
		l.logf("reading the profile names: %v", err)
		return
	}
	for i, n := range names {
		l.profNames[i] = cleanName(n)
	}
}

// cleanName leaves out the control characters and invalid bytes of a profile
// name read from the keyboard, so it cannot upset a log or the tooltip, and
// the spaces around it.
func cleanName(n string) string {
	n = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(n, ""))
	return strings.TrimSpace(n)
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
	l.noteStarts(running)
	if anyOf(running, procs.Swarm) {
		if !l.paused {
			l.paused, l.recheck, l.resume, l.failures = true, true, nil, 0
			l.logf("Swarm II is running; leaving the keyboard to it until it closes")
			l.shut(ctx, true)
			l.report(Status{Profile: -1, Waiting: "Swarm II"})
		}
		return
	}
	if l.paused {
		l.paused = false
		l.logf("Swarm II has closed; taking the keyboard back")
	}
	if l.kb == nil {
		l.connect(ctx, running)
		return
	}
	want := l.expect(running)
	if want == l.expected {
		return
	}
	if want == l.active {
		// The keyboard does not confirm a switch to the active profile.
		l.logf("%s is already active", l.prof(want))
		l.expected = want
		l.ready()
		return
	}
	l.logf("switching to %s", l.prof(want))
	if l.switchTo(ctx, want, switchPlain) {
		l.ready()
	}
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
	p, _ := l.program(running)
	return p
}

// program returns the lowest profile whose program runs and true, or the
// desktop profile and false when none runs.
func (l *loop) program(running map[string]bool) (byte, bool) {
	for _, p := range l.switchers {
		if running[p.SwitchWhenRunning] {
			return p.Number, true
		}
	}
	return l.cfg.DesktopProfile(), false
}

// connect opens the keyboard, enables notifications and switches to the
// expected profile, or at the first connect to the resumed one (see Resume).
// On failure the keyboard is closed again; the next poll retries.
func (l *loop) connect(ctx context.Context, running map[string]bool) {
	want, byProgram := l.program(running)
	target, how := want, switchFresh
	if r := l.resume; r != nil {
		if !byProgram || want == r.Profile {
			target = r.Profile
		}
		if r.Active {
			how = switchPlain
			if target == r.Profile {
				how = switchNone
			}
		}
	}
	kb, err := l.open()
	if err != nil {
		msg := fmt.Sprintf("opening the keyboard: %v", err)
		if errors.Is(err, device.ErrNotFound) {
			msg = "waiting for the keyboard"
			// Unplugged: whatever comes back is a new start, on any
			// profile.
			l.failures, l.resume = 0, nil
		} else {
			// Any other reason, such as access denied, counts toward
			// the replug advice too.
			l.failed()
		}
		l.logOnce(&l.openMsg, msg)
		l.waiting()
		return
	}
	l.openMsg = ""
	l.kb = kb
	l.stored = map[byte]*proto.LightPayload{} // read again: Swarm II may have changed them
	if !l.release(ctx) {
		return
	}
	if err := kb.EnableNotifications(); err != nil {
		l.logf("enabling touch notifications: %v", err)
		l.failed()
		l.drop(ctx)
		return
	}
	// The keyboard answers; the status follows once the switch is done.
	l.failures = 0
	l.resume = nil // the connect switches now: the resume is used up
	l.nextFrame, l.streamed, l.ackLogged = l.now().Add(quiet), false, false
	l.mix.after = l.now().Add(quiet)
	l.readNames()
	if !l.switchTo(ctx, target, how) {
		return
	}
	l.expected = want // a resumed profile stays until the programs change
	l.logf("keyboard ready on %s", l.prof(target))
	l.ready()
	if l.recheck {
		l.check(ctx)
	}
}

// How switchTo switches.
const (
	switchPlain = iota // p is not the active profile
	switchFresh        // the active profile is not known
	switchNone         // p is known to be active: nothing is sent
)

// switchTo releases held keys and switches to p. With switchFresh, used
// while the active profile is not known, the switch goes through another
// profile first: the keyboard does not confirm a switch to the profile that
// is already active. With switchNone nothing is sent and p is taken as
// active.
//
// When the keyboard does not confirm the switch in time, p is taken as
// active, as reopening would most likely only time out again. On any other
// failure the keyboard is closed; the next poll opens it again.
func (l *loop) switchTo(ctx context.Context, p byte, how int) bool {
	if !l.release(ctx) {
		return false
	}
	var err error
	switch how {
	case switchPlain:
		err = l.kb.SwitchProfile(p)
	case switchFresh:
		err = l.kb.SwitchProfileFresh(p)
	}
	if err != nil {
		l.failed()
		if !errors.Is(err, device.ErrTimeout) {
			l.logf("switching to profile %d: %v", p, err)
			l.drop(ctx)
			return false
		}
		l.logf("the keyboard did not confirm the switch to profile %d; assuming it is active", p)
	}
	l.active, l.expected, l.assumed = p, p, err != nil
	if !l.activate(ctx) {
		return false
	}
	if err == nil {
		l.failures = 0
	}
	return true
}

// drop releases held keys and closes the keyboard, after a keyboard error:
// no rest frame is sent.
func (l *loop) drop(ctx context.Context) {
	l.shut(ctx, false)
	if l.kb == nil {
		l.waiting()
	}
}

// shut releases held keys, sends the rest frame if rest is set and closes
// the keyboard. If ctx is cancelled first, the keyboard stays open for stop
// to close after the release.
func (l *loop) shut(ctx context.Context, rest bool) {
	if !l.release(ctx) || l.kb == nil {
		return
	}
	if rest {
		if err := l.rest(); err != nil {
			l.logf("leaving the keyboard's lighting: %v", err)
		}
	}
	l.streamed = false
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
	l.confirm = nil
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

// place is a page and position.
type place struct{ page, position int }

// stale returns, sorted by page and position, the places of the buttons
// whose 0x29 entry does not show them as apply leaves them: an application
// button with a picture slot and the button's label.
func stale(t *proto.Table29, buttons []layout.Button) []place {
	var bad []place
	for _, b := range buttons {
		i, err := proto.Entry29Index(b.Page, b.Position)
		if err != nil {
			continue // refused by layout
		}
		e := t.Entry(i)
		if e.Kind != proto.KindApp || e.Slot == 0 || e.Label != b.Label {
			bad = append(bad, place{b.Page, b.Position})
		}
	}
	sort.Slice(bad, func(i, j int) bool {
		if bad[i].page != bad[j].page {
			return bad[i].page < bad[j].page
		}
		return bad[i].position < bad[j].position
	})
	return bad
}

// positions names sorted places page by page: "page 1 positions 3, 5;
// page 2 position 4".
func positions(ps []place) string {
	var groups []string
	for i := 0; i < len(ps); {
		j := i
		var nums []string
		for ; j < len(ps) && ps[j].page == ps[i].page; j++ {
			nums = append(nums, strconv.Itoa(ps[j].position))
		}
		word := "position "
		if len(nums) > 1 {
			word = "positions "
		}
		groups = append(groups, fmt.Sprintf("page %d %s%s", ps[i].page, word, strings.Join(nums, ", ")))
		i = j
	}
	return strings.Join(groups, "; ")
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
	l.answered()
	switch ev := ev.(type) {
	case proto.ProfileChanged:
		if !l.release(ctx) {
			return
		}
		l.active, l.assumed = ev.Profile, false
		l.logf("%s is active", l.prof(ev.Profile))
		if l.activate(ctx) {
			l.ready()
		}
	case proto.Touch:
		l.touch(ev)
	case proto.SystemSlide:
		l.systemSlide(ev)
	case proto.ProgramSlide:
		l.programSlide(ev)
	case proto.Redraw:
		if ev.View == redrawMain {
			l.mixRedraw()
		}
	case proto.Brightness:
		if l.eng != nil {
			l.eng.SetBrightness(ev.Percent)
		}
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
	s := spot{l.active, ev.Page, ev.Position}
	l.confirm = nil // a release, or a new touch, ends the one waiting
	b, ok := l.buttons[s]
	if !ok || (ev.Down && ev.Kind != proto.TypeApp) {
		return
	}
	what := s.String()
	switch {
	case b.Mode == layout.Tap:
		if ev.Down {
			if b.Confirm > 0 {
				// The keyboard sends one touch per press, so every touch
				// starts a fresh wait, also after a release that was lost.
				l.confirm = &pendingTap{s: s, b: b, since: l.now()}
				return
			}
			l.tapAction(s, b, what)
		}
	case ev.Down:
		if _, held := l.held[s]; !held {
			l.held[s] = l.chords[s]
			l.sent(s, []action.Step{l.chords[s]})
			l.w.add(op{kind: opHoldDown, chord: l.chords[s], what: what})
		}
	default:
		if c, held := l.held[s]; held {
			delete(l.held, s)
			l.w.add(op{kind: opHoldUp, chord: c, what: what})
		}
	}
}

// pendingTap is the latest touch of a button with Confirm. It stays until the
// next touch or release.
type pendingTap struct {
	s     spot
	b     layout.Button
	since time.Time // when the touch began
	done  bool      // the action has been queued
}

// tapAction queues the action of tap button b at s and tells the lighting.
func (l *loop) tapAction(s spot, b layout.Button, what string) {
	l.sent(s, b.Action)
	if len(b.Action) > 0 { // a button with only a state sends nothing
		l.w.add(op{kind: opTap, steps: b.Action, what: what})
	}
}

// confirmDue acts on the pending touch once it has lasted as long as its
// button's Confirm. Otherwise it returns how long that still takes, if there
// is a touch waiting.
func (l *loop) confirmDue() (time.Duration, bool) {
	c := l.confirm
	if c == nil || c.done {
		return 0, false
	}
	left := c.since.Add(c.b.Confirm).Sub(l.now())
	if left > 0 {
		return left, true
	}
	c.done = true
	l.tapAction(c.s, c.b, c.s.String())
	return 0, false
}

// stop releases every held key, after the action that is running, sends the
// active profile's stored lighting (see rest) and closes the keyboard.
// Actions that have not started are dropped.
func (l *loop) stop() {
	l.confirm = nil
	if !l.w.stop(stopTimeout) {
		l.logf("an action is still running; stopping without waiting for it, keys it holds may stay pressed")
	}
	if l.kb == nil {
		return
	}
	defer func() { // also when the rest frame panics
		if err := l.kb.Close(); err != nil {
			l.logf("closing the keyboard: %v", err)
		}
		l.kb = nil
	}()
	if err := l.rest(); err != nil {
		l.logf("leaving the keyboard's lighting: %v", err)
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
