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
	// slot 2 is free again, so the next new hash takes it.
	mustAssign(t, l, c, "h3", 2, true)
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
	mustAssign(t, l, c, "h2", 1, true)
	mustAssign(t, l, a, "h1", 2, true) // old hash is gone with the slot
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

func TestSaveLoadRoundTrip(t *testing.T) {
	l := &Ledger{}
	mustAssign(t, l, a, "h1", 1, true)
	mustAssign(t, l, b, "h1", 1, false)
	mustAssign(t, l, c, "h2", 2, true)
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
