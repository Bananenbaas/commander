// SPDX-License-Identifier: GPL-2.0-or-later

package run

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/keyboard"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/procs"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// The tests run inside a synctest bubble: time is virtual, so polls two
// seconds apart and the executor's 40 ms key presses take no real time, and
// virtual time only moves on once every goroutine (the run loop and the
// action worker) is waiting. That makes the order of keyboard calls and
// keystrokes in the shared log deterministic.

var (
	_ Keyboard = (*keyboard.KB)(nil)
	_ Keyboard = (*fakeKB)(nil)
)

const (
	game  = "game.exe"
	other = "other.exe"
)

var keyNames = map[action.Key]string{action.A: "A", action.B: "B", action.C: "C", action.LCtrl: "LCtrl"}

func chord(keys ...action.Key) action.Chord { return action.Chord{Keys: keys} }

func tapButton(pos int, steps ...action.Step) layout.Button {
	return layout.Button{Page: 1, Position: pos, Mode: layout.Tap, Action: steps}
}

func holdButton(pos int, label string, c action.Chord) layout.Button {
	return layout.Button{Page: 1, Position: pos, Label: label, Mode: layout.Hold, Action: []action.Step{c}}
}

// testConfig manages profile 0 (the desktop), profile 2 (while game.exe
// runs) and profile 3 (while other.exe runs); profiles 1 and 4 are not
// managed. The profiles are deliberately not in ascending order.
func testConfig() *layout.Config {
	return &layout.Config{Profiles: []layout.Profile{
		{Number: 3, SwitchWhenRunning: other, Buttons: []layout.Button{tapButton(3, chord(action.C))}},
		{Number: 0, Buttons: []layout.Button{
			tapButton(3, chord(action.A)),
			holdButton(5, "Hold", chord(action.LCtrl, action.A)),
			tapButton(7, action.Run{Program: "tool"}),
			tapButton(8, chord(action.A), action.Wait{D: 3 * time.Second}, chord(action.B)),
		}},
		{Number: 2, SwitchWhenRunning: game, Buttons: []layout.Button{tapButton(3, chord(action.B))}},
	}}
}

// item is what one NextEvent call of a fake keyboard returns, or, with boom
// set, the value it panics with.
type item struct {
	ev   any
	err  error
	boom any
}

func event(ev any) item      { return item{ev: ev} }
func failure(err error) item { return item{err: err} }

// touch is the touch report of an application button.
func touch(pos byte, down bool) item { return widgetTouch(pos, proto.TypeApp, down) }

// widgetTouch is the touch report of a widget of the given type: 0x03 for a
// key, 0x48 for the speaker, proto.TypeApp for an application button.
func widgetTouch(pos, kind byte, down bool) item {
	return event(proto.Touch{Position: pos, Kind: kind, Down: down})
}

// tap is a touch and its release.
func tap(pos byte) []item { return []item{touch(pos, true), touch(pos, false)} }

func script(parts ...any) []item {
	var out []item
	for _, p := range parts {
		switch p := p.(type) {
		case item:
			out = append(out, p)
		case []item:
			out = append(out, p...)
		default:
			panic(fmt.Sprintf("script: %T", p))
		}
	}
	return out
}

// harness provides fakes for everything Run uses and records, in one log,
// the keyboard calls, the polls and the keys pressed and released.
type harness struct {
	t      *testing.T
	ctx    context.Context
	cancel context.CancelFunc
	start  time.Time
	poll   time.Duration

	mu   sync.Mutex
	log  []string
	msgs []string
	down map[action.Key]bool // keys pressed and not yet released
	busy bool                // a keyboard call is running

	// Set before Run starts; read by the action worker.
	launchErr error

	// Used only by the goroutine that runs Run.
	running   map[string]bool
	runErr    error
	onPoll    map[int]func()
	polls     int
	pollTimes []time.Duration
	names     []string
	opens     int
	openErr   func(n int) error
	scripts   [][]item // input of the first, second, ... keyboard opened
	opened    int
	kb        *fakeKB
	tables    map[byte]*proto.Table29
	notifies  int
	notifyErr func(n int) error
	switches  int
	switchErr func(n int) error
	maxWait   time.Duration
}

// test runs f inside a synctest bubble with a fresh harness.
func test(t *testing.T, f func(h *harness)) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		h := &harness{
			t: t, ctx: ctx, cancel: cancel, start: time.Now(), poll: 2 * time.Second,
			down: map[action.Key]bool{}, running: map[string]bool{}, onPoll: map[int]func(){},
			tables: matchingTables(t, testConfig()),
		}
		f(h)
	})
}

func (h *harness) deps() Deps {
	return Deps{
		Open:    h.open,
		Running: h.runningNow,
		Exec:    &action.Executor{In: injector{h}, L: injector{h}},
		Poll:    h.poll,
		Logf:    h.logf,
	}
}

func (h *harness) run(cfg *layout.Config) {
	h.t.Helper()
	if err := Run(h.ctx, cfg, h.deps()); err != nil {
		h.t.Fatalf("Run: %v", err)
	}
}

func (h *harness) record(s string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.log = append(h.log, s)
}

func (h *harness) logf(format string, args ...any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, fmt.Sprintf(format, args...))
}

// want checks the log.
func (h *harness) want(want ...string) {
	h.t.Helper()
	h.mu.Lock()
	got := slices.Clone(h.log)
	h.mu.Unlock()
	if !slices.Equal(got, want) {
		h.t.Errorf("log\n got %q\nwant %q", got, want)
	}
}

// logged returns the messages that contain every one of parts.
func (h *harness) logged(parts ...string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, m := range h.msgs {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(m, p)
		}
		if ok {
			out = append(out, m)
		}
	}
	return out
}

func (h *harness) wantLogged(n int, parts ...string) {
	h.t.Helper()
	if got := h.logged(parts...); len(got) != n {
		h.mu.Lock()
		all := slices.Clone(h.msgs)
		h.mu.Unlock()
		h.t.Errorf("%d messages contain %q, want %d; messages: %q", len(got), parts, n, all)
	}
}

func (h *harness) swarm() bool {
	for _, n := range procs.Swarm {
		if h.running[n] {
			return true
		}
	}
	return false
}

func (h *harness) open() (Keyboard, error) {
	if h.swarm() {
		h.t.Error("Open called while Swarm II runs")
	}
	if h.kb != nil {
		h.t.Error("Open called while a keyboard is open")
	}
	n := h.opens
	h.opens++
	if h.openErr != nil {
		if err := h.openErr(n); err != nil {
			h.record("open failed")
			return nil, err
		}
	}
	k := &fakeKB{h: h}
	if h.opened < len(h.scripts) {
		k.script = slices.Clone(h.scripts[h.opened])
	}
	h.opened++
	h.kb = k
	h.record("open")
	return k, nil
}

// push adds input to the open keyboard.
func (h *harness) push(items ...item) {
	if h.kb == nil {
		h.t.Error("push without an open keyboard")
		return
	}
	h.kb.script = append(h.kb.script, items...)
}

func (h *harness) runningNow(names []string) (map[string]bool, error) {
	n := h.polls
	h.polls++
	if f := h.onPoll[n]; f != nil {
		f()
	}
	h.record("poll")
	h.pollTimes = append(h.pollTimes, time.Since(h.start))
	h.names = slices.Clone(names)
	if n >= 100 {
		h.t.Errorf("Run still going after %d polls", n)
		h.cancel()
	}
	if h.runErr != nil {
		return nil, h.runErr
	}
	out := map[string]bool{}
	for _, name := range names {
		out[name] = h.running[name]
	}
	return out, nil
}

// noneHeld reports a keyboard call made while keys are still pressed.
func (h *harness) noneHeld(call string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.down) > 0 {
		h.t.Errorf("%s while keys are held: %v", call, h.down)
	}
}

// fakeKB is one opened keyboard. NextEvent returns its script, then waits
// out the timeout and returns device.ErrTimeout.
type fakeKB struct {
	h      *harness
	script []item
	closed bool
}

func (k *fakeKB) enter(call string) {
	k.h.mu.Lock()
	defer k.h.mu.Unlock()
	if k.closed {
		k.h.t.Errorf("%s after Close", call)
	}
	if k.h.busy {
		k.h.t.Errorf("%s while another keyboard call runs", call)
	}
	k.h.busy = true
}

func (k *fakeKB) leave() {
	k.h.mu.Lock()
	defer k.h.mu.Unlock()
	k.h.busy = false
}

func (k *fakeKB) EnableNotifications() error {
	k.enter("EnableNotifications")
	defer k.leave()
	k.h.noneHeld("EnableNotifications")
	k.h.record("notify")
	n := k.h.notifies
	k.h.notifies++
	if k.h.notifyErr != nil {
		return k.h.notifyErr(n)
	}
	return nil
}

func (k *fakeKB) SwitchProfile(p byte) error {
	k.enter("SwitchProfile")
	defer k.leave()
	k.h.noneHeld("SwitchProfile")
	k.h.record(fmt.Sprintf("switch %d", p))
	n := k.h.switches
	k.h.switches++
	if k.h.switchErr != nil {
		return k.h.switchErr(n)
	}
	return nil
}

func (k *fakeKB) NextEvent(timeout time.Duration) (any, error) {
	k.enter("NextEvent")
	defer k.leave()
	if timeout <= 0 {
		k.h.t.Errorf("NextEvent(%v)", timeout)
	}
	k.h.maxWait = max(k.h.maxWait, timeout)
	if len(k.script) > 0 {
		it := k.script[0]
		k.script = k.script[1:]
		if it.boom != nil {
			panic(it.boom)
		}
		return it.ev, it.err
	}
	time.Sleep(timeout)
	return nil, device.ErrTimeout
}

func (k *fakeKB) ReadButtons(p byte) (*proto.Table29, error) {
	k.enter("ReadButtons")
	defer k.leave()
	k.h.record(fmt.Sprintf("read %d", p))
	t := k.h.tables[p]
	if t == nil {
		return nil, fmt.Errorf("no table for profile %d", p)
	}
	return t, nil
}

func (k *fakeKB) Close() error {
	k.enter("Close")
	defer k.leave()
	k.h.noneHeld("Close")
	k.h.record("close")
	k.closed = true
	k.h.kb = nil
	return nil
}

// injector is the executor's Injector and Launcher.
type injector struct{ h *harness }

func keyName(k action.Key) string {
	if n, ok := keyNames[k]; ok {
		return n
	}
	return fmt.Sprintf("%#x", uint16(k))
}

func (i injector) Down(k action.Key) error {
	i.h.mu.Lock()
	defer i.h.mu.Unlock()
	i.h.down[k] = true
	i.h.log = append(i.h.log, "down "+keyName(k))
	return nil
}

func (i injector) Up(k action.Key) error {
	i.h.mu.Lock()
	defer i.h.mu.Unlock()
	delete(i.h.down, k)
	i.h.log = append(i.h.log, "up "+keyName(k))
	return nil
}

func (i injector) Run(program string, args []string) error {
	i.h.record("launch " + program)
	return i.h.launchErr
}

func (i injector) Open(target string) error {
	i.h.record("visit " + target)
	return nil
}

// newTable returns an empty 0x29 table of profile p.
func newTable(t *testing.T, p byte) *proto.Table29 {
	t.Helper()
	b := make([]byte, 584)
	b[0], b[1], b[2], b[3] = 0x29, 0x48, 0x02, p
	s := proto.Sum16(b[:582])
	b[582], b[583] = byte(s), byte(s>>8)
	tb, err := proto.ParseTable29(b)
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func setEntry(t *testing.T, tb *proto.Table29, pos int, e proto.Entry29) {
	t.Helper()
	if err := tb.SetEntry(pos, e); err != nil {
		t.Fatal(err)
	}
}

// matchingTables returns, for every profile, a 0x29 table that shows each
// configured button of cfg as apply would leave it.
func matchingTables(t *testing.T, cfg *layout.Config) map[byte]*proto.Table29 {
	t.Helper()
	out := map[byte]*proto.Table29{}
	for p := byte(0); p < 5; p++ {
		out[p] = newTable(t, p)
	}
	slot := byte(1)
	for _, p := range cfg.Profiles {
		for _, b := range p.Buttons {
			setEntry(t, out[p.Number], b.Position, proto.AppEntry(slot, b.Label))
			slot++
		}
	}
	return out
}

// holdConfig has one hold button with the given steps, which layout.Load
// would refuse unless they are a single chord.
func holdConfig(steps ...action.Step) *layout.Config {
	b := layout.Button{Page: 1, Position: 5, Mode: layout.Hold, Action: steps}
	return &layout.Config{Profiles: []layout.Profile{{Number: 0, Buttons: []layout.Button{b}}}}
}

func TestRunNeedsItsParts(t *testing.T) {
	open := func() (Keyboard, error) { return nil, device.ErrNotFound }
	exec := &action.Executor{}
	for _, c := range []struct {
		name string
		cfg  *layout.Config
		d    Deps
	}{
		{"no configuration", nil, Deps{Open: open, Exec: exec}},
		{"no Open", testConfig(), Deps{Exec: exec}},
		{"no executor", testConfig(), Deps{Open: open}},
		{"hold without action", holdConfig(), Deps{Open: open, Exec: exec}},
		{"hold with two steps", holdConfig(chord(action.A), chord(action.B)), Deps{Open: open, Exec: exec}},
		{"hold of a program", holdConfig(action.Run{Program: "tool"}), Deps{Open: open, Exec: exec}},
	} {
		if err := Run(context.Background(), c.cfg, c.d); err == nil {
			t.Errorf("%s: no error", c.name)
		}
	}
}

func TestStartUp(t *testing.T) {
	test(t, func(h *harness) {
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "poll", "close")
		want := append(slices.Clone(procs.Swarm), game, other)
		if !slices.Equal(h.names, want) {
			h.t.Errorf("Running(%q), want Running(%q)", h.names, want)
		}
		h.wantLogged(1, "profile 0")
	})
}

func TestStartUpChoosesTheProfile(t *testing.T) {
	for _, c := range []struct {
		running []string
		want    string
	}{
		{nil, "switch 0"},
		{[]string{game}, "switch 2"},
		{[]string{other}, "switch 3"},
		{[]string{other, game}, "switch 2"}, // the lower profile wins
	} {
		t.Run(strings.Join(c.running, "+"), func(t *testing.T) {
			test(t, func(h *harness) {
				for _, n := range c.running {
					h.running[n] = true
				}
				h.onPoll[1] = h.cancel
				h.run(testConfig())
				h.want("poll", "open", "notify", c.want, "poll", "close")
			})
		})
	}
}

func TestDesktopIsTheFallback(t *testing.T) {
	test(t, func(h *harness) {
		cfg := &layout.Config{Profiles: []layout.Profile{
			{Number: 4, SwitchWhenRunning: game, Buttons: []layout.Button{tapButton(3, chord(action.B))}},
			{Number: 1, Buttons: []layout.Button{tapButton(3, chord(action.A))}},
		}}
		h.onPoll[1] = h.cancel
		h.run(cfg)
		h.want("poll", "open", "notify", "switch 1", "poll", "close")
	})
}

// With only a game's layout, the desktop is the lowest profile without a
// layout, and the keyboard goes back there when the game ends.
func TestGameOnlyFolderSwitchesBack(t *testing.T) {
	test(t, func(h *harness) {
		cfg := &layout.Config{Profiles: []layout.Profile{
			{Number: 1, SwitchWhenRunning: game, Buttons: []layout.Button{tapButton(3, chord(action.B))}},
		}}
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[2] = func() { h.running[game] = false }
		h.onPoll[3] = h.cancel
		h.run(cfg)
		h.want("poll", "open", "notify", "switch 0",
			"poll", "switch 1",
			"poll", "switch 0",
			"poll", "close")
	})
}

func TestTapRunsTheAction(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{tap(3)}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down A", "up A", "poll", "close")
	})
}

func TestIgnoredInput(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{script(
			event(proto.AppTap{ID: 0x125f, Down: true}), // comes with every tap; only the touch counts
			event(proto.AppTap{ID: 0x125f, Down: false}),
			event(proto.Redraw{View: 0x01, Position: 3}),
			tap(9),          // no button there
			touch(3, false), // the release of a tap button
			touch(5, false), // a hold release without its touch
			tap(3),          // control: this one runs
		)}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down A", "up A", "poll", "close")
	})
}

func TestFollowsTheActiveProfile(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{script(
			event(proto.ProfileChanged{Profile: 4}), tap(3), // not managed: nothing
			event(proto.ProfileChanged{Profile: 2}), tap(3), // the game's B
			event(proto.ProfileChanged{Profile: 0}), tap(3), // the desktop's A
		)}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down B", "up B", "down A", "up A", "poll", "close")
	})
}

func TestHoldLastsUntilRelease(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(5, true), touch(5, true)}} // a second touch does not press again
		h.onPoll[1] = func() { h.push(touch(5, false)) }
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down LCtrl", "down A",
			"poll", "up A", "up LCtrl", "poll", "close")
	})
}

func TestOnlyApplicationButtonTouchesAct(t *testing.T) {
	test(t, func(h *harness) {
		// A managed position that is a key (0x03) or the speaker (0x48) on
		// the keyboard sends its own key; acting as well would fire twice.
		var in []item
		for _, kind := range []byte{0x03, 0x48} {
			for _, pos := range []byte{3, 5} { // a tap and a hold button
				in = append(in, widgetTouch(pos, kind, true), widgetTouch(pos, kind, false))
			}
		}
		h.scripts = [][]item{script(in, tap(3))} // control: an application button
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down A", "up A", "poll", "close")
	})
}

func TestHoldIsReleasedWhateverTheReleaseKind(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(5, true), widgetTouch(5, 0x03, false)}}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down LCtrl", "down A", "up A", "up LCtrl", "poll", "close")
	})
}

func TestHoldCanBePressedAgainAfterARelease(t *testing.T) {
	test(t, func(h *harness) {
		// The profile change releases the held chord without a touch
		// release; the next touch must press it again.
		h.scripts = [][]item{{touch(5, true), event(proto.ProfileChanged{Profile: 0}), touch(5, true)}}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down LCtrl", "down A", "up A", "up LCtrl",
			"down LCtrl", "down A", "poll", "up A", "up LCtrl", "close")
	})
}

func TestProfileChangeReleasesHeldKeys(t *testing.T) {
	test(t, func(h *harness) {
		// The release arrives on profile 1, where position 5 is not managed.
		h.scripts = [][]item{{touch(5, true), event(proto.ProfileChanged{Profile: 1}), touch(5, false)}}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down LCtrl", "down A", "up A", "up LCtrl", "poll", "close")
	})
}

func TestGameStartAndStopSwitchProfiles(t *testing.T) {
	test(t, func(h *harness) {
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[2] = func() { h.push(tap(3)...) }
		h.onPoll[3] = func() { h.running[game] = false }
		h.onPoll[4] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0",
			"poll", "switch 2",
			"poll", "down B", "up B",
			"poll", "switch 0",
			"poll", "close")
		h.wantLogged(1, "switching to profile 2")
		h.wantLogged(1, "switching to profile 0")
	})
}

func TestOtherProgramsDoNotSwitch(t *testing.T) {
	test(t, func(h *harness) {
		h.running[game] = true
		h.onPoll[1] = func() { h.running[other] = true } // profile 2 still comes first
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 2", "poll", "poll", "close")
	})
}

func TestKeysAreReleasedBeforeSwitching(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(5, true)}}
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down LCtrl", "down A",
			"poll", "up A", "up LCtrl", "switch 2", "poll", "close")
	})
}

func TestSwarmAppearing(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(5, true)}}
		h.onPoll[1] = func() { h.running[procs.Swarm[0]] = true }
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		// Released and closed within the poll that saw Swarm II; after that
		// the keyboard is not opened or read (the fakes report that).
		h.want("poll", "open", "notify", "switch 0", "down LCtrl", "down A",
			"poll", "up A", "up LCtrl", "close", "poll", "poll")
		if h.opens != 1 {
			h.t.Errorf("%d opens, want 1", h.opens)
		}
		h.wantLogged(1, "Swarm II is running")
	})
}

func TestSwarmAtStartUp(t *testing.T) {
	test(t, func(h *harness) {
		h.running[procs.Swarm[1]] = true
		h.onPoll[3] = func() { h.running[procs.Swarm[1]] = false }
		h.onPoll[4] = h.cancel
		h.run(testConfig())
		h.want("poll", "poll", "poll",
			"poll", "open", "notify", "switch 0", "read 0", "read 2", "read 3",
			"poll", "close")
		h.wantLogged(1, "Swarm II is running")
		h.wantLogged(1, "Swarm II has closed")
		h.wantLogged(0, "apply")
	})
}

func TestResumeWarnsAboutChangedButtons(t *testing.T) {
	test(t, func(h *harness) {
		setEntry(t, h.tables[0], 3, proto.Entry29{Key: 0x07, Kind: 0x01, Label: "k3"}) // a key again
		setEntry(t, h.tables[0], 5, proto.AppEntry(2, ""))                             // label lost
		setEntry(t, h.tables[2], 3, proto.AppEntry(0, "Go"))                           // no picture
		h.onPoll[1] = func() { h.running[procs.Swarm[0]] = true }
		h.onPoll[2] = func() { h.running[procs.Swarm[0]] = false }
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		// Only the reopening after Swarm II reads the tables, and nothing is
		// written: Keyboard has no write call.
		h.want("poll", "open", "notify", "switch 0",
			"poll", "close",
			"poll", "open", "notify", "switch 0", "read 0", "read 2", "read 3",
			"poll", "close")
		h.wantLogged(1, "profile 0", "positions 3, 5", "kb7screen apply")
		h.wantLogged(1, "profile 2", "position 3", "kb7screen apply")
		h.wantLogged(2, "kb7screen apply")
	})
}

func TestStale(t *testing.T) {
	tb := newTable(t, 0)
	setEntry(t, tb, 1, proto.AppEntry(1, ""))
	setEntry(t, tb, 2, proto.AppEntry(2, "Go"))
	setEntry(t, tb, 4, proto.Entry29{Key: 0x47, Kind: 0x01})
	setEntry(t, tb, 6, proto.AppEntry(0, ""))
	setEntry(t, tb, 9, proto.AppEntry(3, "Gone"))
	setEntry(t, tb, 10, proto.AppEntry(4, "Go"))
	buttons := []layout.Button{
		{Page: 1, Position: 10},             // label set on the keyboard, not in the layout
		{Page: 1, Position: 1},              // as applied
		{Page: 1, Position: 2, Label: "Go"}, // as applied
		{Page: 1, Position: 4},              // a key, not an application button
		{Page: 1, Position: 6},              // no picture
		{Page: 1, Position: 9, Label: "Go"}, // another label
	}
	if got, want := stale(tb, buttons), []int{4, 6, 9, 10}; !slices.Equal(got, want) {
		t.Errorf("stale = %v, want %v", got, want)
	}
}

func TestUnplugReleasesAndRetriesEveryPoll(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(5, true)}}
		h.onPoll[1] = func() { h.push(failure(fmt.Errorf("%w: reading input", device.ErrDisconnected))) }
		h.openErr = func(n int) error {
			if n >= 1 && n <= 3 {
				return fmt.Errorf("%w: no stream collection", device.ErrNotFound)
			}
			return nil
		}
		h.onPoll[6] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down LCtrl", "down A",
			"poll", "up A", "up LCtrl", "close",
			"poll", "open failed",
			"poll", "open failed",
			"poll", "open failed",
			"poll", "open", "notify", "switch 0",
			"poll", "close")
		h.wantLogged(1, "waiting for the keyboard")
	})
}

func TestReadErrorsReconnect(t *testing.T) {
	for _, err := range []error{device.ErrClosed, errors.New("broken pipe")} {
		t.Run(err.Error(), func(t *testing.T) {
			test(t, func(h *harness) {
				h.scripts = [][]item{{failure(err)}}
				h.onPoll[2] = h.cancel
				h.run(testConfig())
				h.want("poll", "open", "notify", "switch 0", "close",
					"poll", "open", "notify", "switch 0",
					"poll", "close")
				h.wantLogged(1, err.Error())
			})
		})
	}
}

func TestStartFailuresRetry(t *testing.T) {
	first := func(n int) error {
		if n == 0 {
			return errors.New("no answer")
		}
		return nil
	}
	t.Run("notifications", func(t *testing.T) {
		test(t, func(h *harness) {
			h.notifyErr = first
			h.onPoll[2] = h.cancel
			h.run(testConfig())
			h.want("poll", "open", "notify", "close",
				"poll", "open", "notify", "switch 0",
				"poll", "close")
			h.wantLogged(1, "no answer")
		})
	})
	t.Run("switch", func(t *testing.T) {
		test(t, func(h *harness) {
			h.switchErr = first
			h.onPoll[2] = h.cancel
			h.run(testConfig())
			h.want("poll", "open", "notify", "switch 0", "close",
				"poll", "open", "notify", "switch 0",
				"poll", "close")
			h.wantLogged(1, "no answer")
		})
	})
	t.Run("open", func(t *testing.T) {
		test(t, func(h *harness) {
			h.openErr = func(n int) error {
				if n < 2 {
					return errors.New("access denied")
				}
				return nil
			}
			h.onPoll[3] = h.cancel
			h.run(testConfig())
			h.want("poll", "open failed", "poll", "open failed",
				"poll", "open", "notify", "switch 0",
				"poll", "close")
			h.wantLogged(1, "access denied")
		})
	})
}

func TestGameSwitchFailureReconnects(t *testing.T) {
	test(t, func(h *harness) {
		h.switchErr = func(n int) error {
			if n == 1 {
				return errors.New("no answer")
			}
			return nil
		}
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0",
			"poll", "switch 2", "close",
			"poll", "open", "notify", "switch 2",
			"poll", "close")
	})
}

// unconfirmed is what keyboard.KB.SwitchProfile returns when the keyboard
// does not report the switch in time.
var unconfirmed = fmt.Errorf("keyboard: waiting for the profile change: %w", device.ErrTimeout)

func TestUnconfirmedSwitchIsAssumed(t *testing.T) {
	t.Run("at start-up", func(t *testing.T) {
		test(t, func(h *harness) {
			h.switchErr = func(n int) error { return unconfirmed }
			h.scripts = [][]item{tap(3)}
			h.onPoll[1] = h.cancel
			h.run(testConfig())
			h.want("poll", "open", "notify", "switch 0", "down A", "up A", "poll", "close")
			h.wantLogged(1, "did not confirm the switch to profile 0; assuming it is active")
		})
	})
	t.Run("game", func(t *testing.T) {
		test(t, func(h *harness) {
			h.switchErr = func(n int) error {
				if n == 1 {
					return unconfirmed
				}
				return nil
			}
			h.onPoll[1] = func() { h.running[game] = true }
			h.onPoll[2] = func() { h.push(tap(3)...) } // on the game's profile
			h.onPoll[3] = h.cancel
			h.run(testConfig())
			h.want("poll", "open", "notify", "switch 0",
				"poll", "switch 2",
				"poll", "down B", "up B",
				"poll", "close")
			h.wantLogged(1, "did not confirm the switch to profile 2; assuming it is active")
		})
	})
}

func TestPanicStillReleasesAndCloses(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(5, true)}}
		h.onPoll[1] = func() { h.push(item{boom: "boom"}) }
		func() {
			defer func() {
				if r := recover(); r != "boom" {
					h.t.Errorf("recovered %v, want the keyboard's panic", r)
				}
			}()
			_ = Run(h.ctx, testConfig(), h.deps())
			h.t.Error("Run returned")
		}()
		h.want("poll", "open", "notify", "switch 0", "down LCtrl", "down A",
			"poll", "up A", "up LCtrl", "close")
	})
}

func TestProcessCheckFailure(t *testing.T) {
	test(t, func(h *harness) {
		h.runErr = errors.New("process list unavailable")
		h.onPoll[2] = func() { h.runErr = nil }
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		// Without knowing whether Swarm II runs, the keyboard is not opened.
		h.want("poll", "poll", "poll", "open", "notify", "switch 0", "poll", "close")
		h.wantLogged(1, "process list unavailable")
	})
}

func TestCancelReleasesAndCloses(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(5, true)}}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down LCtrl", "down A",
			"poll", "up A", "up LCtrl", "close")
	})
}

func TestCancelWithoutKeyboard(t *testing.T) {
	test(t, func(h *harness) {
		h.running[procs.Swarm[0]] = true
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "poll")
	})
}

func TestCancelDropsWaitingActions(t *testing.T) {
	test(t, func(h *harness) {
		// Position 8 presses A, waits 3 s and presses B. The tap on position
		// 3 waits behind it and is dropped when Run is stopped at 2 s.
		h.scripts = [][]item{{touch(8, true), touch(3, true)}}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "down A", "up A",
			"poll", "down B", "up B", "close")
	})
}

func TestCancelDoesNotWaitForever(t *testing.T) {
	test(t, func(h *harness) {
		cfg := &layout.Config{Profiles: []layout.Profile{{Number: 0, Buttons: []layout.Button{
			tapButton(3, chord(action.A), action.Wait{D: 60 * time.Second}, chord(action.B)),
		}}}}
		h.scripts = [][]item{{touch(3, true)}}
		h.onPoll[1] = h.cancel
		h.run(cfg)
		if d := time.Since(h.start); d > 10*time.Second {
			h.t.Errorf("Run returned after %v", d)
		}
		h.want("poll", "open", "notify", "switch 0", "down A", "up A", "poll", "close")
		h.wantLogged(1, "still running")
		// The worker finishes the tap on its own and then ends.
		time.Sleep(time.Minute)
		h.want("poll", "open", "notify", "switch 0", "down A", "up A", "poll", "close", "down B", "up B")
	})
}

func TestActionErrorsAreLogged(t *testing.T) {
	test(t, func(h *harness) {
		h.launchErr = errors.New("no such program")
		h.scripts = [][]item{script(tap(7), tap(3))}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "switch 0", "launch tool", "down A", "up A", "poll", "close")
		h.wantLogged(1, "profile 0 position 7", "no such program")
	})
}

func TestPollTiming(t *testing.T) {
	for _, swarm := range []bool{false, true} {
		t.Run(fmt.Sprintf("swarm=%v", swarm), func(t *testing.T) {
			test(t, func(h *harness) {
				h.poll = 250 * time.Millisecond
				h.running[procs.Swarm[0]] = swarm
				h.onPoll[4] = h.cancel
				h.run(testConfig())
				want := []time.Duration{0, 250 * time.Millisecond, 500 * time.Millisecond, 750 * time.Millisecond, time.Second}
				if !slices.Equal(h.pollTimes, want) {
					h.t.Errorf("polls at %v, want %v", h.pollTimes, want)
				}
				if h.maxWait > 100*time.Millisecond {
					h.t.Errorf("NextEvent waited up to %v, want at most 100ms", h.maxWait)
				}
			})
		})
	}
}
