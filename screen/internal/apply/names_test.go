// SPDX-License-Identifier: GPL-2.0-or-later

package apply

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/ledger"
	"github.com/Bananenbaas/commander/screen/internal/proto"
)

// stored16 is the 0x16 table the fake holds before anything is applied: the
// five names of a fresh keyboard, slot 3 with bytes after its NUL.
func stored16(t *testing.T) *proto.Table16 {
	t.Helper()
	b := make([]byte, proto.Len16)
	b[0], b[1] = 0x16, 0x68
	for i, n := range []string{"DESKTOP", "SC", "DEFAULT 03", "DEFAULT 04", "DEFAULT 05"} {
		copy(b[2+20*i:], n)
	}
	copy(b[2+20*3+11:], []byte{0x04, 0x00, 0x00, 0x05})
	t16, err := proto.ParseTable16(seal(b))
	if err != nil {
		t.Fatal(err)
	}
	return t16
}

func (f *fakeKB) ReadNamesTable() (*proto.Table16, error) {
	f.record("read16")
	if f.b16 == nil {
		f.b16 = stored16(f.t)
	}
	return proto.ParseTable16(f.b16.Bytes())
}

func (f *fakeKB) WriteNamesTable(t *proto.Table16) error {
	f.record("write16")
	if f.namesErr != nil {
		return f.namesErr
	}
	c, err := proto.ParseTable16(t.Bytes())
	if err != nil {
		return err
	}
	f.b16 = c
	return nil
}

func nameCalls(f *fakeKB) []string {
	var out []string
	for _, c := range f.calls {
		if c == "read16" || c == "write16" {
			out = append(out, c)
		}
	}
	return out
}

func TestApplyRenamesProfiles(t *testing.T) {
	f := newFake(t)
	before := stored16(t).Bytes()
	p1 := prof(1, "sc.toml", "", btn(3, "One"))
	p1.Name = "SC" // already the name
	p2 := prof(2, "two.toml", "", btn(4, "Two"))
	p2.Name = "TEST"
	p4 := prof(4, "four.toml", "")
	p4.Name = "Edit"
	rep := mustApply(t, f, cfgOf(p1, p2, p4, prof(0, "d.toml", "", btn(5, "D"))), &ledger.Ledger{}, false)

	want := []Rename{{Profile: 2, Name: "TEST"}, {Profile: 4, Name: "Edit"}}
	if len(rep.Renamed) != len(want) || rep.Renamed[0] != want[0] || rep.Renamed[1] != want[1] {
		t.Fatalf("Renamed = %v, want %v", rep.Renamed, want)
	}
	if got := nameCalls(f); strings.Join(got, ",") != "read16,write16" {
		t.Errorf("calls %v, want one read and one write", got)
	}
	after := f.b16.Bytes()
	for i := range after[:102] {
		inTwo, inFour := i >= 42 && i < 62, i >= 82 && i < 102
		if !inTwo && !inFour && after[i] != before[i] {
			t.Fatalf("byte %d changed outside the renamed fields", i)
		}
	}
	if f.b16.Name(2) != "TEST" || f.b16.Name(4) != "Edit" || f.b16.Name(0) != "DESKTOP" || f.b16.Name(1) != "SC" || f.b16.Name(3) != "DEFAULT 04" {
		t.Errorf("names %q %q %q %q %q", f.b16.Name(0), f.b16.Name(1), f.b16.Name(2), f.b16.Name(3), f.b16.Name(4))
	}
	if !bytes.Equal(after[2+20*3:2+20*4], before[2+20*3:2+20*4]) {
		t.Error("slot 3 not written as read")
	}

	// A second apply finds the names as asked and writes nothing.
	n := len(f.calls)
	rep = mustApply(t, f, cfgOf(p1, p2, p4, prof(0, "d.toml", "", btn(5, "D"))), &ledger.Ledger{}, false)
	if len(rep.Renamed) != 0 {
		t.Errorf("second apply: Renamed = %v", rep.Renamed)
	}
	for _, c := range f.calls[n:] {
		if c == "write16" {
			t.Error("second apply wrote the names")
		}
	}
}

func TestApplyNoNameNoTable(t *testing.T) {
	f := newFake(t)
	rep := mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"))), &ledger.Ledger{}, false)
	if c := nameCalls(f); len(c) != 0 || len(rep.Renamed) != 0 {
		t.Errorf("name calls %v, Renamed %v", c, rep.Renamed)
	}
}

func TestApplyRenameError(t *testing.T) {
	f := newFake(t)
	f.namesErr = errors.New("status 05 00 00 00")
	p := prof(2, "two.toml", "", btn(4, "Two"))
	p.Name = "TEST"
	rep, err := Apply(context.Background(), f, cfgOf(p), &ledger.Ledger{}, false, nothingRuns, nil)
	if err == nil || !strings.Contains(err.Error(), "profile names") {
		t.Fatalf("err = %v", err)
	}
	if len(rep.Renamed) != 0 {
		t.Errorf("Renamed = %v after a failed write", rep.Renamed)
	}
}
