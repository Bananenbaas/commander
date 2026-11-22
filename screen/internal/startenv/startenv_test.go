// SPDX-License-Identifier: GPL-2.0-or-later

package startenv

import (
	"reflect"
	"testing"
)

func TestChangesPutsBackWhatWasThere(t *testing.T) {
	before := map[string]Value{
		"A": {},                        // unset at start
		"B": {Set: true},               // set to "" at start
		"C": {Text: "keep", Set: true}, // set at start, changed since
		"D": {Text: "same", Set: true}, // unchanged
		"E": {},                        // unset at start and now
	}
	now := map[string]Value{
		"A": {Text: "", Set: true},
		"B": {Text: "x", Set: true},
		"C": {Text: "0", Set: true},
		"D": {Text: "same", Set: true},
		"E": {},
	}
	got := changes([]string{"A", "B", "C", "D", "E"}, before, now)
	want := []Change{
		{Name: "A", Unset: true},
		{Name: "B", Text: ""},
		{Name: "C", Text: "keep"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("changes = %+v, want %+v", got, want)
	}
}

func TestChangesUnsetsWhatWasRemoved(t *testing.T) {
	before := map[string]Value{"A": {Text: "v", Set: true}}
	now := map[string]Value{"A": {}}
	got := changes([]string{"A"}, before, now)
	if want := []Change{{Name: "A", Text: "v"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("changes = %+v, want %+v", got, want)
	}
}

func TestRecordReadsEveryName(t *testing.T) {
	env := map[string]string{"A": "1", "B": ""}
	got := record([]string{"A", "B", "C"}, func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	})
	want := map[string]Value{"A": {Text: "1", Set: true}, "B": {Set: true}, "C": {}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("record = %+v, want %+v", got, want)
	}
}

func TestRestoreUndoesAChange(t *testing.T) {
	const name = "WEBVIEW2_USER_DATA_FOLDER"
	t.Setenv(name, "changed by a test")
	if err := Restore(); err != nil {
		t.Fatal(err)
	}
	v, ok := lookup(name)
	if s := start[name]; v != s.Text || ok != s.Set {
		t.Errorf("after Restore %s = %q, %v; at start %q, %v", name, v, ok, s.Text, s.Set)
	}
}
