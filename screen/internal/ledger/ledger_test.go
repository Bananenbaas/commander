// SPDX-License-Identifier: GPL-2.0-or-later

package ledger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

func mustAssign(t *testing.T, l *Ledger, o Owner, hash string, wantSlot byte, wantUpload bool) {
	t.Helper()
	slot, up, err := l.Assign(o, hash)
	if err != nil {
		t.Fatalf("Assign(%v, %q): %v", o, hash, err)
	}
	if slot != wantSlot || up != wantUpload {
		t.Fatalf("Assign(%v, %q) = %d, %v; want %d, %v", o, hash, slot, up, wantSlot, wantUpload)
	}
}

var (
	a = Owner{1, 1, 0}
	b = Owner{1, 1, 1}
	c = Owner{2, 1, 0}
)

func TestAssignFirstAndRepeat(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, a, "h1", 1, false)
	if s, ok := l.SlotOf(a); !ok || s != 1 {
		t.Fatalf("SlotOf = %d, %v", s, ok)
	}
	if _, ok := l.SlotOf(b); ok {
		t.Fatal("SlotOf for unknown owner reported ok")
	}
}

func TestAssignShared(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h1", 1, false)
	mustAssign(t, l, c, "h2", 2, true)
}

func TestSharedOwnerChangesHash(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h1", 1, false)
	mustAssign(t, l, b, "h2", 2, true)
	// a still owns slot 1 with h1.
	mustAssign(t, l, a, "h1", 1, false)
}

func TestOwnerReusesOwnSlot(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, a, "h2", 1, true)
	mustAssign(t, l, a, "h2", 1, false)
}

func TestMoveToExistingHashFreesOldSlot(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h2", 2, true)
	mustAssign(t, l, b, "h1", 1, false)
	// Slot 2 is free but still holds h2: a new hash takes a slot whose
	// content is unknown, and h2 comes back without an upload.
	mustAssign(t, l, c, "h3", 3, true)
	mustAssign(t, l, a, "h2", 2, false)
}

func TestFreedSlotKeepsItsContent(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	l.Release(a)
	if _, ok := l.SlotOf(a); ok {
		t.Fatal("released owner still has a slot")
	}
	// A button moved to another position finds its picture again.
	mustAssign(t, l, b, "h1", 1, false)
}

func TestFreeSlotWithKnownContentIsTakenLast(t *testing.T) {
	l := &Ledger{}
	for i := 1; i <= MaxSlot; i++ {
		mustAssign(t, l, Owner{1, 1, i}, fmt.Sprintf("h%d", i), byte(i), true)
	}
	l.Release(Owner{1, 1, 4})
	mustAssign(t, l, Owner{2, 1, 0}, "new", 4, true)
	// h4 is gone with the slot's new content.
	if _, _, err := l.Assign(Owner{2, 1, 1}, "h4"); !errors.Is(err, ErrNoSlot) {
		t.Fatalf("err = %v, want ErrNoSlot", err)
	}
}

func TestForgetDropsTheContentOfFreeSlots(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h2", 2, true)
	l.Release(a)
	l.Release(b)
	l.Forget()
	mustAssign(t, l, c, "h1", 1, true)
	l.ForgetSlot(2)
	mustAssign(t, l, a, "h2", 2, true)
}

func TestNoSlot(t *testing.T) {
	l := &Ledger{}
	for i := 1; i <= MaxSlot; i++ {
		mustAssign(t, l, Owner{1, 1, i}, fmt.Sprintf("h%d", i), byte(i), true)
	}
	_, _, err := l.Assign(Owner{2, 1, 0}, "extra")
	if !errors.Is(err, ErrNoSlot) {
		t.Fatalf("err = %v, want ErrNoSlot", err)
	}
	// A failed assign leaves the ledger untouched.
	mustAssign(t, l, Owner{1, 1, 1}, "h1", 1, false)
}

func TestRelease(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h1", 1, false)
	l.Release(a)
	mustAssign(t, l, b, "h1", 1, false)
	l.Release(b)
	if _, ok := l.SlotOf(b); ok {
		t.Fatal("released owner still has a slot")
	}
	mustAssign(t, l, c, "h2", 2, true)  // a slot of unknown content first
	mustAssign(t, l, a, "h1", 1, false) // the free slot kept its picture
}

func TestOriginals(t *testing.T) {
	l := &Ledger{}
	if _, ok := l.Original(a); ok {
		t.Fatal("unexpected original")
	}
	first := Original{Entry: proto.Entry29{Mod: 1, Key: 2, Kind: 3, Slot: 4, Label: "x"}, Type: proto.TypeEntry{Type: 5, Value: 6}}
	second := Original{Entry: proto.Entry29{Key: 9}}
	l.RecordOriginal(a, first)
	l.RecordOriginal(a, second)
	got, ok := l.Original(a)
	if !ok || got != first {
		t.Fatalf("Original = %+v, %v; want %+v", got, ok, first)
	}
}

func TestForgetOriginal(t *testing.T) {
	l := &Ledger{}
	l.ForgetOriginal(a) // nothing recorded: nothing happens
	first := Original{Entry: proto.Entry29{Key: 1}}
	later := Original{Entry: proto.Entry29{Key: 2}}
	l.RecordOriginal(a, first)
	l.RecordOriginal(b, first)
	l.ForgetOriginal(a)
	if _, ok := l.Original(a); ok {
		t.Fatal("original still recorded")
	}
	if got, ok := l.Original(b); !ok || got != first {
		t.Fatalf("other original = %+v, %v", got, ok)
	}
	l.RecordOriginal(a, later)
	if got, _ := l.Original(a); got != later {
		t.Fatalf("Original after a new takeover = %+v, want %+v", got, later)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	l.ForgetOriginal(a)
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Original(a); ok {
		t.Fatal("forgotten original saved")
	}
}

func TestForget(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h1", 1, false)
	mustAssign(t, l, c, "h2", 2, true)
	l.Forget()
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h1", 1, false)
	mustAssign(t, l, c, "h2", 2, true)
	mustAssign(t, l, c, "h2", 2, false)
}

func TestForgetSlot(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h1", 1, false)
	mustAssign(t, l, c, "h2", 2, true)
	l.ForgetSlot(1)
	l.ForgetSlot(5) // a free slot: nothing happens
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h1", 1, false)
	mustAssign(t, l, c, "h2", 2, false)
	if _, ok := l.SlotOf(Owner{1, 1, 5}); ok {
		t.Fatal("forgetting a free slot created an owner")
	}
}

func TestOwners(t *testing.T) {
	l := &Ledger{}
	if got := l.Owners(1); len(got) != 0 {
		t.Fatalf("Owners of an empty ledger = %v", got)
	}
	mustAssign(t, l, Owner{1, 1, 7}, "h1", 1, true)
	mustAssign(t, l, Owner{2, 1, 0}, "h1", 1, false)
	mustAssign(t, l, Owner{1, 1, 2}, "h2", 2, true)
	mustAssign(t, l, Owner{1, 1, 4}, "h1", 1, false)
	l.RecordOriginal(Owner{1, 1, 9}, Original{}) // an original alone is not an owner
	want := []Owner{{1, 1, 2}, {1, 1, 4}, {1, 1, 7}}
	got := l.Owners(1)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("Owners(1) = %v, want %v", got, want)
	}
	if got := l.Owners(2); fmt.Sprint(got) != fmt.Sprint([]Owner{{2, 1, 0}}) {
		t.Fatalf("Owners(2) = %v", got)
	}
	l.Release(Owner{1, 1, 4})
	if got := l.Owners(1); fmt.Sprint(got) != fmt.Sprint([]Owner{{1, 1, 2}, {1, 1, 7}}) {
		t.Fatalf("Owners(1) after release = %v", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h1", 1, false)
	mustAssign(t, l, c, "h2", 2, true)
	mustAssign(t, l, Owner{3, 1, 0}, "h3", 3, true)
	l.Release(Owner{3, 1, 0})
	orig := Original{Entry: proto.Entry29{Mod: 1, Key: 2, Kind: 3, Slot: 4, Label: "x"}, Type: proto.TypeEntry{Type: 5, Value: 6}}
	l.RecordOriginal(a, orig)

	path := filepath.Join(t.TempDir(), "state.json")
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	mustAssign(t, m, a, "h1", 1, false)
	mustAssign(t, m, b, "h1", 1, false)
	mustAssign(t, m, c, "h2", 2, false)
	mustAssign(t, m, Owner{4, 1, 0}, "h3", 3, false) // a free slot keeps its content
	if got, ok := m.Original(a); !ok || got != orig {
		t.Fatalf("Original = %+v, %v", got, ok)
	}
}

func TestLoadMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	l, err := Load(filepath.Join(dir, "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	mustAssign(t, l, a, "h1", 1, true)

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Fatal("corrupt file loaded without error")
	}
}

func TestLoadOlderFile(t *testing.T) {
	// A file written before free slots kept their content.
	path := filepath.Join(t.TempDir(), "state.json")
	data := `{"slots":[{"slot":2,"hash":"h2","users":["p1/1/0"]}],"originals":[]}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	mustAssign(t, l, a, "h2", 2, false)
	mustAssign(t, l, b, "h9", 1, true)
}

func TestBlockedSlotIsNeitherMatchedNorTaken(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h2", 2, true)
	l.Release(b)
	l.Block(1)
	l.Block(2)
	l.Block(3)
	// a leaves its blocked slot for a new one, and the free slot 2 is not
	// matched either: a launcher kb7screen does not manage shows them.
	mustAssign(t, l, a, "h1", 4, true)
	mustAssign(t, l, c, "h2", 5, true)
	if !l.Blocked(3) || l.Blocked(6) {
		t.Fatal("Blocked does not report the blocked slots")
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Blocked(3) {
		t.Fatal("a blocked slot was saved")
	}
}
