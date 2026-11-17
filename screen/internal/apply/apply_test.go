package apply

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Bananenbaas/kb7-commander/screen/internal/keyboard"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/ledger"
	"github.com/Bananenbaas/kb7-commander/screen/internal/picture"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// fakeKB models the keyboard: a 0x29 and a 0x25 table per profile, the
// picture slots and the active profile. Writes go to the active profile, and
// every call is recorded.
type fakeKB struct {
	t         *testing.T
	b29       map[byte]*proto.Table29
	b25       map[byte]*proto.Table25
	slots     map[byte][]byte
	active    int // -1 until the first switch
	notified  bool
	calls     []string
	uploadErr func(slot byte) error
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
	f := &fakeKB{t: t, b29: map[byte]*proto.Table29{}, b25: map[byte]*proto.Table25{}, slots: map[byte][]byte{}, active: -1}
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
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeKB) since(n int) string { return strings.Join(f.calls[n:], ", ") }

func (f *fakeKB) EnableNotifications() error {
	f.record("notify")
	f.notified = true
	return nil
}

func (f *fakeKB) SwitchProfile(p byte) error {
	f.record("switch %d", p)
	if !f.notified {
		return errors.New("fake: no profile notification without 0x0e")
	}
	f.active = int(p)
	return nil
}

func (f *fakeKB) ReadButtons(p byte) (*proto.Table29, error) {
	f.record("read29 %d", p)
	return proto.ParseTable29(f.b29[p].Bytes())
}

func (f *fakeKB) ReadTypes(p byte) (*proto.Table25, error) {
	f.record("read25 %d", p)
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

func (f *fakeKB) WriteType(pos byte, te proto.TypeEntry) error {
	f.record("type %d", pos)
	if f.active < 0 {
		return errors.New("fake: widget type written before a profile switch")
	}
	return f.b25[byte(f.active)].SetType(int(pos), te)
}

func (f *fakeKB) WriteTypes(t *proto.Table25) error {
	f.record("write25 %d", t.Profile())
	if int(t.Profile()) != f.active {
		return fmt.Errorf("fake: type table of profile %d written while %d is active", t.Profile(), f.active)
	}
	c, err := proto.ParseTable25(t.Bytes())
	if err != nil {
		return err
	}
	f.b25[t.Profile()] = c
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
	rep, err := Apply(kb, cfg, l, full, nothingRuns)
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

func TestApplyOrder(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(
		prof(1, "sc.toml", "", btn(4, "Two"), btn(3, "One")),
		prof(0, "desktop.toml", "", btn(0, "Zero")),
	)
	rep := mustApply(t, f, cfg, l, false)

	want := "notify, " +
		"switch 0, read29 0, read25 0, upload 1, write29 0, type 0, " +
		"switch 1, read29 1, read25 1, upload 2, upload 3, write29 1, type 3, type 4, " +
		"switch 0"
	if got := f.since(0); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
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
	if _, err := Restore(g, l, 1); err != nil {
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
	if got, want := f.since(n), "notify, switch 1, read29 1, read25 1, switch 1"; got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
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
	want := "notify, switch 1, read29 1, read25 1, upload 1, upload 2, type 3, type 4, switch 1"
	if got := f.since(n); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
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
	want := "notify, switch 1, read29 1, read25 1, upload 1, upload 2, write29 1, type 3, type 4, type 5, switch 1"
	if got := f.since(0); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
	checkEntry(t, f, 1, 3, proto.AppEntry(1, ""))
	checkEntry(t, f, 1, 4, proto.AppEntry(2, ""))
	checkEntry(t, f, 1, 5, proto.AppEntry(1, ""))
	sameOwners(t, "Uploaded", rep.Uploaded, []ledger.Owner{own(1, 3), own(1, 4)})
	sameOwners(t, "Skipped", rep.Skipped, []ledger.Owner{own(1, 5)})
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
	want := "notify, switch 1, read29 1, read25 1, upload 1, write29 1, type 3, switch 1"
	if got := f.since(n); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
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
	want := "notify, switch 1, read29 1, read25 1, upload 1, write29 1, type 3, type 5, switch 1"
	if got := f.since(n); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
	checkEntry(t, f, 1, 5, proto.AppEntry(1, ""))
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
	want := "notify, switch 1, read29 1, read25 1, write29 1, write25 1, switch 1"
	if got := f.since(n); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
	checkEntry(t, f, 1, 4, keyEntry(4))
	checkType(t, f, 1, 4, keyType(4))
	checkEntry(t, f, 1, 3, proto.AppEntry(1, ""))
	checkType(t, f, 1, 3, proto.AppType)
	sameOwners(t, "Restored", rep.Restored, []ledger.Owner{own(1, 4)})
	if _, ok := l.SlotOf(own(1, 4)); ok {
		t.Fatal("restored position still holds a slot")
	}
	// Its slot is free for the next new picture.
	mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"), btn(6, "Six"))), l, false)
	checkEntry(t, f, 1, 6, proto.AppEntry(2, ""))
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
	want := "notify, switch 0, read29 0, read25 0, " +
		"switch 2, read29 2, read25 2, write29 2, write25 2, switch 0"
	if got := f.since(n); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
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
	cases := []struct {
		name    string
		cfg     *layout.Config
		running func(string) bool
		want    string
	}{
		{"game running", cfgOf(
			prof(0, "desktop.toml", "", btn(0, "Zero")),
			prof(2, "sc.toml", "StarCitizen.exe", btn(3, "One")),
		), func(n string) bool { return n == "StarCitizen.exe" }, "switch 2"},
		{"game not running", cfgOf(
			prof(0, "desktop.toml", "", btn(0, "Zero")),
			prof(2, "sc.toml", "StarCitizen.exe", btn(3, "One")),
		), nothingRuns, "switch 0"},
		{"no running check", cfgOf(
			prof(0, "desktop.toml", "", btn(0, "Zero")),
			prof(2, "sc.toml", "StarCitizen.exe", btn(3, "One")),
		), nil, "switch 0"},
		{"desktop is not the lowest", cfgOf(
			prof(0, "sc.toml", "StarCitizen.exe", btn(3, "One")),
			prof(1, "desktop.toml", "", btn(0, "Zero")),
		), nothingRuns, "switch 1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFake(t)
			if _, err := Apply(f, c.cfg, &ledger.Ledger{}, false, c.running); err != nil {
				t.Fatal(err)
			}
			if len(f.calls) == 0 || f.calls[len(f.calls)-1] != c.want {
				t.Fatalf("calls %v, want %q last", f.calls, c.want)
			}
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
			_, err := Apply(f, cfg, l, false, nothingRuns)
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
	if _, err := Apply(f, cfgOf(good, bad), l, true, nothingRuns); err == nil {
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
	_, err := Apply(f, cfgOf(prof(1, "sc.toml", "", bs...)), &ledger.Ledger{}, false, nothingRuns)
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
	rep, err := Apply(f, cfg, l, false, nothingRuns)
	if !errors.Is(err, keyboard.ErrUploadLeftOpen) {
		t.Fatalf("err = %v, want ErrUploadLeftOpen", err)
	}
	if !strings.Contains(err.Error(), "unplug") || !strings.Contains(err.Error(), "position 4") {
		t.Errorf("error %q lost the advice or the position", err)
	}
	if got, want := f.since(0), "notify, switch 1, read29 1, read25 1, upload 1, upload 2"; got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
	if _, ok := l.SlotOf(own(1, 4)); ok {
		t.Error("the owner of the failed upload still holds a slot")
	}
	if s, ok := l.SlotOf(own(1, 3)); !ok || s != 1 {
		t.Errorf("SlotOf(position 3) = %d, %v; want 1, true", s, ok)
	}
	if _, ok := l.SlotOf(own(1, 5)); ok {
		t.Error("a button after the failure got a slot")
	}
	sameOwners(t, "Uploaded", rep.Uploaded, []ledger.Owner{own(1, 3)})
}

func TestFailedUploadIntoSharedSlotIsNotTrusted(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := cfgOf(prof(1, "sc.toml", "", btn(3, "Same"), btn(5, "Same")))
	mustApply(t, f, cfg, l, false)
	f.uploadErr = func(byte) error { return keyboard.ErrOpenerRefused }
	if _, err := Apply(f, cfg, l, true, nothingRuns); !errors.Is(err, keyboard.ErrOpenerRefused) {
		t.Fatalf("err = %v, want ErrOpenerRefused", err)
	}
	// Position 5 still uses slot 1, whose content is unknown now: the next
	// apply sends it again.
	f.uploadErr = nil
	n := len(f.calls)
	mustApply(t, f, cfg, l, false)
	if got := f.since(n); !strings.Contains(got, "upload") {
		t.Fatalf("slot of a failed upload trusted: %s", got)
	}
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

func TestKeyboardErrorStops(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	// Without notifications the fake refuses the switch, as the keyboard
	// would not report it.
	k := &noNotify{f}
	_, err := Apply(k, cfgOf(prof(1, "sc.toml", "", btn(3, "One"))), l, false, nothingRuns)
	if err == nil || !strings.Contains(err.Error(), "profile 1") {
		t.Fatalf("err = %v", err)
	}
	if got, want := f.since(0), "switch 1"; got != want {
		t.Fatalf("calls after the failure: %s", got)
	}
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
	return f, l, orig
}

func TestRestore(t *testing.T) {
	f, l, orig := restoreSetup(t, "Home")
	before2 := f.b29[2].Bytes()
	rep, err := Restore(f, l, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := "notify, switch 1, read29 1, read25 1, write29 1, write25 1"
	if got := f.since(0); got != want {
		t.Fatalf("calls:\n got %s\nwant %s", got, want)
	}
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
	if !bytes.Equal(f.b29[2].Bytes(), before2) {
		t.Error("profile 2 changed")
	}
	sameOwners(t, "Restored", rep.Restored, []ledger.Owner{own(1, 3), own(1, 4)})
	sameOwners(t, "Owners(1)", l.Owners(1), nil)
	sameOwners(t, "Owners(2)", l.Owners(2), []ledger.Owner{own(2, 0)})
	if len(rep.Notes) != 0 {
		t.Errorf("notes: %q", rep.Notes)
	}
}

func TestRestoreLabelFallback(t *testing.T) {
	f, l, orig := restoreSetup(t, "bad\x01label")
	rep, err := Restore(f, l, 1)
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
	rep, err := Restore(f, &ledger.Ledger{}, 1)
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

func TestRestoreRefusesForeignPosition(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	if _, _, err := l.Assign(ledger.Owner{Profile: 1, Page: 2, Position: 3}, "h"); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(f, l, 1); err == nil {
		t.Fatal("a page 2 position was restored")
	}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "write") {
			t.Fatalf("written: %v", f.calls)
		}
	}
}
