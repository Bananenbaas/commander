// SPDX-License-Identifier: GPL-2.0-or-later

package apply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/keyboard"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/ledger"
	"github.com/Bananenbaas/kb7-commander/screen/internal/picture"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// fakeKB models the keyboard: a 0x29 and a 0x25 table per profile, the
// picture slots, the active profile and the input waiting to be read. Every
// call but NextEvent is recorded.
//
// A 0x29 table goes to the profile it names, which must be active. Type
// writes are queued, as the keyboard queues them: each changes the profile
// that is active when the fake gets to it, which is at the next switch (after
// the switch has taken effect), table read or redraw wait. A redraw wait
// succeeds once a queued type table was taken over since the last one.
type fakeKB struct {
	t         *testing.T
	b29       map[byte]*proto.Table29
	b25       map[byte]*proto.Table25
	slots     map[byte][]byte
	active    int // -1 until the first switch
	notified  bool
	calls     []string
	events    []any                 // input waiting to be read
	eventErr  error                 // returned once by NextEvent, before the events
	hook      func(call string)     // called with every recorded call
	uploadErr func(slot byte) error // after the upload is recorded
	switchErr func(p byte) error
	redrawErr error // returned by WaitRedraw instead of the redraw

	b11         map[byte]*proto.Table11 // 0x11 tables, made on the first read
	lightStatus []byte                  // status after a 0x11 write; nil means 05 01 00 00

	queue  []func(p byte) error // type writes not yet taken over
	redraw bool                 // a type table was taken over since the last redraw wait
}

// process takes the queued type writes over into the active profile.
func (f *fakeKB) process() {
	for _, w := range f.queue {
		if f.active < 0 {
			f.t.Error("fake: widget type taken over before a profile switch")
			continue
		}
		if err := w(byte(f.active)); err != nil {
			f.t.Error(err)
		}
	}
	f.queue = nil
}

var (
	_ Keyboard = (*fakeKB)(nil)
	_ Keyboard = (*keyboard.KB)(nil)
)

func seal(b []byte) []byte {
	n := len(b) - 2
	s := proto.Sum16(b[:n])
	b[n], b[n+1] = byte(s), byte(s>>8)
	return b
}

// keyEntry and keyType are what the fake holds before anything is applied.
func keyEntry(pos int) proto.Entry29 {
	return proto.Entry29{Key: byte(0x04 + pos), Kind: 0x01, Label: fmt.Sprintf("k%d", pos)}
}

func keyType(pos int) proto.TypeEntry { return proto.TypeEntry{Type: 0x03, Value: byte(0x40 + pos)} }

func newFake(t *testing.T) *fakeKB {
	f := &fakeKB{t: t, b29: map[byte]*proto.Table29{}, b25: map[byte]*proto.Table25{}, b11: map[byte]*proto.Table11{}, slots: map[byte][]byte{}, active: -1}
	for p := byte(0); p < 5; p++ {
		b := make([]byte, 584)
		b[0], b[1], b[2], b[3] = 0x29, 0x48, 0x02, p
		t29, err := proto.ParseTable29(seal(b))
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 35; i++ {
			if err := t29.SetEntry(i, keyEntry(i)); err != nil {
				t.Fatal(err)
			}
		}
		c := make([]byte, 164)
		c[0], c[1], c[2] = 0x25, 0xa4, p
		t25, err := proto.ParseTable25(seal(c))
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 38; i++ {
			if err := t25.SetType(i, keyType(i)); err != nil {
				t.Fatal(err)
			}
		}
		f.b29[p], f.b25[p] = t29, t25
	}
	return f
}

func (f *fakeKB) record(format string, args ...any) {
	c := fmt.Sprintf(format, args...)
	f.calls = append(f.calls, c)
	if f.hook != nil {
		f.hook(c)
	}
}

// switches counts the profile switches recorded from call n on.
func (f *fakeKB) switches(n int) int {
	count := 0
	for _, c := range f.calls[n:] {
		if strings.HasPrefix(c, "switch ") || strings.HasPrefix(c, "fresh ") {
			count++
		}
	}
	return count
}

// userSwitch changes the profile on the keyboard itself, which reports it.
func (f *fakeKB) userSwitch(p byte) {
	f.active = int(p)
	f.events = append(f.events, proto.ProfileChanged{Profile: p})
}

func (f *fakeKB) since(n int) string { return strings.Join(f.calls[n:], ", ") }

// save records the caller's ledger save among the keyboard calls.
func (f *fakeKB) save() error {
	f.record("save")
	return nil
}

func (f *fakeKB) EnableNotifications() error {
	f.record("notify")
	f.notified = true
	return nil
}

// SwitchProfile drops the waiting input first, as the keyboard package does.
// A switch to the active profile is not confirmed, as on the keyboard.
func (f *fakeKB) SwitchProfile(p byte) error {
	f.record("switch %d", p)
	return f.doSwitch(p, false)
}

// SwitchProfileFresh goes through another profile, so it is confirmed even
// when p is already active.
func (f *fakeKB) SwitchProfileFresh(p byte) error {
	f.record("fresh %d", p)
	return f.doSwitch(p, true)
}

func (f *fakeKB) doSwitch(p byte, fresh bool) error {
	f.events = nil
	if !f.notified {
		return errors.New("fake: no profile notification without 0x0e")
	}
	if f.switchErr != nil {
		if err := f.switchErr(p); err != nil {
			return err
		}
	}
	if !fresh && f.active == int(p) {
		return fmt.Errorf("fake: switch to the active profile %d not confirmed: %w", p, device.ErrTimeout)
	}
	f.active = int(p)
	f.process()
	return nil
}

func (f *fakeKB) WaitRedraw(time.Duration) error {
	f.record("redraw %d", f.active)
	if f.redrawErr != nil {
		return f.redrawErr
	}
	f.process()
	f.events = nil
	if !f.redraw {
		return fmt.Errorf("fake: no redraw: %w", device.ErrTimeout)
	}
	f.redraw = false
	return nil
}

func (f *fakeKB) NextEvent(time.Duration) (any, error) {
	if err := f.eventErr; err != nil {
		f.eventErr = nil
		return nil, err
	}
	if len(f.events) == 0 {
		return nil, device.ErrTimeout
	}
	ev := f.events[0]
	f.events = f.events[1:]
	return ev, nil
}

func (f *fakeKB) ReadButtons(p byte) (*proto.Table29, error) {
	f.record("read29 %d", p)
	f.process()
	return proto.ParseTable29(f.b29[p].Bytes())
}

func (f *fakeKB) ReadTypes(p byte) (*proto.Table25, error) {
	f.record("read25 %d", p)
	f.process()
	return proto.ParseTable25(f.b25[p].Bytes())
}

func (f *fakeKB) Upload(slot byte, data []byte) error {
	f.record("upload %d", slot)
	if len(data) != picture.Size {
		return fmt.Errorf("fake: upload of %d bytes", len(data))
	}
	if f.uploadErr != nil {
		if err := f.uploadErr(slot); err != nil {
			return err
		}
	}
	f.slots[slot] = append([]byte(nil), data...)
	return nil
}

func (f *fakeKB) WriteButtons(t *proto.Table29) error {
	f.record("write29 %d", t.Profile())
	if int(t.Profile()) != f.active {
		return fmt.Errorf("fake: button table of profile %d written while %d is active", t.Profile(), f.active)
	}
	c, err := proto.ParseTable29(t.Bytes())
	if err != nil {
		return err
	}
	f.b29[t.Profile()] = c
	return nil
}

// WriteType is the per-position type report (0x35), which apply no longer
// sends; the fake keeps it to show why. It names no profile and is queued.
func (f *fakeKB) WriteType(pos byte, te proto.TypeEntry) error {
	f.record("type %d", pos)
	f.queue = append(f.queue, func(p byte) error { return f.b25[p].SetType(int(pos), te) })
	return nil
}

// WriteTypes queues a whole type table; the types it holds go to the profile
// active when the fake takes it over.
func (f *fakeKB) WriteTypes(t *proto.Table25) error {
	f.record("write25 %d", t.Profile())
	c, err := proto.ParseTable25(t.Bytes())
	if err != nil {
		return err
	}
	f.queue = append(f.queue, func(p byte) error {
		for i := 0; i < 38; i++ {
			if err := f.b25[p].SetType(i, c.Type(i)); err != nil {
				return err
			}
		}
		f.redraw = true
		return nil
	})
	return nil
}

var blue = color.NRGBA{R: 0x00, G: 0xAA, B: 0xFF, A: 0xFF}

func btn(pos int, title string) layout.Button {
	return layout.Button{Page: 1, Position: pos, Title: title, Color: blue}
}

func prof(n byte, file, program string, bs ...layout.Button) layout.Profile {
	return layout.Profile{Number: n, File: filepath.Join("cfg", file), SwitchWhenRunning: program, Buttons: bs}
}

func cfgOf(ps ...layout.Profile) *layout.Config { return &layout.Config{Dir: "cfg", Profiles: ps} }

func own(p, pos int) ledger.Owner { return ledger.Owner{Profile: p, Page: 1, Position: pos} }

func nothingRuns(string) bool { return false }

func mustApply(t *testing.T, kb Keyboard, cfg *layout.Config, l *ledger.Ledger, full bool) Report {
	t.Helper()
	rep, err := Apply(context.Background(), kb, cfg, l, full, nothingRuns, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	return rep
}

func rendered(t *testing.T, title string) []byte {
	t.Helper()
	img, err := picture.Render(picture.Style{Color: blue, Title: title})
	if err != nil {
		t.Fatal(err)
	}
	data, err := picture.Encode(img)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sameOwners(t *testing.T, what string, got, want []ledger.Owner) {
	t.Helper()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// checkCalls compares the calls from call n on with want, leaving out the
// reads of the 0x29 tables that Apply only scans for launchers it does not
// manage: a 0x29 read not followed by the profile's 0x25 read.
// checkAllCalls keeps them.
func checkCalls(t *testing.T, f *fakeKB, from int, want string) {
	t.Helper()
	var kept []string
	calls := f.calls[from:]
	for i, c := range calls {
		if p, ok := strings.CutPrefix(c, "read29 "); ok && (i+1 == len(calls) || calls[i+1] != "read25 "+p) {
			continue
		}
		kept = append(kept, c)
	}
	if got := strings.Join(kept, ", "); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
}

func checkAllCalls(t *testing.T, f *fakeKB, from int, want string) {
	t.Helper()
	if got := f.since(from); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
}

func checkEntry(t *testing.T, f *fakeKB, p byte, pos int, want proto.Entry29) {
	t.Helper()
	if got := f.b29[p].Entry(pos); got != want {
		t.Errorf("profile %d position %d entry = %+v, want %+v", p, pos, got, want)
	}
}

func checkType(t *testing.T, f *fakeKB, p byte, pos int, want proto.TypeEntry) {
	t.Helper()
	if got := f.b25[p].Type(pos); got != want {
		t.Errorf("profile %d position %d type = %+v, want %+v", p, pos, got, want)
	}
}

// shows checks that a position is an application button whose slot holds the
// rendered picture with the given title, and returns the slot.
func shows(t *testing.T, f *fakeKB, p byte, pos int, title string) byte {
	t.Helper()
	e := f.b29[p].Entry(pos)
	if e != proto.AppEntry(e.Slot, "") || e.Slot == 0 {
		t.Fatalf("profile %d position %d entry = %+v, not an application button", p, pos, e)
	}
	if !bytes.Equal(f.slots[e.Slot], rendered(t, title)) {
		t.Fatalf("profile %d position %d shows slot %d, which does not hold %q", p, pos, e.Slot, title)
	}
	return e.Slot
}

func TestApplyOrder(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(
		prof(1, "sc.toml", "", btn(4, "Two"), btn(3, "One")),
		prof(0, "desktop.toml", "", btn(0, "Zero")),
	)
	rep := mustApply(t, f, cfg, l, false)

	checkCalls(t, f, 0, "notify, "+
		"fresh 0, read29 0, read25 0, switch 1, read29 1, read25 1, "+
		"switch 0, upload 1, write29 0, write25 0, redraw 0, "+
		"switch 1, upload 2, upload 3, write29 1, write25 1, redraw 1, "+
		"switch 0")
	checkEntry(t, f, 0, 0, proto.AppEntry(1, ""))
	checkEntry(t, f, 1, 3, proto.AppEntry(2, ""))
	checkEntry(t, f, 1, 4, proto.AppEntry(3, ""))
	for _, c := range []struct {
		p   byte
		pos int
	}{{0, 0}, {1, 3}, {1, 4}} {
		checkType(t, f, c.p, c.pos, proto.AppType)
	}
	if !bytes.Equal(f.slots[2], rendered(t, "One")) || !bytes.Equal(f.slots[3], rendered(t, "Two")) {
		t.Error("uploaded pictures are not the rendered buttons")
	}
	sameOwners(t, "Uploaded", rep.Uploaded, []ledger.Owner{own(0, 0), own(1, 3), own(1, 4)})
	sameOwners(t, "Skipped", rep.Skipped, nil)
	sameOwners(t, "Restored", rep.Restored, nil)
}

func TestSavePoints(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(
		prof(0, "desktop.toml", "", btn(0, "Zero")),
		prof(1, "sc.toml", "", btn(3, "One")),
	)
	if _, err := Apply(context.Background(), f, cfg, l, false, nothingRuns, f.save); err != nil {
		t.Fatal(err)
	}
	// The originals are saved before anything is overwritten, and the
	// slots after every profile.
	checkCalls(t, f, 0, "notify, "+
		"fresh 0, read29 0, read25 0, switch 1, read29 1, read25 1, save, "+
		"switch 0, upload 1, write29 0, write25 0, redraw 0, save, "+
		"switch 1, upload 2, write29 1, write25 1, redraw 1, save, "+
		"switch 0")
	n := len(f.calls)
	if _, err := Restore(context.Background(), f, l, 1, f.save); err != nil {
		t.Fatal(err)
	}
	checkCalls(t, f, n, "notify, fresh 1, read29 1, read25 1, write29 1, write25 1, redraw 1, save")
}

func TestSaveErrorStops(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One")))
	disk := errors.New("disk full")
	failing := func() error { f.record("save"); return disk }
	if _, err := Apply(context.Background(), f, cfg, l, false, nothingRuns, failing); !errors.Is(err, disk) {
		t.Fatalf("err = %v, want the save error", err)
	}
	checkCalls(t, f, 0, "notify, fresh 1, read29 1, read25 1, save")

	mustApply(t, f, cfg, l, false)
	n := len(f.calls)
	if _, err := Restore(context.Background(), f, l, 1, failing); !errors.Is(err, disk) {
		t.Fatalf("Restore err = %v, want the save error", err)
	}
	checkCalls(t, f, n, "notify, fresh 1, read29 1, read25 1, write29 1, write25 1, redraw 1, save")
}

func TestLabelIsWritten(t *testing.T) {
	f := newFake(t)
	b := btn(3, "One")
	b.Label = "Go"
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", b)), &ledger.Ledger{}, false)
	checkEntry(t, f, 1, 3, proto.AppEntry(1, "Go"))
}

func TestEnableNotificationsIsFirst(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"))), l, false)
	if len(f.calls) == 0 || f.calls[0] != "notify" {
		t.Fatalf("calls of Apply %v, want notify first", f.calls)
	}
	g := newFake(t)
	if _, err := Restore(context.Background(), g, l, 1, nil); err != nil {
		t.Fatal(err)
	}
	if len(g.calls) == 0 || g.calls[0] != "notify" {
		t.Fatalf("calls of Restore %v, want notify first", g.calls)
	}
}

func TestUnchangedSecondApply(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Two")))
	mustApply(t, f, cfg, l, false)
	n := len(f.calls)
	rep := mustApply(t, f, cfg, l, false)
	checkCalls(t, f, n, "notify, fresh 1, read29 1, read25 1")
	sameOwners(t, "Uploaded", rep.Uploaded, nil)
	sameOwners(t, "Skipped", rep.Skipped, []ledger.Owner{own(1, 3), own(1, 4)})
	if len(rep.Notes) != 0 {
		t.Errorf("notes: %q", rep.Notes)
	}
}

func TestFullUploadsEverything(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Two")))
	mustApply(t, f, cfg, l, false)
	n := len(f.calls)
	rep := mustApply(t, f, cfg, l, true)
	checkCalls(t, f, n, "notify, fresh 1, read29 1, read25 1, upload 1, upload 2, write25 1, redraw 1")
	sameOwners(t, "Uploaded", rep.Uploaded, []ledger.Owner{own(1, 3), own(1, 4)})
	// The ledger knows the pictures again afterwards.
	n = len(f.calls)
	mustApply(t, f, cfg, l, false)
	if got := f.since(n); strings.Contains(got, "upload") {
		t.Fatalf("apply after --full uploaded again: %s", got)
	}
}

func TestIdenticalPicturesShareSlot(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "Same"), btn(5, "Same"), btn(4, "Other")))
	rep := mustApply(t, f, cfg, l, false)
	checkCalls(t, f, 0, "notify, fresh 1, read29 1, read25 1, "+
		"upload 1, upload 2, write29 1, write25 1, redraw 1")
	checkEntry(t, f, 1, 3, proto.AppEntry(1, ""))
	checkEntry(t, f, 1, 4, proto.AppEntry(2, ""))
	checkEntry(t, f, 1, 5, proto.AppEntry(1, ""))
	sameOwners(t, "Uploaded", rep.Uploaded, []ledger.Owner{own(1, 3), own(1, 4)})
	sameOwners(t, "Skipped", rep.Skipped, []ledger.Owner{own(1, 5)})
}

func TestSlotSharedAcrossProfilesIsRedrawn(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(0, "desktop.toml", "", btn(0, "X")), prof(1, "sc.toml", "", btn(3, "X")))
	mustApply(t, f, cfg, l, false)
	checkEntry(t, f, 1, 3, proto.AppEntry(1, ""))
	n := len(f.calls)
	mustApply(t, f, cfg, l, true)
	// Slot 1 is sent once, in profile 0; profile 1 shows it too and is
	// redrawn.
	checkCalls(t, f, n, "notify, fresh 0, read29 0, read25 0, switch 1, read29 1, read25 1, "+
		"switch 0, upload 1, write25 0, redraw 0, switch 1, write25 1, redraw 1, switch 0")
}

// swarmEntry is what Swarm II leaves at a position it does not know.
var swarmEntry = proto.Entry29{Key: 0x01, Kind: 0x0b, Slot: 0x00}

func TestPositionChangedBySwarm(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Two")))
	mustApply(t, f, cfg, l, false)
	if err := f.b29[1].SetEntry(3, swarmEntry); err != nil {
		t.Fatal(err)
	}
	n := len(f.calls)
	rep := mustApply(t, f, cfg, l, false)
	checkCalls(t, f, n, "notify, fresh 1, read29 1, read25 1, upload 1, write29 1, write25 1, redraw 1")
	checkEntry(t, f, 1, 3, proto.AppEntry(1, ""))
	sameOwners(t, "Uploaded", rep.Uploaded, []ledger.Owner{own(1, 3)})
	sameOwners(t, "Skipped", rep.Skipped, []ledger.Owner{own(1, 4)})
	if len(rep.Notes) != 1 || !strings.Contains(rep.Notes[0], "position 3") {
		t.Errorf("notes: %q", rep.Notes)
	}
}

func TestPositionChangedBySwarmSharedSlot(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "Same"), btn(5, "Same")))
	mustApply(t, f, cfg, l, false)
	if err := f.b29[1].SetEntry(5, swarmEntry); err != nil {
		t.Fatal(err)
	}
	n := len(f.calls)
	mustApply(t, f, cfg, l, false)
	// The slot may have been reused, so it is sent again once, and both
	// positions that show it are redrawn.
	checkCalls(t, f, n, "notify, fresh 1, read29 1, read25 1, upload 1, write29 1, write25 1, redraw 1")
	checkEntry(t, f, 1, 5, proto.AppEntry(1, ""))
}

func TestSharedSlotIsNotTakenFromAnotherProfile(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	mustApply(t, f, cfgOf(prof(0, "desktop.toml", "", btn(0, "X")), prof(1, "sc.toml", "", btn(3, "X"))), l, false)
	if err := f.b29[1].SetEntry(3, swarmEntry); err != nil {
		t.Fatal(err)
	}
	n := len(f.calls)
	mustApply(t, f, cfgOf(prof(0, "desktop.toml", "", btn(0, "X")), prof(1, "sc.toml", "", btn(3, "Y"))), l, false)
	if x, y := shows(t, f, 0, 0, "X"), shows(t, f, 1, 3, "Y"); x == y {
		t.Fatalf("both positions show slot %d", x)
	}
	// Profile 0 keeps slot 1 and, as its content is no longer trusted, sends
	// it again; profile 1's new picture gets a slot of its own.
	checkCalls(t, f, n, "notify, fresh 0, read29 0, read25 0, switch 1, read29 1, read25 1, "+
		"switch 0, upload 1, write25 0, redraw 0, switch 1, upload 2, write29 1, write25 1, redraw 1, switch 0")
}

func TestFirstTakeoverRecordsOriginal(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One")))
	mustApply(t, f, cfg, l, false)
	want := ledger.Original{Entry: keyEntry(3), Type: keyType(3)}
	if got, ok := l.Original(own(1, 3)); !ok || got != want {
		t.Fatalf("Original = %+v, %v; want %+v", got, ok, want)
	}
	// A later apply over a changed position keeps the first original.
	if err := f.b29[1].SetEntry(3, swarmEntry); err != nil {
		t.Fatal(err)
	}
	mustApply(t, f, cfg, l, false)
	if got, _ := l.Original(own(1, 3)); got != want {
		t.Fatalf("Original after second apply = %+v, want %+v", got, want)
	}
}

func TestRemovedButtonGetsOriginalBack(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Two"))), l, false)
	n := len(f.calls)
	rep := mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"))), l, false)
	checkCalls(t, f, n, "notify, fresh 1, read29 1, read25 1, write29 1, write25 1, redraw 1")
	checkEntry(t, f, 1, 4, keyEntry(4))
	checkType(t, f, 1, 4, keyType(4))
	checkEntry(t, f, 1, 3, proto.AppEntry(1, ""))
	checkType(t, f, 1, 3, proto.AppType)
	sameOwners(t, "Restored", rep.Restored, []ledger.Owner{own(1, 4)})
	if _, ok := l.SlotOf(own(1, 4)); ok {
		t.Fatal("restored position still holds a slot")
	}
	if _, ok := l.Original(own(1, 4)); ok {
		t.Fatal("restored position still has a recorded original")
	}
	// The position is changed elsewhere and taken over again: the new
	// takeover records what it holds now. The freed slot still holds "Two",
	// so the new picture goes to a slot of unknown content.
	changed := proto.Entry29{Key: 0x30, Kind: 0x01, Label: "new"}
	if err := f.b29[1].SetEntry(4, changed); err != nil {
		t.Fatal(err)
	}
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Four"))), l, false)
	checkEntry(t, f, 1, 4, proto.AppEntry(3, ""))
	if got, _ := l.Original(own(1, 4)); got.Entry != changed {
		t.Fatalf("Original after a new takeover = %+v, want %+v", got.Entry, changed)
	}
}

func TestMovedButtonsAreNotUploaded(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Two"), btn(5, "Three"))), l, false)
	moved := cfgOf(prof(1, "sc.toml", "", btn(8, "One"), btn(9, "Two"), btn(5, "Three")))
	n := len(f.calls)
	rep := mustApply(t, f, moved, l, false)
	if got := f.since(n); strings.Contains(got, "upload") {
		t.Fatalf("moving buttons uploaded their pictures: %s", got)
	}
	sameOwners(t, "Restored", rep.Restored, []ledger.Owner{own(1, 3), own(1, 4)})
	sameOwners(t, "Skipped", rep.Skipped, []ledger.Owner{own(1, 5), own(1, 8), own(1, 9)})
	shows(t, f, 1, 8, "One")
	shows(t, f, 1, 9, "Two")
	checkEntry(t, f, 1, 3, keyEntry(3))

	// --full still sends every picture.
	n = len(f.calls)
	rep = mustApply(t, f, moved, l, true)
	if got := strings.Count(f.since(n), "upload"); got != 3 {
		t.Fatalf("--full sent %d pictures, want 3: %s", got, f.since(n))
	}
}

// buttons returns buttons at positions from..to-1 titled with prefix.
func buttons(prefix string, from, to int) []layout.Button {
	var bs []layout.Button
	for i := from; i < to; i++ {
		bs = append(bs, btn(i, fmt.Sprintf("%s%d", prefix, i)))
	}
	return bs
}

func TestRemovalAndAdditionInOneProfile(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	// Every slot is taken: 11 pictures on profile 1, the rest on profile 2.
	p2 := buttons("C", 0, ledger.MaxSlot-11)
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", buttons("B", 0, 11)...), prof(2, "two.toml", "", p2...)), l, false)
	// Profile 1 drops position 10 and gains position 11: the removed
	// button's slot must be free before the new one is assigned.
	next := append(buttons("B", 0, 10), btn(11, "New"))
	rep := mustApply(t, f, cfgOf(prof(1, "sc.toml", "", next...), prof(2, "two.toml", "", p2...)), l, false)
	sameOwners(t, "Restored", rep.Restored, []ledger.Owner{own(1, 10)})
	checkEntry(t, f, 1, 10, keyEntry(10))
	shows(t, f, 1, 11, "New")
}

func TestRemovalInHigherProfileFreesSlot(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	// One slot free: profile 1 holds one picture, profiles 2 and 3 the rest
	// but one.
	p2 := buttons("B", 0, 12)
	p3 := buttons("D", 0, ledger.MaxSlot-1-1-12)
	mustApply(t, f, cfgOf(prof(1, "a.toml", "", btn(0, "A")), prof(2, "b.toml", "", p2...), prof(3, "c.toml", "", p3...)), l, false)
	// Profile 1 gains two pictures and profile 2 drops one: all of profile
	// 2's removals happen before profile 1 needs the second slot.
	cfg := cfgOf(prof(1, "a.toml", "", btn(0, "A"), btn(1, "C"), btn(2, "E")), prof(2, "b.toml", "", p2[:len(p2)-1]...), prof(3, "c.toml", "", p3...))
	rep := mustApply(t, f, cfg, l, false)
	sameOwners(t, "Restored", rep.Restored, []ledger.Owner{own(2, len(p2)-1)})
	shows(t, f, 1, 1, "C")
	shows(t, f, 1, 2, "E")
	checkEntry(t, f, 2, len(p2)-1, keyEntry(len(p2)-1))
}

func TestRemovedProfileGetsOriginalsBack(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	mustApply(t, f, cfgOf(
		prof(0, "desktop.toml", "", btn(0, "Zero")),
		prof(2, "sc.toml", "", btn(3, "One")),
	), l, false)
	n := len(f.calls)
	rep := mustApply(t, f, cfgOf(prof(0, "desktop.toml", "", btn(0, "Zero"))), l, false)
	checkCalls(t, f, n, "notify, fresh 0, read29 0, read25 0, "+
		"switch 2, read29 2, read25 2, write29 2, write25 2, redraw 2, switch 0")
	checkEntry(t, f, 2, 3, keyEntry(3))
	checkType(t, f, 2, 3, keyType(3))
	sameOwners(t, "Restored", rep.Restored, []ledger.Owner{own(2, 3)})
}

func TestPositionsNotInLayoutUntouched(t *testing.T) {
	f := newFake(t)
	before := newFake(t)
	l := &ledger.Ledger{}
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(7, "Seven"))), l, false)
	for i := 0; i < 35; i++ {
		if i != 3 && i != 7 {
			checkEntry(t, f, 1, i, keyEntry(i))
		}
	}
	for i := 0; i < 38; i++ {
		if i != 3 && i != 7 {
			checkType(t, f, 1, i, keyType(i))
		}
	}
	for _, p := range []byte{0, 2, 3, 4} {
		if !bytes.Equal(f.b29[p].Bytes(), before.b29[p].Bytes()) || !bytes.Equal(f.b25[p].Bytes(), before.b25[p].Bytes()) {
			t.Errorf("profile %d changed", p)
		}
	}
}

func TestFinalProfile(t *testing.T) {
	desktopAndGame := func() *layout.Config {
		return cfgOf(
			prof(0, "desktop.toml", "", btn(0, "Zero")),
			prof(2, "sc.toml", "StarCitizen.exe", btn(3, "One")),
		)
	}
	cases := []struct {
		name    string
		cfg     *layout.Config
		running func(string) bool
		want    int
	}{
		{"game running", desktopAndGame(), func(n string) bool { return n == "StarCitizen.exe" }, 2},
		{"game not running", desktopAndGame(), nothingRuns, 0},
		{"no running check", desktopAndGame(), nil, 0},
		{"game only, not running", cfgOf(prof(1, "sc.toml", "StarCitizen.exe", btn(3, "One"))), nothingRuns, 0},
		{"desktop is not the lowest", cfgOf(
			prof(0, "sc.toml", "StarCitizen.exe", btn(3, "One")),
			prof(1, "desktop.toml", "", btn(0, "Zero")),
		), nothingRuns, 1},
		{"first running profile wins", cfgOf(
			prof(3, "b.toml", "B.exe", btn(0, "Zero")),
			prof(1, "a.toml", "A.exe", btn(3, "One")),
			prof(0, "desktop.toml", "", btn(4, "Four")),
		), func(string) bool { return true }, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			if _, err := Apply(context.Background(), f, c.cfg, &ledger.Ledger{}, false, c.running, nil); err != nil {
				t.Fatal(err)
			}
			if f.active != c.want {
				t.Fatalf("profile %d is active, want %d; calls %v", f.active, c.want, f.calls)
			}
		})
	}
}

func TestOneSwitchForOneProfile(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Two")))
	for _, c := range []struct {
		name string
		full bool
	}{{"first apply", false}, {"unchanged apply", false}, {"full apply", true}} {
		n := len(f.calls)
		if _, err := Apply(context.Background(), f, cfg, l, c.full, nothingRuns, nil); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := f.switches(n); got != 1 {
			t.Errorf("%s: %d profile switches, want 1: %s", c.name, got, f.since(n))
		}
	}
	n := len(f.calls)
	if _, err := Restore(context.Background(), f, l, 1, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.switches(n); got != 1 {
		t.Errorf("restore: %d profile switches, want 1: %s", got, f.since(n))
	}
}

func TestSwitchAgainAfterAProfileChange(t *testing.T) {
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One")))
	cases := []struct {
		name  string
		input func(f *fakeKB)
		want  string
	}{
		{"changed on the keyboard", func(f *fakeKB) { f.userSwitch(0) },
			"notify, fresh 1, read29 1, read25 1, switch 1, upload 1, write29 1, write25 1, redraw 1"},
		{"changed there and back", func(f *fakeKB) { f.userSwitch(3); f.userSwitch(1) },
			"notify, fresh 1, read29 1, read25 1, upload 1, write29 1, write25 1, redraw 1"},
		{"other input only", func(f *fakeKB) { f.events = append(f.events, proto.Redraw{View: 0x20}) },
			"notify, fresh 1, read29 1, read25 1, upload 1, write29 1, write25 1, redraw 1"},
		{"input unreadable", func(f *fakeKB) { f.eventErr = errors.New("read failed") },
			"notify, fresh 1, read29 1, read25 1, fresh 1, upload 1, write29 1, write25 1, redraw 1"},
		{"too much input", func(f *fakeKB) {
			for range maxEvents {
				f.events = append(f.events, proto.Redraw{View: 0x20})
			}
		}, "notify, fresh 1, read29 1, read25 1, fresh 1, upload 1, write29 1, write25 1, redraw 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			f.hook = func(call string) {
				if call == "read25 1" {
					c.input(f)
				}
			}
			if _, err := Apply(context.Background(), f, cfg, &ledger.Ledger{}, false, nothingRuns, nil); err != nil {
				t.Fatal(err)
			}
			checkCalls(t, f, 0, c.want)
			shows(t, f, 1, 3, "One")
		})
	}
}

func TestRenderFailsBeforeKeyboard(t *testing.T) {
	dir := t.TempDir()
	notPNG := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(notPNG, []byte("not a png"), 0o644); err != nil {
		t.Fatal(err)
	}
	long := btn(7, "A title far too long to fit on one button at any size")
	pic := btn(6, "")
	pic.Picture = notPNG
	sym := btn(8, "Sym")
	sym.Symbol = notPNG
	cases := []struct {
		name   string
		b      layout.Button
		target error
		where  string
	}{
		{"title too long", long, picture.ErrTitleTooLong, "sc.toml: page 1 position 7"},
		{"broken picture", pic, nil, "sc.toml: page 1 position 6"},
		{"broken symbol", sym, nil, "sc.toml: page 1 position 8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			l := &ledger.Ledger{}
			cfg := cfgOf(
				prof(1, "desktop.toml", "", btn(3, "One")),
				prof(2, "sc.toml", "", c.b),
			)
			_, err := Apply(context.Background(), f, cfg, l, false, nothingRuns, f.save)
			if err == nil {
				t.Fatal("no error")
			}
			if c.target != nil && !errors.Is(err, c.target) {
				t.Errorf("error %v is not %v", err, c.target)
			}
			if !strings.Contains(err.Error(), c.where) {
				t.Errorf("error %q does not name %q", err, c.where)
			}
			if len(f.calls) != 0 {
				t.Errorf("keyboard calls before the failure: %v", f.calls)
			}
			if len(l.Owners(1)) != 0 {
				t.Error("ledger changed")
			}
		})
	}
}

func TestFailedRenderKeepsLedger(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	good := prof(1, "sc.toml", "", btn(3, "One"))
	mustApply(t, f, cfgOf(good), l, false)
	bad := prof(2, "other.toml", "", btn(4, "A title far too long to fit on one button at any size"))
	if _, err := Apply(context.Background(), f, cfgOf(good, bad), l, true, nothingRuns, nil); err == nil {
		t.Fatal("no error")
	}
	n := len(f.calls)
	mustApply(t, f, cfgOf(good), l, false)
	if got := f.since(n); strings.Contains(got, "upload") {
		t.Fatalf("a failed --full apply made the ledger forget its pictures: %s", got)
	}
}

func TestTooManyPicturesFailBeforeKeyboard(t *testing.T) {
	f := newFake(t)
	var bs []layout.Button
	for i := 0; i <= ledger.MaxSlot; i++ {
		bs = append(bs, btn(i, fmt.Sprintf("B%d", i)))
	}
	_, err := Apply(context.Background(), f, cfgOf(prof(1, "sc.toml", "", bs...)), &ledger.Ledger{}, false, nothingRuns, nil)
	if !errors.Is(err, ledger.ErrNoSlot) {
		t.Fatalf("err = %v, want ErrNoSlot", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("keyboard calls: %v", f.calls)
	}
}

func TestUploadLeftOpenStops(t *testing.T) {
	f := newFake(t)
	f.uploadErr = func(slot byte) error {
		if slot == 2 {
			return fmt.Errorf("%w (frame 7 of 1065: timeout)", keyboard.ErrUploadLeftOpen)
		}
		return nil
	}
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Two"), btn(5, "Three")))
	rep, err := Apply(context.Background(), f, cfg, l, false, nothingRuns, nil)
	if !errors.Is(err, keyboard.ErrUploadLeftOpen) {
		t.Fatalf("err = %v, want ErrUploadLeftOpen", err)
	}
	if !strings.Contains(err.Error(), "unplug") || !strings.Contains(err.Error(), "position 4") {
		t.Errorf("error %q lost the advice or the position", err)
	}
	checkCalls(t, f, 0, "notify, fresh 1, read29 1, read25 1, upload 1, upload 2")
	// The position stays managed, and its slot is not trusted.
	if s, ok := l.SlotOf(own(1, 4)); !ok || s != 2 {
		t.Errorf("SlotOf(position 4) = %d, %v; want 2, true", s, ok)
	}
	if s, ok := l.SlotOf(own(1, 3)); !ok || s != 1 {
		t.Errorf("SlotOf(position 3) = %d, %v; want 1, true", s, ok)
	}
	if _, ok := l.SlotOf(own(1, 5)); ok {
		t.Error("a button after the failure got a slot")
	}
	sameOwners(t, "Uploaded", rep.Uploaded, []ledger.Owner{own(1, 3)})

	// The next apply sends the picture of the failed upload.
	f.uploadErr = nil
	n := len(f.calls)
	mustApply(t, f, cfg, l, false)
	if got := f.since(n); !strings.Contains(got, "upload 2") {
		t.Fatalf("failed picture not sent again: %s", got)
	}
	shows(t, f, 1, 3, "One")
	shows(t, f, 1, 4, "Two")
	shows(t, f, 1, 5, "Three")
}

func TestFailedUploadKeepsPositionManaged(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Two"))), l, false)
	f.uploadErr = func(byte) error { return keyboard.ErrOpenerRefused }
	_, err := Apply(context.Background(), f, cfgOf(prof(1, "sc.toml", "", btn(3, "Uno"), btn(4, "Two"))), l, false, nothingRuns, nil)
	if !errors.Is(err, keyboard.ErrOpenerRefused) {
		t.Fatalf("err = %v, want ErrOpenerRefused", err)
	}
	// The keyboard still shows slot 1 at position 3; once the button is
	// removed, the position gets its original back.
	f.uploadErr = nil
	rep := mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(4, "Two"))), l, false)
	sameOwners(t, "Restored", rep.Restored, []ledger.Owner{own(1, 3)})
	checkEntry(t, f, 1, 3, keyEntry(3))
	checkType(t, f, 1, 3, keyType(3))
}

// A picture uploaded before a later upload fails was not written to its
// position; the next apply sends it again and redraws it.
func TestErrorAfterAnUploadDistrustsUploadedSlots(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Two"))), l, false)
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "Uno"), btn(4, "Dos")))
	uploads := 0
	f.uploadErr = func(byte) error {
		uploads++
		if uploads == 2 {
			return keyboard.ErrOpenerRefused
		}
		return nil
	}
	if _, err := Apply(context.Background(), f, cfg, l, false, nothingRuns, nil); !errors.Is(err, keyboard.ErrOpenerRefused) {
		t.Fatalf("err = %v, want ErrOpenerRefused", err)
	}
	f.uploadErr = nil
	n := len(f.calls)
	mustApply(t, f, cfg, l, false)
	got := f.since(n)
	if strings.Count(got, "upload") != 2 || !strings.Contains(got, "write25 1, redraw 1") {
		t.Fatalf("second apply: %s, want both pictures uploaded and both positions redrawn", got)
	}
}

func TestFailedUploadIntoSharedSlotIsNotTrusted(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "Same"), btn(5, "Same")))
	mustApply(t, f, cfg, l, false)
	f.uploadErr = func(byte) error { return keyboard.ErrOpenerRefused }
	if _, err := Apply(context.Background(), f, cfg, l, true, nothingRuns, nil); !errors.Is(err, keyboard.ErrOpenerRefused) {
		t.Fatalf("err = %v, want ErrOpenerRefused", err)
	}
	// Slot 1's content is unknown now: the next apply sends it again.
	f.uploadErr = nil
	n := len(f.calls)
	mustApply(t, f, cfg, l, false)
	if got := f.since(n); !strings.Contains(got, "upload") {
		t.Fatalf("slot of a failed upload trusted: %s", got)
	}
}

func TestSwitchFailureBeforeUploadIsNotTrusted(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One")))
	mustApply(t, f, cfg, l, false)
	timeout := errors.New("no profile change")
	switches := 0
	f.switchErr = func(byte) error {
		if switches++; switches == 2 {
			return timeout
		}
		return nil
	}
	// The profile is changed on the keyboard after the first pass, so the
	// second pass switches again, and that switch fails.
	f.hook = func(call string) {
		if call == "read25 1" {
			f.userSwitch(0)
		}
	}
	n := len(f.calls)
	if _, err := Apply(context.Background(), f, cfg, l, true, nothingRuns, nil); !errors.Is(err, timeout) {
		t.Fatalf("err = %v, want the switch error", err)
	}
	checkCalls(t, f, n, "notify, fresh 1, read29 1, read25 1, switch 1")
	// Slot 1 was to be sent again but was not, while position 3 shows it:
	// the next apply sends it.
	f.switchErr, f.hook = nil, nil
	n = len(f.calls)
	mustApply(t, f, cfg, l, false)
	if got := f.since(n); !strings.Contains(got, "upload 1") {
		t.Fatalf("unfilled slot trusted: %s", got)
	}
	shows(t, f, 1, 3, "One")
}

func TestPictureAndSymbolFiles(t *testing.T) {
	dir := t.TempDir()
	writePNG := func(name string, img image.Image) string {
		p := filepath.Join(dir, name)
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	readPNG := func(p string) image.Image {
		r, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		img, err := png.Decode(r)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	pic := image.NewNRGBA(image.Rect(0, 0, 50, 20))
	sym := image.NewNRGBA(image.Rect(0, 0, 30, 30))
	for i := 0; i < len(pic.Pix); i += 4 {
		pic.Pix[i], pic.Pix[i+3] = 0xff, 0xc0
	}
	for y := 10; y < 20; y++ {
		for x := 0; x < 30; x++ {
			sym.SetNRGBA(x, y, color.NRGBA{0xff, 0xff, 0xff, 0xff})
		}
	}
	picPath, symPath := writePNG("pic.png", pic), writePNG("sym.png", sym)

	a := btn(3, "")
	a.Picture = picPath
	b := btn(4, "Sym")
	b.Symbol = symPath
	f := newFake(t)
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", a, b)), &ledger.Ledger{}, false)

	wantPic, err := picture.Encode(picture.Fit(readPNG(picPath)))
	if err != nil {
		t.Fatal(err)
	}
	img, err := picture.Render(picture.Style{Color: blue, Symbol: readPNG(symPath), Title: "Sym"})
	if err != nil {
		t.Fatal(err)
	}
	wantSym, err := picture.Encode(img)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.slots[1], wantPic) {
		t.Error("picture file not uploaded as fitted")
	}
	if !bytes.Equal(f.slots[2], wantSym) {
		t.Error("symbol file not rendered into the built-in style")
	}
}

// The per-position type report names no profile and is queued: a switch
// sent right after it moves the change to the other profile. This is what
// the keyboard did with apply's earlier sequence.
func TestQueuedTypeWriteFollowsTheSwitch(t *testing.T) {
	f := newFake(t)
	f.notified = true
	if err := f.SwitchProfile(1); err != nil {
		t.Fatal(err)
	}
	if err := f.WriteType(3, proto.AppType); err != nil {
		t.Fatal(err)
	}
	if err := f.SwitchProfile(0); err != nil {
		t.Fatal(err)
	}
	checkType(t, f, 0, 3, proto.AppType)
	checkType(t, f, 1, 3, keyType(3))
}

// Apply writes each profile's type table and waits for the redraw before
// anything else is sent, so every change stays on its own profile.
func TestTypeTableIsTakenOverBeforeTheNextSwitch(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	mustApply(t, f, cfgOf(prof(0, "desktop.toml", "", btn(0, "Zero")), prof(2, "sc.toml", "", btn(3, "One"), btn(4, "Two"))), l, false)
	// Profile 2 loses a button and profile 1 gains one: a restore in the
	// first pass and two type tables in the second.
	cfg := cfgOf(prof(0, "desktop.toml", "", btn(0, "Zero")), prof(1, "a.toml", "", btn(5, "Five")), prof(2, "sc.toml", "", btn(3, "One")))
	n := len(f.calls)
	mustApply(t, f, cfg, l, false)
	calls := f.calls[n:]
	for i, c := range calls {
		if p, ok := strings.CutPrefix(c, "write25 "); ok {
			if i+1 >= len(calls) || calls[i+1] != "redraw "+p {
				t.Fatalf("%s is not followed by the redraw wait: %s", c, f.since(n))
			}
		}
	}
	if !strings.Contains(f.since(n), "write25 2, redraw 2") || !strings.Contains(f.since(n), "write25 1, redraw 1") {
		t.Fatalf("calls: %s", f.since(n))
	}
	if strings.Contains(f.since(n), "type ") {
		t.Fatalf("a per-position type report was sent: %s", f.since(n))
	}
	checkType(t, f, 0, 0, proto.AppType)
	checkType(t, f, 0, 5, keyType(5))
	checkType(t, f, 1, 5, proto.AppType)
	checkType(t, f, 1, 0, keyType(0))
	checkType(t, f, 2, 3, proto.AppType)
	checkType(t, f, 2, 4, keyType(4))
	checkType(t, f, 2, 5, keyType(5))
	shows(t, f, 1, 5, "Five")
}

func TestRedrawTimeoutStopsBeforeTheNextSwitch(t *testing.T) {
	f := newFake(t)
	f.redrawErr = fmt.Errorf("keyboard: waiting for the redraw request: %w", device.ErrTimeout)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(0, "desktop.toml", "", btn(0, "Zero")), prof(1, "sc.toml", "", btn(3, "One")))
	_, err := Apply(context.Background(), f, cfg, l, false, nothingRuns, nil)
	if !errors.Is(err, device.ErrTimeout) || !strings.Contains(err.Error(), "profile 0") {
		t.Fatalf("err = %v, want the redraw timeout of profile 0", err)
	}
	checkCalls(t, f, 0, "notify, fresh 0, read29 0, read25 0, switch 1, read29 1, read25 1, "+
		"switch 0, upload 1, write29 0, write25 0, redraw 0")
	// The next apply writes the tables again.
	f.redrawErr = nil
	f.process()
	n := len(f.calls)
	mustApply(t, f, cfg, l, false)
	if got := f.since(n); !strings.Contains(got, "write25 0, redraw 0") {
		t.Fatalf("type table of profile 0 not written again: %s", got)
	}
	shows(t, f, 0, 0, "Zero")
	shows(t, f, 1, 3, "One")
}

// The first switch goes through another profile, so it is confirmed when the
// keyboard is already on the profile.
func TestFirstSwitchToTheActiveProfile(t *testing.T) {
	f := newFake(t)
	f.active = 1
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"))), &ledger.Ledger{}, false)
	shows(t, f, 1, 3, "One")
	if _, err := Restore(context.Background(), f, &ledger.Ledger{}, 1, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRedrawTimeoutKeepsPositionsManaged(t *testing.T) {
	f, l, _ := restoreSetup(t, "Home")
	f.redrawErr = fmt.Errorf("keyboard: waiting for the redraw request: %w", device.ErrTimeout)
	if _, err := Restore(context.Background(), f, l, 1, nil); !errors.Is(err, device.ErrTimeout) {
		t.Fatalf("err = %v, want the redraw timeout", err)
	}
	checkCalls(t, f, 0, "notify, fresh 1, read29 1, read25 1, write29 1, write25 1, redraw 1")
	sameOwners(t, "Owners(1)", l.Owners(1), []ledger.Owner{own(1, 3), own(1, 4)})
}

func TestKeyboardErrorStops(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	// Without notifications the fake refuses the switch, as the keyboard
	// would not report it.
	k := &noNotify{f}
	_, err := Apply(context.Background(), k, cfgOf(prof(1, "sc.toml", "", btn(3, "One"))), l, false, nothingRuns, nil)
	if err == nil || !strings.Contains(err.Error(), "profile 1") {
		t.Fatalf("err = %v", err)
	}
	checkCalls(t, f, 0, "fresh 1")
}

// noNotify drops EnableNotifications, so the following switch fails.
type noNotify struct{ *fakeKB }

func (noNotify) EnableNotifications() error { return nil }

// restoreSetup gives profile 1 two managed positions as an apply would leave
// them (3 with an original, 4 without) and profile 2 one.
func restoreSetup(t *testing.T, label string) (*fakeKB, *ledger.Ledger, ledger.Original) {
	f := newFake(t)
	l := &ledger.Ledger{}
	orig := ledger.Original{Entry: proto.Entry29{Key: 0x4a, Kind: 0x01, Label: label}, Type: proto.TypeEntry{Type: 0x3b, Value: 0x02}}
	for i, o := range []ledger.Owner{own(1, 3), own(1, 4), own(2, 0)} {
		slot, _, err := l.Assign(o, fmt.Sprintf("h%d", i))
		if err != nil {
			t.Fatal(err)
		}
		if err := f.b29[byte(o.Profile)].SetEntry(o.Position, proto.AppEntry(slot, "")); err != nil {
			t.Fatal(err)
		}
		if err := f.b25[byte(o.Profile)].SetType(o.Position, proto.AppType); err != nil {
			t.Fatal(err)
		}
	}
	l.RecordOriginal(own(1, 3), orig)
	l.RecordOriginal(own(2, 0), orig)
	return f, l, orig
}

func TestRestore(t *testing.T) {
	f, l, orig := restoreSetup(t, "Home")
	before29, before25 := f.b29[2].Bytes(), f.b25[2].Bytes()
	rep, err := Restore(context.Background(), f, l, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	checkCalls(t, f, 0, "notify, fresh 1, read29 1, read25 1, write29 1, write25 1, redraw 1")
	checkEntry(t, f, 1, 3, orig.Entry)
	checkType(t, f, 1, 3, orig.Type)
	checkEntry(t, f, 1, 4, proto.Entry29{})
	checkType(t, f, 1, 4, proto.TypeEntry{})
	for i := 0; i < 35; i++ {
		if i != 3 && i != 4 {
			checkEntry(t, f, 1, i, keyEntry(i))
		}
	}
	for i := 0; i < 38; i++ {
		if i != 3 && i != 4 {
			checkType(t, f, 1, i, keyType(i))
		}
	}
	if !bytes.Equal(f.b29[2].Bytes(), before29) || !bytes.Equal(f.b25[2].Bytes(), before25) {
		t.Error("profile 2 changed")
	}
	sameOwners(t, "Restored", rep.Restored, []ledger.Owner{own(1, 3), own(1, 4)})
	sameOwners(t, "Owners(1)", l.Owners(1), nil)
	sameOwners(t, "Owners(2)", l.Owners(2), []ledger.Owner{own(2, 0)})
	if _, ok := l.Original(own(1, 3)); ok {
		t.Error("restored position still has a recorded original")
	}
	if _, ok := l.Original(own(2, 0)); !ok {
		t.Error("original of another profile dropped")
	}
	if len(rep.Notes) != 0 {
		t.Errorf("notes: %q", rep.Notes)
	}
}

func TestRestoreLabelFallback(t *testing.T) {
	f, l, orig := restoreSetup(t, "bad\x01label")
	rep, err := Restore(context.Background(), f, l, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := orig.Entry
	want.Label = ""
	checkEntry(t, f, 1, 3, want)
	checkType(t, f, 1, 3, orig.Type)
	if len(rep.Notes) != 1 || !strings.Contains(rep.Notes[0], "position 3") {
		t.Fatalf("notes: %q", rep.Notes)
	}
}

func TestRestoreWithoutOwners(t *testing.T) {
	f := newFake(t)
	rep, err := Restore(context.Background(), f, &ledger.Ledger{}, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("keyboard calls: %v", f.calls)
	}
	if len(rep.Notes) != 1 {
		t.Fatalf("notes: %q", rep.Notes)
	}
}

func TestRestoreRefusesUnknownProfile(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	if _, _, err := l.Assign(ledger.Owner{Profile: 5, Page: 1, Position: 3}, "h"); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), f, l, 5, nil); err == nil {
		t.Fatal("profile 5 restored")
	}
	if len(f.calls) != 0 {
		t.Fatalf("keyboard calls: %v", f.calls)
	}
}

func TestRestoreRefusesForeignPosition(t *testing.T) {
	f := newFake(t)
	for _, o := range []ledger.Owner{{Profile: 1, Page: 4, Position: 3}, {Profile: 1, Page: 3, Position: 11}} {
		l := &ledger.Ledger{}
		if _, _, err := l.Assign(o, "h"); err != nil {
			t.Fatal(err)
		}
		if _, err := Restore(context.Background(), f, l, 1, nil); err == nil {
			t.Fatalf("%v was restored", o)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("keyboard calls: %v", f.calls)
	}
}

// TestPagesTwoAndThree: a button on page 2 position 4 is 0x29 entry 16 and
// 0x25 entry 17, next to page 1 position 3 at entries 3 and 3; restore puts
// them back.
func TestPagesTwoAndThree(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	p2 := layout.Button{Page: 2, Position: 4, Title: "Two", Color: blue}
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"), p2)), l, false)
	o2 := ledger.Owner{Profile: 1, Page: 2, Position: 4}
	shows(t, f, 1, 3, "One")
	shows(t, f, 1, 16, "Two")
	checkType(t, f, 1, 3, proto.AppType)
	checkType(t, f, 1, 17, proto.AppType)
	for i := 0; i < 35; i++ {
		if i != 3 && i != 16 {
			checkEntry(t, f, 1, i, keyEntry(i))
		}
	}
	for i := 0; i < 38; i++ {
		if i != 3 && i != 17 {
			checkType(t, f, 1, i, keyType(i))
		}
	}
	want := ledger.Original{Entry: keyEntry(16), Type: keyType(17)}
	if got, ok := l.Original(o2); !ok || got != want {
		t.Fatalf("Original = %+v, %v; want %+v", got, ok, want)
	}
	rep, err := Restore(context.Background(), f, l, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	sameOwners(t, "Restored", rep.Restored, []ledger.Owner{own(1, 3), o2})
	for i := 0; i < 35; i++ {
		checkEntry(t, f, 1, i, keyEntry(i))
	}
	for i := 0; i < 38; i++ {
		checkType(t, f, 1, i, keyType(i))
	}
}

// cancelAt returns a context that the fake cancels as soon as it records
// the given call.
func cancelAt(f *fakeKB, call string) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	f.t.Cleanup(cancel)
	f.hook = func(c string) {
		if c == call {
			cancel()
		}
	}
	return ctx
}

func wantStopped(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrStopped) || !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want ErrStopped carrying the cancellation", err)
	}
}

func TestStopAfterTheFirstPicture(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Two"), btn(5, "Three")))
	rep, err := Apply(cancelAt(f, "upload 1"), f, cfg, l, false, nothingRuns, f.save)
	wantStopped(t, err)
	// The picture being sent is finished, no other is started, and the
	// position that shows it is written.
	checkCalls(t, f, 0, "notify, fresh 1, read29 1, read25 1, save, upload 1, write29 1, write25 1, redraw 1")
	shows(t, f, 1, 3, "One")
	for _, pos := range []int{4, 5} {
		checkEntry(t, f, 1, pos, keyEntry(pos))
		checkType(t, f, 1, pos, keyType(pos))
		if _, ok := l.SlotOf(own(1, pos)); ok {
			t.Errorf("position %d, not reached, holds a slot", pos)
		}
	}
	sameOwners(t, "Uploaded", rep.Uploaded, []ledger.Owner{own(1, 3)})

	// The ledger knows what is on the keyboard: the next apply sends only
	// the pictures that were not sent.
	f.hook = nil
	n := len(f.calls)
	rep = mustApply(t, f, cfg, l, false)
	checkCalls(t, f, n, "notify, fresh 1, read29 1, read25 1, upload 2, upload 3, write29 1, write25 1, redraw 1")
	sameOwners(t, "Skipped", rep.Skipped, []ledger.Owner{own(1, 3)})
	shows(t, f, 1, 3, "One")
	shows(t, f, 1, 4, "Two")
	shows(t, f, 1, 5, "Three")
}

func TestStopBetweenProfiles(t *testing.T) {
	cfg := cfgOf(prof(0, "desktop.toml", "", btn(0, "Zero")), prof(1, "sc.toml", "", btn(3, "One")))
	cases := []struct {
		name, at, want string
	}{
		{"first pass", "read25 0", "notify, fresh 0, read29 0, read25 0"},
		{"second pass", "redraw 0", "notify, fresh 0, read29 0, read25 0, switch 1, read29 1, read25 1, " +
			"switch 0, upload 1, write29 0, write25 0, redraw 0"},
		{"before the final switch", "redraw 1", "notify, fresh 0, read29 0, read25 0, switch 1, read29 1, read25 1, " +
			"switch 0, upload 1, write29 0, write25 0, redraw 0, switch 1, upload 2, write29 1, write25 1, redraw 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			l := &ledger.Ledger{}
			_, err := Apply(cancelAt(f, c.at), f, cfg, l, false, nothingRuns, nil)
			wantStopped(t, err)
			checkCalls(t, f, 0, c.want)
			// The next apply finishes the work.
			f.hook = nil
			mustApply(t, f, cfg, l, false)
			shows(t, f, 0, 0, "Zero")
			shows(t, f, 1, 3, "One")
			if f.active != 0 {
				t.Errorf("profile %d is active, want 0", f.active)
			}
		})
	}
}

func TestStopBeforeTheKeyboard(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One")))
	mustApply(t, f, cfg, l, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n := len(f.calls)
	_, err := Apply(ctx, f, cfg, l, true, nothingRuns, f.save)
	wantStopped(t, err)
	if got := f.since(n); got != "" {
		t.Fatalf("calls after the stop: %s", got)
	}
	// A stopped --full apply has not made the ledger forget the pictures.
	n = len(f.calls)
	mustApply(t, f, cfg, l, false)
	if got := f.since(n); strings.Contains(got, "upload") {
		t.Fatalf("pictures sent again: %s", got)
	}
}

// A slot sent before the stop that a position not yet written shows is sent
// again by the next apply, which redraws every position that shows it.
func TestStopDistrustsSlotsOfPendingPositions(t *testing.T) {
	cases := []struct {
		name string
		cfg  *layout.Config
		at   string
		want string
	}{
		{"same profile", cfgOf(prof(1, "sc.toml", "", btn(3, "Same"), btn(5, "Same"))), "upload 1",
			"notify, fresh 1, read29 1, read25 1, upload 1, write25 1, redraw 1"},
		{"later profile", cfgOf(prof(0, "desktop.toml", "", btn(0, "X")), prof(1, "sc.toml", "", btn(3, "X"))), "redraw 0",
			"notify, fresh 0, read29 0, read25 0, switch 1, read29 1, read25 1, " +
				"switch 0, upload 1, write25 0, redraw 0, switch 1, write25 1, redraw 1, switch 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			l := &ledger.Ledger{}
			mustApply(t, f, c.cfg, l, false)
			_, err := Apply(cancelAt(f, c.at), f, c.cfg, l, true, nothingRuns, nil)
			wantStopped(t, err)
			f.hook = nil
			n := len(f.calls)
			mustApply(t, f, c.cfg, l, false)
			checkCalls(t, f, n, c.want)
		})
	}
}

func TestRestoreStopsBeforeTheKeyboard(t *testing.T) {
	f, l, _ := restoreSetup(t, "Home")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Restore(ctx, f, l, 1, nil)
	wantStopped(t, err)
	if len(f.calls) != 0 {
		t.Fatalf("keyboard calls: %v", f.calls)
	}
	sameOwners(t, "Owners(1)", l.Owners(1), []ledger.Owner{own(1, 3), own(1, 4)})
}

// foreign puts an application button kb7screen does not manage on entry i of
// profile p, showing slot n.
func foreign(t *testing.T, f *fakeKB, p byte, i int, n byte) {
	t.Helper()
	if err := f.b29[p].SetEntry(i, proto.AppEntry(n, "app")); err != nil {
		t.Fatal(err)
	}
}

func TestSlotsOfForeignLaunchersAreLeftAlone(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	foreign(t, f, 3, 2, 1)  // another profile, page 1
	foreign(t, f, 1, 16, 2) // the applied profile, page 2 position 4
	foreign(t, f, 1, 7, 0)  // slot 0 is the firmware's own icon
	rep := mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"))), l, false)
	checkAllCalls(t, f, 0, "notify, fresh 1, read29 1, read25 1, read29 0, read29 2, read29 3, read29 4, "+
		"upload 3, write29 1, write25 1, redraw 1")
	checkEntry(t, f, 1, 3, proto.AppEntry(3, ""))
	want := []string{
		"slot 1 is shown by profile 3 page 1 position 2, which kb7screen does not manage; it is left alone",
		"slot 2 is shown by profile 1 page 2 position 4, which kb7screen does not manage; it is left alone",
	}
	if fmt.Sprint(rep.Notes) != fmt.Sprint(want) {
		t.Errorf("notes:\n got %q\nwant %q", rep.Notes, want)
	}
}

func TestOwnSlotMovesAwayFromAForeignLauncher(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One")))
	mustApply(t, f, cfg, l, false)
	foreign(t, f, 2, 0, 1) // Swarm II put a launcher on our slot
	n := len(f.calls)
	mustApply(t, f, cfg, l, false)
	if got := f.since(n); !strings.Contains(got, "upload 2") || strings.Contains(got, "upload 1") {
		t.Fatalf("calls: %s", got)
	}
	shows(t, f, 1, 3, "One")
}

func TestTooFewSlotsBesideForeignLaunchers(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	for n := byte(1); n < ledger.MaxSlot; n++ {
		foreign(t, f, 2, int(n-1), n)
	}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(4, "Two")))
	_, err := Apply(context.Background(), f, cfg, l, false, nothingRuns, nil)
	if !errors.Is(err, ledger.ErrNoSlot) {
		t.Fatalf("err = %v, want ErrNoSlot", err)
	}
	for _, s := range []string{"profile 2 page 1 position 0", "profile 2 page 2 position 1", "kb7screen clear-launchers"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not name %q", err, s)
		}
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "upload") || strings.HasPrefix(c, "write") {
			t.Fatalf("wrote although the pictures do not fit: %q", f.calls)
		}
	}
}
