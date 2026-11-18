// SPDX-License-Identifier: GPL-2.0-or-later

package apply

import (
	"context"
	"reflect"
	"testing"

	"github.com/Bananenbaas/kb7-commander/screen/internal/ledger"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
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
		if err := f.b25[2].SetType(typeIndex(i), te); err != nil {
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
		checkType(t, f, 2, typeIndex(i), proto.TypeEntry{})
	}
	checkType(t, f, 2, 2, proto.TypeEntry{Type: 0x05, Value: 0x01})
	checkEntry(t, f, 2, 1, keyEntry(1))
	checkType(t, f, 2, 12, keyType(12)) // page 2's marker untouched
}

func TestClearLaunchersRefusesManagedProfile(t *testing.T) {
	f, l, _ := restoreSetup(t, "Home")
	if _, err := ClearLaunchers(context.Background(), f, l, 1); err == nil {
		t.Fatal("managed profile cleared")
	}
	if len(f.calls) != 0 {
		t.Errorf("calls made: %q", f.calls)
	}
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

func TestTypeIndex(t *testing.T) {
	for i, want := range map[int]int{0: 0, 11: 11, 12: 13, 16: 17, 24: 26, 34: 36} {
		if got := typeIndex(i); got != want {
			t.Errorf("typeIndex(%d) = %d, want %d", i, got, want)
		}
	}
}
