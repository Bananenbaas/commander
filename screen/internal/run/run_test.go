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
	"github.com/Bananenbaas/kb7-commander/screen/internal/keys"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/light"
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

var keyNames = map[action.Key]string{action.A: "A", action.B: "B", action.C: "C", action.N: "N", action.LCtrl: "LCtrl"}

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

	// Lighting, set up by lit before Run starts; nil without it.
	engine *light.Engine
	keys   chan keys.Event

	// Lighting calls, used only by the goroutine that runs Run.
	frames      []frame
	streams     int
	streamAt    []time.Duration // every StreamLights call, failed or not
	streamErr   func(n int) error
	brightReads int
	bright      int
	brightErr   error
	notifyAt    []time.Duration
}

// frame is one set of lighting frames the fake keyboard was sent.
type frame struct {
	at time.Duration
	p  proto.LightPayload
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
			tables: matchingTables(t, testConfig()), bright: 100,
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
		Light:   h.engine,
		Keys:    h.keys,
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
	k.h.notifyAt = append(k.h.notifyAt, time.Since(k.h.start))
	n := k.h.notifies
	k.h.notifies++
	if k.h.notifyErr != nil {
		return k.h.notifyErr(n)
	}
	return nil
}

func (k *fakeKB) SwitchProfile(p byte) error {
	return k.doSwitch("SwitchProfile", fmt.Sprintf("switch %d", p))
}

func (k *fakeKB) SwitchProfileFresh(p byte) error {
	return k.doSwitch("SwitchProfileFresh", fmt.Sprintf("fresh %d", p))
}

func (k *fakeKB) doSwitch(call, rec string) error {
	k.enter(call)
	defer k.leave()
	k.h.noneHeld(call)
	k.h.record(rec)
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

// StreamLights records the frame set in the log: "frame" while LED N shows
// the gear state, "frame (gear off)" otherwise. With the stacks of these
// tests, that second kind is what a rest frame looks like.
func (k *fakeKB) StreamLights(p *proto.LightPayload) error {
	k.enter("StreamLights")
	defer k.leave()
	n := k.h.streams
	k.h.streams++
	k.h.streamAt = append(k.h.streamAt, time.Since(k.h.start))
	if k.h.streamErr != nil {
		if err := k.h.streamErr(n); err != nil {
			k.h.record("stream failed")
			return err
		}
	}
	k.h.frames = append(k.h.frames, frame{at: time.Since(k.h.start), p: *p})
	if gearShown(p) {
		k.h.record("frame")
	} else {
		k.h.record("frame (gear off)")
	}
	return nil
}

func (k *fakeKB) ReadBrightness(p byte) (int, error) {
	k.enter("ReadBrightness")
	defer k.leave()
	k.h.record(fmt.Sprintf("brightness %d", p))
	k.h.brightReads++
	if k.h.brightErr != nil {
		return 0, k.h.brightErr
	}
	return k.h.bright, nil
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
		h.want("poll", "open", "notify", "fresh 0", "poll", "close")
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
		{nil, "fresh 0"},
		{[]string{game}, "fresh 2"},
		{[]string{other}, "fresh 3"},
		{[]string{other, game}, "fresh 2"}, // the lower profile wins
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
		h.want("poll", "open", "notify", "fresh 1", "poll", "close")
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
		h.want("poll", "open", "notify", "fresh 0",
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
		h.want("poll", "open", "notify", "fresh 0", "down A", "up A", "poll", "close")
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
		h.want("poll", "open", "notify", "fresh 0", "down A", "up A", "poll", "close")
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
		h.want("poll", "open", "notify", "fresh 0", "down B", "up B", "down A", "up A", "poll", "close")
	})
}

func TestHoldLastsUntilRelease(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(5, true), touch(5, true)}} // a second touch does not press again
		h.onPoll[1] = func() { h.push(touch(5, false)) }
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "fresh 0", "down LCtrl", "down A",
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
		h.want("poll", "open", "notify", "fresh 0", "down A", "up A", "poll", "close")
	})
}

func TestHoldIsReleasedWhateverTheReleaseKind(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(5, true), widgetTouch(5, 0x03, false)}}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "fresh 0", "down LCtrl", "down A", "up A", "up LCtrl", "poll", "close")
	})
}

func TestHoldCanBePressedAgainAfterARelease(t *testing.T) {
	test(t, func(h *harness) {
		// The profile change releases the held chord without a touch
		// release; the next touch must press it again.
		h.scripts = [][]item{{touch(5, true), event(proto.ProfileChanged{Profile: 0}), touch(5, true)}}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "fresh 0", "down LCtrl", "down A", "up A", "up LCtrl",
			"down LCtrl", "down A", "poll", "up A", "up LCtrl", "close")
	})
}

func TestProfileChangeReleasesHeldKeys(t *testing.T) {
	test(t, func(h *harness) {
		// The release arrives on profile 1, where position 5 is not managed.
		h.scripts = [][]item{{touch(5, true), event(proto.ProfileChanged{Profile: 1}), touch(5, false)}}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "fresh 0", "down LCtrl", "down A", "up A", "up LCtrl", "poll", "close")
	})
}

func TestGameStartAndStopSwitchProfiles(t *testing.T) {
	test(t, func(h *harness) {
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[2] = func() { h.push(tap(3)...) }
		h.onPoll[3] = func() { h.running[game] = false }
		h.onPoll[4] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "fresh 0",
			"poll", "switch 2",
			"poll", "down B", "up B",
			"poll", "switch 0",
			"poll", "close")
		h.wantLogged(1, "switching to profile 2")
		h.wantLogged(1, "switching to profile 0")
	})
}

// The keyboard does not confirm a switch to the active profile: when the
// game's profile was chosen on the keyboard already, nothing is sent.
func TestGameStartOnTheActiveProfileDoesNotSwitch(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{event(proto.ProfileChanged{Profile: 2})}}
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[2] = func() { h.running[game] = false }
		h.onPoll[3] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "fresh 0",
			"poll",
			"poll", "switch 0",
			"poll", "close")
		h.wantLogged(1, "profile 2 is already active")
	})
}

func TestOtherProgramsDoNotSwitch(t *testing.T) {
	test(t, func(h *harness) {
		h.running[game] = true
		h.onPoll[1] = func() { h.running[other] = true } // profile 2 still comes first
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "fresh 2", "poll", "poll", "close")
	})
}

func TestKeysAreReleasedBeforeSwitching(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(5, true)}}
		h.onPoll[1] = func() { h.running[game] = true }
		h.onPoll[2] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "fresh 0", "down LCtrl", "down A",
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
		h.want("poll", "open", "notify", "fresh 0", "down LCtrl", "down A",
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
			"poll", "open", "notify", "fresh 0", "read 0", "read 2", "read 3",
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
		h.want("poll", "open", "notify", "fresh 0",
			"poll", "close",
			"poll", "open", "notify", "fresh 0", "read 0", "read 2", "read 3",
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
		h.want("poll", "open", "notify", "fresh 0", "down LCtrl", "down A",
			"poll", "up A", "up LCtrl", "close",
			"poll", "open failed",
			"poll", "open failed",
			"poll", "open failed",
			"poll", "open", "notify", "fresh 0",
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
				h.want("poll", "open", "notify", "fresh 0", "close",
					"poll", "open", "notify", "fresh 0",
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
				"poll", "open", "notify", "fresh 0",
				"poll", "close")
			h.wantLogged(1, "no answer")
		})
	})
	t.Run("switch", func(t *testing.T) {
		test(t, func(h *harness) {
			h.switchErr = first
			h.onPoll[2] = h.cancel
			h.run(testConfig())
			h.want("poll", "open", "notify", "fresh 0", "close",
				"poll", "open", "notify", "fresh 0",
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
				"poll", "open", "notify", "fresh 0",
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
		h.want("poll", "open", "notify", "fresh 0",
			"poll", "switch 2", "close",
			"poll", "open", "notify", "fresh 2",
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
			h.want("poll", "open", "notify", "fresh 0", "down A", "up A", "poll", "close")
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
			h.want("poll", "open", "notify", "fresh 0",
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
		h.want("poll", "open", "notify", "fresh 0", "down LCtrl", "down A",
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
		h.want("poll", "poll", "poll", "open", "notify", "fresh 0", "poll", "close")
		h.wantLogged(1, "process list unavailable")
	})
}

func TestCancelReleasesAndCloses(t *testing.T) {
	test(t, func(h *harness) {
		h.scripts = [][]item{{touch(5, true)}}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "fresh 0", "down LCtrl", "down A",
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
		h.want("poll", "open", "notify", "fresh 0", "down A", "up A",
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
		h.want("poll", "open", "notify", "fresh 0", "down A", "up A", "poll", "close")
		h.wantLogged(1, "still running")
		// The worker finishes the tap on its own and then ends.
		time.Sleep(time.Minute)
		h.want("poll", "open", "notify", "fresh 0", "down A", "up A", "poll", "close", "down B", "up B")
	})
}

func TestActionErrorsAreLogged(t *testing.T) {
	test(t, func(h *harness) {
		h.launchErr = errors.New("no such program")
		h.scripts = [][]item{script(tap(7), tap(3))}
		h.onPoll[1] = h.cancel
		h.run(testConfig())
		h.want("poll", "open", "notify", "fresh 0", "launch tool", "down A", "up A", "poll", "close")
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

// Lighting.

// litStack is red (200, 0, 0) at 20 frames per second, with a state "gear"
// (toggle N) that lights N green; gear starts as given ("on" or "off").
func litStack(t *testing.T, start string) *light.Stack {
	t.Helper()
	s, err := light.Build(light.Config{FPS: 20, Layers: []light.LayerConfig{
		{Effect: "static", Settings: map[string]any{"color": "#C80000"}},
		{Effect: "state", Keys: []string{"N"}, Settings: map[string]any{
			"name": "gear", "toggle": "N", "start": start, "color": "#00FF00"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const (
	frameGap = 50 * time.Millisecond   // between two frames at 20 frames per second
	quietGap = 2500 * time.Millisecond // no frame this long after notifications are enabled
)

// litConfig is testConfig with lighting on profile 0, where gear starts on,
// and four more buttons on profile 0 that name gear: a hold of N (6), one
// without an action (9), a tap of N (10) and a tap of A (11).
func litConfig(t *testing.T) *layout.Config {
	t.Helper()
	cfg := testConfig()
	for i := range cfg.Profiles {
		p := &cfg.Profiles[i]
		if p.Number != 0 {
			continue
		}
		p.Light = litStack(t, "on")
		gear := func(b layout.Button) layout.Button { b.State = "gear"; return b }
		p.Buttons = append(p.Buttons,
			gear(holdButton(6, "Gear", chord(action.N))),
			gear(tapButton(9)),
			gear(tapButton(10, chord(action.N))),
			gear(tapButton(11, chord(action.A))),
		)
	}
	return cfg
}

// lit gives Run a lighting engine for the lit profiles of cfg and a key
// channel.
func (h *harness) lit(cfg *layout.Config) {
	stacks := map[byte]*light.Stack{}
	for _, p := range cfg.Profiles {
		if p.Light != nil {
			stacks[p.Number] = p.Light
		}
	}
	h.engine = light.NewEngine(stacks, h.start)
	h.keys = make(chan keys.Event, 16)
	h.tables = matchingTables(h.t, cfg)
}

func ledOf(k action.Key) int {
	i, ok := light.LEDOfKey(k)
	if !ok {
		panic(fmt.Sprintf("key %#x has no LED", uint16(k)))
	}
	return i
}

// rgb reads an LED out of a payload: block led/16, offsets +0, +16, +32.
func rgb(p *proto.LightPayload, led int) [3]byte {
	base := led/16*48 + led%16
	return [3]byte{p[base], p[base+16], p[base+32]}
}

// gearShown reports whether N shows green, as it does while gear is on.
func gearShown(p *proto.LightPayload) bool { return rgb(p, ledOf(action.N))[1] > 0 }

// restOf is the rest frame of s at full brightness.
func restOf(s *light.Stack) proto.LightPayload {
	return *light.NewEngine(map[byte]*light.Stack{0: s}, time.Time{}).Rest(0)
}

// compact returns the log with every run of "frame" entries as one "frames".
func compact(log []string) []string {
	var out []string
	for _, s := range log {
		if s == "frame" {
			if len(out) > 0 && out[len(out)-1] == "frames" {
				continue
			}
			s = "frames"
		}
		out = append(out, s)
	}
	return out
}

// wantCompact checks the log with the runs of frames squeezed.
func (h *harness) wantCompact(want ...string) {
	h.t.Helper()
	h.mu.Lock()
	got := compact(h.log)
	h.mu.Unlock()
	if !slices.Equal(got, want) {
		h.t.Errorf("log\n got %q\nwant %q", got, want)
	}
}

// frameAt returns the last frame sent at or before d.
func (h *harness) frameAt(d time.Duration) *proto.LightPayload {
	h.t.Helper()
	var last *proto.LightPayload
	for i := range h.frames {
		if h.frames[i].at <= d {
			last = &h.frames[i].p
		}
	}
	if last == nil {
		h.t.Fatalf("no frame by %v", d)
	}
	return last
}

func (h *harness) gearAt(d time.Duration) bool {
	h.t.Helper()
	return gearShown(h.frameAt(d))
}

// framesBetween counts the frames sent from from up to, not including, to.
func (h *harness) framesBetween(from, to time.Duration) int {
	n := 0
	for _, f := range h.frames {
		if f.at >= from && f.at < to {
			n++
		}
	}
	return n
}

// notified returns when notifications were enabled the i-th time, from 0.
func (h *harness) notified(i int) time.Duration {
	h.t.Helper()
	if i >= len(h.notifyAt) {
		h.t.Fatalf("notifications enabled %d times, want more than %d", len(h.notifyAt), i)
	}
	return h.notifyAt[i]
}

// pressN sends a press and release of N on the key channel.
func (h *harness) pressN() {
	now := time.Now()
	h.keys <- keys.Event{Key: action.N, Down: true, At: now}
	h.keys <- keys.Event{Key: action.N, Down: false, At: now}
}

// Review Focus 1: without [light], run behaves as before.
func TestWithoutLightNothingIsStreamed(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(h *harness)
	}{
		{"no engine", func(h *harness) {}},
		{"an engine but no lit profile", func(h *harness) {
			h.engine = light.NewEngine(nil, h.start)
			h.keys = make(chan keys.Event, 16)
			h.keys <- keys.Event{Key: action.N, Down: true, At: h.start}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			test(t, func(h *harness) {
				c.setup(h)
				h.scripts = [][]item{script(
					event(proto.Brightness{Percent: 50}),
					event(proto.LightsOff{Off: true}),
					event(proto.ProfileChanged{Profile: 0}),
					tap(3),
				)}
				h.onPoll[3] = h.cancel
				h.run(testConfig())
				h.want("poll", "open", "notify", "fresh 0", "down A", "up A", "poll", "poll", "poll", "close")
				if h.streams != 0 || h.brightReads != 0 {
					h.t.Errorf("%d StreamLights and %d ReadBrightness calls, want none", h.streams, h.brightReads)
				}
				if h.maxWait != 100*time.Millisecond {
					h.t.Errorf("NextEvent waited up to %v, want 100ms", h.maxWait)
				}
			})
		})
	}
}

func TestFramesFollowTheFrameRate(t *testing.T) {
	test(t, func(h *harness) {
		cfg := litConfig(t)
		h.lit(cfg)
		h.onPoll[3] = h.cancel
		h.run(cfg)
		h.wantCompact("poll", "open", "notify", "fresh 0", "brightness 0",
			"poll", "frames",
			"poll", "frames",
			"poll", "frame (gear off)", "close")
		if len(h.frames) < 2 {
			h.t.Fatalf("%d frames", len(h.frames))
		}
		start := h.notified(0) + quietGap
		if first := h.frames[0].at; first < start || first > start+frameGap {
			h.t.Errorf("first frame at %v, want from %v to %v", first, start, start+frameGap)
		}
		regular := h.frames[:len(h.frames)-1]
		if n, want := len(regular), int((6*time.Second-start)/frameGap); n < want-1 || n > want+1 {
			h.t.Errorf("%d frames from %v to 6s, want about %d", n, start, want)
		}
		for i := 1; i < len(regular); i++ {
			if gap := regular[i].at - regular[i-1].at; gap < frameGap-time.Millisecond || gap > frameGap+time.Millisecond {
				h.t.Errorf("frame %d came %v after the one before, want %v", i, gap, frameGap)
			}
		}
		if got := rgb(&regular[0].p, ledOf(action.A)); got != [3]byte{200, 0, 0} {
			h.t.Errorf("A shows %v, want red 200", got)
		}
		if got, want := h.frames[len(h.frames)-1].p, restOf(cfg.Profiles[1].Light); got != want {
			h.t.Error("the last frame is not the rest frame")
		}
		want := []time.Duration{0, 2 * time.Second, 4 * time.Second, 6 * time.Second}
		if !slices.Equal(h.pollTimes, want) {
			h.t.Errorf("polls at %v, want %v", h.pollTimes, want)
		}
		if h.maxWait > 100*time.Millisecond {
			h.t.Errorf("NextEvent waited up to %v, want at most 100ms", h.maxWait)
		}
	})
}

// Review Focus 3: the keyboard unplugged mid-stream.
func TestStreamErrors(t *testing.T) {
	t.Run("disconnect", func(t *testing.T) {
		test(t, func(h *harness) {
			cfg := litConfig(t)
			h.lit(cfg)
			h.streamErr = func(n int) error {
				if n == 5 {
					return fmt.Errorf("%w: writing", device.ErrDisconnected)
				}
				return nil
			}
			h.onPoll[4] = h.cancel
			h.run(cfg)
			// No rest frame for a keyboard that failed.
			h.wantCompact("poll", "open", "notify", "fresh 0", "brightness 0",
				"poll", "frames", "stream failed", "close",
				"poll", "open", "notify", "fresh 0", "brightness 0",
				"poll", "frames",
				"poll", "frame (gear off)", "close")
			if n := h.framesBetween(0, 4*time.Second); n != 5 {
				h.t.Errorf("%d frames before the reconnect, want 5", n)
			}
			resume := h.notified(1) + quietGap
			if n := h.framesBetween(4*time.Second, resume); n != 0 {
				h.t.Errorf("%d frames within %v of reconnecting", n, quietGap)
			}
			if n := h.framesBetween(resume, 8*time.Second); n < 29 {
				h.t.Errorf("%d frames after reconnecting, want about 30", n)
			}
			h.wantLogged(1, "lighting", "writing")
		})
	})
	t.Run("missed acknowledgements", func(t *testing.T) {
		test(t, func(h *harness) {
			cfg := litConfig(t)
			h.lit(cfg)
			h.streamErr = func(n int) error {
				if n >= 3 {
					return fmt.Errorf("keyboard: lighting bank 2 not acknowledged: %w", device.ErrTimeout)
				}
				return nil
			}
			h.onPoll[2] = func() { h.push(tap(3)...) }
			h.onPoll[5] = h.cancel
			h.run(cfg)
			// After a missed acknowledgement the next frame waits as long as
			// after enabling notifications; touches are read meanwhile.
			h.wantCompact("poll", "open", "notify", "fresh 0", "brightness 0",
				"poll", "frames", "stream failed",
				"poll", "down A", "up A", "stream failed",
				"poll", "stream failed",
				"poll",
				"poll", "stream failed", "close")
			for i := 4; i < len(h.streamAt)-1; i++ { // the last one is the rest frame
				if gap := h.streamAt[i] - h.streamAt[i-1]; gap < quietGap {
					h.t.Errorf("frame %d tried %v after the one before, want at least %v", i, gap, quietGap)
				}
			}
			if h.maxWait > 100*time.Millisecond {
				h.t.Errorf("NextEvent waited up to %v, want at most 100ms", h.maxWait)
			}
			want := []time.Duration{0, 2 * time.Second, 4 * time.Second, 6 * time.Second, 8 * time.Second, 10 * time.Second}
			if !slices.Equal(h.pollTimes, want) {
				h.t.Errorf("polls at %v, want %v", h.pollTimes, want)
			}
			h.wantLogged(1, "not acknowledged")
		})
	})
	t.Run("stalled write", func(t *testing.T) {
		test(t, func(h *harness) {
			cfg := litConfig(t)
			h.lit(cfg)
			h.streamErr = func(n int) error {
				if n == 0 {
					time.Sleep(5 * time.Second) // how long a write the keyboard does not take lasts
					return fmt.Errorf("keyboard: lighting bank 1: %w", keyboard.ErrStreamStalled)
				}
				return nil
			}
			h.onPoll[5] = h.cancel
			h.run(cfg)
			// The keyboard is closed, not tried again every tick; it is opened
			// again at the next poll and the polls go on two seconds apart.
			h.wantCompact("poll", "open", "notify", "fresh 0", "brightness 0",
				"poll", "stream failed", "close",
				"poll", "open", "notify", "fresh 0", "brightness 0",
				"poll", "frames",
				"poll", "frames",
				"poll", "frame (gear off)", "close")
			want := []time.Duration{0, 2 * time.Second, 7500 * time.Millisecond, 9500 * time.Millisecond,
				11500 * time.Millisecond, 13500 * time.Millisecond}
			if !slices.Equal(h.pollTimes, want) {
				h.t.Errorf("polls at %v, want %v", h.pollTimes, want)
			}
			h.wantLogged(1, "did not take")
		})
	})
}

// Review Focus 4: a switch to a profile without [light].
func TestRestFrameOnUnlitProfile(t *testing.T) {
	t.Run("on the keyboard", func(t *testing.T) {
		test(t, func(h *harness) {
			cfg := litConfig(t)
			h.lit(cfg)
			h.onPoll[2] = func() { h.push(event(proto.ProfileChanged{Profile: 4})) }
			h.onPoll[3] = func() { h.push(event(proto.ProfileChanged{Profile: 0})) }
			h.onPoll[4] = h.cancel
			h.run(cfg)
			h.wantCompact("poll", "open", "notify", "fresh 0", "brightness 0",
				"poll", "frames",
				"poll", "frames", "frame (gear off)", "brightness 4",
				"poll", "brightness 0", "frames",
				"poll", "frame (gear off)", "close")
			if got, want := *h.frameAt(5 * time.Second), restOf(cfg.Profiles[1].Light); got != want {
				h.t.Error("the frame sent on the switch to profile 4 is not the rest frame")
			}
		})
	})
	t.Run("for a program", func(t *testing.T) {
		test(t, func(h *harness) {
			cfg := litConfig(t)
			h.lit(cfg)
			h.onPoll[2] = func() { h.running[game] = true }
			h.onPoll[3] = h.cancel
			h.run(cfg)
			h.wantCompact("poll", "open", "notify", "fresh 0", "brightness 0",
				"poll", "frames",
				"poll", "switch 2", "frame (gear off)", "brightness 2",
				"poll", "close")
		})
	})
}

// Review Focus 5: Swarm II starting while streaming.
func TestSwarmWhileStreaming(t *testing.T) {
	test(t, func(h *harness) {
		cfg := litConfig(t)
		h.lit(cfg)
		h.scripts = [][]item{{touch(5, true)}}
		h.onPoll[2] = func() { h.running[procs.Swarm[0]] = true }
		h.onPoll[4] = func() { h.running[procs.Swarm[0]] = false }
		h.onPoll[6] = h.cancel
		h.run(cfg)
		h.wantCompact("poll", "open", "notify", "fresh 0", "brightness 0", "down LCtrl", "down A",
			"poll", "frames",
			"poll", "up A", "up LCtrl", "frame (gear off)", "close",
			"poll",
			"poll", "open", "notify", "fresh 0", "brightness 0", "read 0", "read 2", "read 3",
			"poll", "frames",
			"poll", "frame (gear off)", "close")
		if n := h.framesBetween(4*time.Second+time.Nanosecond, h.notified(1)+quietGap); n != 0 {
			h.t.Errorf("%d frames while paused or quiet", n)
		}
	})
}

func TestKeyEventsReachTheEngineBeforeTheNextFrame(t *testing.T) {
	test(t, func(h *harness) {
		cfg := litConfig(t)
		h.lit(cfg)
		h.onPoll[2] = h.pressN
		h.onPoll[3] = func() { close(h.keys) } // the listener ended; Run goes on without it
		h.onPoll[4] = h.cancel
		h.run(cfg)
		if !h.gearAt(4*time.Second - time.Nanosecond) {
			h.t.Error("gear off before N was pressed")
		}
		at4 := false
		for _, f := range h.frames {
			if f.at < 4*time.Second {
				continue
			}
			at4 = at4 || f.at == 4*time.Second
			if gearShown(&f.p) {
				h.t.Errorf("frame at %v still shows gear", f.at)
			}
		}
		if !at4 {
			h.t.Error("no frame at the poll of 4s")
		}
		want := []time.Duration{0, 2 * time.Second, 4 * time.Second, 6 * time.Second, 8 * time.Second}
		if !slices.Equal(h.pollTimes, want) {
			h.t.Errorf("polls at %v, want %v", h.pollTimes, want)
		}
	})
}

func TestStateButtons(t *testing.T) {
	test(t, func(h *harness) {
		cfg := litConfig(t)
		h.lit(cfg)
		h.onPoll[2] = func() { h.push(tap(9)...) }      // no action: flips gear, sends nothing
		h.onPoll[3] = func() { h.push(tap(10)...) }     // sends N, the toggle: flips once
		h.onPoll[4] = func() { h.push(tap(11)...) }     // sends A and flips
		h.onPoll[5] = func() { h.push(touch(6, true)) } // holds N, the toggle: flips once
		h.onPoll[6] = h.cancel
		h.run(cfg)
		for i, want := range []bool{true, false, true, false, true} {
			at := time.Duration(2*i+3)*time.Second + 900*time.Millisecond
			if got := h.gearAt(at); got != want {
				h.t.Errorf("gear at %v: %v, want %v", at, got, want)
			}
		}
		var pressed []string
		h.mu.Lock()
		for _, s := range h.log {
			if strings.HasPrefix(s, "down ") || strings.HasPrefix(s, "up ") {
				pressed = append(pressed, s)
			}
		}
		h.mu.Unlock()
		if want := []string{"down N", "up N", "down A", "up A", "down N", "up N"}; !slices.Equal(pressed, want) {
			h.t.Errorf("keys %q, want %q", pressed, want)
		}
	})
}

func TestBrightness(t *testing.T) {
	red := func(h *harness, d time.Duration) byte { return rgb(h.frameAt(d), ledOf(action.A))[0] }
	t.Run("read fails", func(t *testing.T) {
		test(t, func(h *harness) {
			cfg := litConfig(t)
			h.lit(cfg)
			h.brightErr = errors.New("no answer")
			h.onPoll[2] = func() {
				h.push(event(proto.ProfileChanged{Profile: 0}), event(proto.LightsOff{Off: true}),
					event(proto.Brightness{Percent: 50}))
			}
			h.onPoll[3] = h.cancel
			h.run(cfg)
			if got := red(h, 3900*time.Millisecond); got != 200 {
				h.t.Errorf("red %d before the notification, want 200", got)
			}
			if got := red(h, 5900*time.Millisecond); got != 100 {
				h.t.Errorf("red %d at 50%%, want 100", got)
			}
			if got := rgb(h.frameAt(5900*time.Millisecond), ledOf(action.N)); got != [3]byte{0, 127, 0} {
				h.t.Errorf("N %v at 50%%, want green 127", got)
			}
			if h.brightReads != 2 {
				h.t.Errorf("%d brightness reads, want 2", h.brightReads)
			}
			h.wantLogged(1, "no answer")
		})
	})
	t.Run("a new failure after a read is logged again", func(t *testing.T) {
		test(t, func(h *harness) {
			cfg := litConfig(t)
			h.lit(cfg)
			h.brightErr = errors.New("no answer")
			h.onPoll[1] = func() { h.brightErr = nil; h.push(event(proto.ProfileChanged{Profile: 0})) }
			h.onPoll[2] = func() { h.brightErr = errors.New("no answer"); h.push(event(proto.ProfileChanged{Profile: 0})) }
			h.onPoll[3] = h.cancel
			h.run(cfg)
			h.wantLogged(2, "no answer")
		})
	})
	t.Run("disconnect", func(t *testing.T) {
		test(t, func(h *harness) {
			cfg := litConfig(t)
			h.lit(cfg)
			h.brightErr = fmt.Errorf("%w: reading feature", device.ErrDisconnected)
			h.onPoll[1] = func() { h.brightErr = nil }
			h.onPoll[2] = h.cancel
			h.run(cfg)
			h.want("poll", "open", "notify", "fresh 0", "brightness 0", "close",
				"poll", "open", "notify", "fresh 0", "brightness 0",
				"poll", "close")
			h.wantLogged(0, "keyboard ready on profile 0", "reading")
			h.wantLogged(1, "keyboard ready on profile 0")
		})
	})
	t.Run("read", func(t *testing.T) {
		test(t, func(h *harness) {
			cfg := litConfig(t)
			h.lit(cfg)
			h.bright = 40
			h.onPoll[2] = h.cancel
			h.run(cfg)
			if got := red(h, 3900*time.Millisecond); got != 80 {
				h.t.Errorf("red %d at 40%%, want 80", got)
			}
		})
	})
}

// gameLit is testConfig with lighting on the game's profile 2, where gear
// starts on.
func gameLit(t *testing.T) *layout.Config {
	t.Helper()
	cfg := testConfig()
	for i := range cfg.Profiles {
		if cfg.Profiles[i].Number == 2 {
			cfg.Profiles[i].Light = litStack(t, "on")
		}
	}
	return cfg
}

func TestProgramStartResetsStates(t *testing.T) {
	t.Run("start and restart", func(t *testing.T) {
		test(t, func(h *harness) {
			cfg := gameLit(t)
			h.lit(cfg)
			h.running[game] = true
			h.onPoll[1] = h.pressN                           // gear off
			h.onPoll[2] = func() { h.running[game] = false } // to the desktop
			h.onPoll[3] = func() { h.running[game] = true }  // started again: gear on
			h.onPoll[4] = h.pressN                           // gear off
			h.onPoll[5] = func() {                           // switched on the keyboard: no start
				h.push(event(proto.ProfileChanged{Profile: 0}), event(proto.ProfileChanged{Profile: 2}))
			}
			h.onPoll[6] = h.cancel
			h.run(cfg)
			for _, c := range []struct {
				at   time.Duration
				want bool
			}{
				{3900 * time.Millisecond, false},
				{7900 * time.Millisecond, true},
				{9900 * time.Millisecond, false},
				{11900 * time.Millisecond, false},
			} {
				if got := h.gearAt(c.at); got != c.want {
					h.t.Errorf("gear at %v: %v, want %v", c.at, got, c.want)
				}
			}
		})
	})
	t.Run("running at the first poll", func(t *testing.T) {
		test(t, func(h *harness) {
			cfg := gameLit(t)
			h.lit(cfg)
			h.engine.SetActive(2)
			h.engine.Flip(2, "gear") // off before Run starts
			h.running[game] = true
			h.onPoll[2] = h.cancel
			h.run(cfg)
			if !h.gearAt(3900 * time.Millisecond) {
				h.t.Error("gear not back at its start value")
			}
		})
	})
}

func TestPanicInTheRestFrameStillCloses(t *testing.T) {
	test(t, func(h *harness) {
		cfg := litConfig(t)
		h.lit(cfg)
		h.streamErr = func(n int) error {
			if h.ctx.Err() != nil {
				panic("boom")
			}
			return nil
		}
		h.onPoll[2] = h.cancel
		func() {
			defer func() {
				if r := recover(); r != "boom" {
					h.t.Errorf("recovered %v, want the rest frame's panic", r)
				}
			}()
			_ = Run(h.ctx, cfg, h.deps())
			h.t.Error("Run returned")
		}()
		h.wantCompact("poll", "open", "notify", "fresh 0", "brightness 0",
			"poll", "frames", "poll", "close")
	})
}
