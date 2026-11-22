// SPDX-License-Identifier: GPL-2.0-or-later

package apply

import (
	"context"
	"reflect"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/ledger"
	"github.com/Bananenbaas/commander/screen/internal/proto"
)

func TestClearLaunchers(t *testing.T) {
	f := newFake(t)
	// Profile 2 as Swarm II left it in 40-swarm-slots-pages-1: applications
	// on page 1, a website on page 2, a third-party application on page 3,
	// and a modifier button (type 05/01) that must stay.
	set := func(i int, e proto.Entry29, te proto.TypeEntry) {
		if err := f.b29[2].SetEntry(i, e); err != nil {
			t.Fatal(err)
		}
		if err := f.b25[2].SetType(typeAt(t, i), te); err != nil {
			t.Fatal(err)
		}
	}
	set(0, proto.AppEntry(9, "app1"), proto.AppType)
	set(6, proto.AppEntry(15, "app7"), proto.AppType)
	set(16, proto.Entry29{Mod: 0x07, Key: 0x0b, Label: "hohoho.nl"}, proto.TypeEntry{Type: 0x05, Value: 0x05})
	set(28, proto.Entry29{Kind: 0x01, Label: "steam"}, proto.TypeEntry{Type: 0x42})
	set(2, proto.Entry29{Kind: 0x01, Key: 0xe1}, proto.TypeEntry{Type: 0x05, Value: 0x01})

	got, err := ClearLaunchers(context.Background(), f, &ledger.Ledger{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"page 1 position 0", "page 1 position 6", "page 2 position 4", "page 3 position 4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleared %q, want %q", got, want)
	}
	for _, i := range []int{0, 6, 16, 28} {
		checkEntry(t, f, 2, i, proto.Entry29{})
		checkType(t, f, 2, typeAt(t, i), proto.TypeEntry{})
	}
	checkType(t, f, 2, 2, proto.TypeEntry{Type: 0x05, Value: 0x01})
	checkEntry(t, f, 2, 1, keyEntry(1))
	checkType(t, f, 2, 12, keyType(12)) // page 2's marker untouched
}

func TestClearLaunchersSkipsManagedPositions(t *testing.T) {
	f, l, _ := restoreSetup(t, "Home") // profile 1 positions 3 and 4 are managed
	foreign(t, f, 1, 5, 12)
	foreign(t, f, 1, 15, 13) // page 2 position 3: not managed
	if err := f.b25[1].SetType(5, proto.AppType); err != nil {
		t.Fatal(err)
	}
	// Page 2 position 6 (0x29 entry 18, 0x25 entry 19) is managed.
	managed := ledger.Owner{Profile: 1, Page: 2, Position: 6}
	slot, _, err := l.Assign(managed, "page2")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.b29[1].SetEntry(18, proto.AppEntry(slot, "")); err != nil {
		t.Fatal(err)
	}
	if err := f.b25[1].SetType(19, proto.AppType); err != nil {
		t.Fatal(err)
	}
	got, err := ClearLaunchers(context.Background(), f, l, 1)
	if err != nil {
		t.Fatal(err)
	}
	checkEntry(t, f, 1, 18, proto.AppEntry(slot, ""))
	checkType(t, f, 1, 19, proto.AppType)
	want := []string{"page 1 position 5", "page 2 position 3"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cleared %q, want %q", got, want)
	}
	checkEntry(t, f, 1, 5, proto.Entry29{})
	checkType(t, f, 1, 5, proto.TypeEntry{})
	checkEntry(t, f, 1, 15, proto.Entry29{})
	for _, pos := range []int{3, 4} {
		if e := f.b29[1].Entry(pos); e.Kind != proto.KindApp {
			t.Errorf("managed position %d cleared: %+v", pos, e)
		}
		checkType(t, f, 1, pos, proto.AppType)
	}
	if _, ok := l.SlotOf(own(1, 3)); !ok {
		t.Error("the ledger lost a managed position")
	}
}

func TestClearLaunchersLastTypeEntry(t *testing.T) {
	// Page 3 position 11 has a 0x25 entry (37) but no 0x29 entry.
	f := newFake(t)
	if err := f.b25[2].SetType(37, proto.TypeEntry{Type: 0x42}); err != nil {
		t.Fatal(err)
	}
	if err := f.b25[2].SetType(36, proto.TypeEntry{Type: 0x05, Value: 0x01}); err != nil {
		t.Fatal(err)
	}
	got, err := ClearLaunchers(context.Background(), f, &ledger.Ledger{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"page 3 position 11"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cleared %q, want %q", got, want)
	}
	checkType(t, f, 2, 37, proto.TypeEntry{})
	checkType(t, f, 2, 36, proto.TypeEntry{Type: 0x05, Value: 0x01})
	checkEntry(t, f, 2, 34, keyEntry(34))
}

func TestClearLaunchersNothingToDo(t *testing.T) {
	f := newFake(t)
	got, err := ClearLaunchers(context.Background(), f, &ledger.Ledger{}, 3)
	if err != nil || got != nil {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, c := range f.calls {
		if c == "write29 3" || c == "write25 3" {
			t.Errorf("wrote although nothing launches: %q", f.calls)
		}
	}
}

// typeAt is the 0x25 entry of 0x29 entry i.
func typeAt(t *testing.T, i int) int {
	t.Helper()
	ti, err := proto.TypeIndex(i/12+1, i%12)
	if err != nil {
		t.Fatal(err)
	}
	return ti
}
