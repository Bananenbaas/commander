// SPDX-License-Identifier: GPL-2.0-or-later

package action

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

// recorder logs every call of the fakes in one ordered list.
type recorder struct {
	log     []string
	failDn  Key
	failing bool
	failUp  Key
	upFails bool
}

func (r *recorder) Down(k Key) error {
	if r.failing && k == r.failDn {
		return errors.New("inject failed")
	}
	r.log = append(r.log, fmt.Sprintf("down %#x", uint16(k)))
	return nil
}

func (r *recorder) Up(k Key) error {
	if r.upFails && k == r.failUp {
		return errors.New("release failed")
	}
	r.log = append(r.log, fmt.Sprintf("up %#x", uint16(k)))
	return nil
}

func (r *recorder) Run(program string, args []string) error {
	r.log = append(r.log, fmt.Sprintf("run %s %v", program, args))
	return nil
}

func (r *recorder) Open(target string) error {
	r.log = append(r.log, "open "+target)
	return nil
}

func newExec(r *recorder) *Executor {
	return &Executor{In: r, L: r, Sleep: func(d time.Duration) {
		r.log = append(r.log, "sleep "+d.String())
	}}
}

func check(t *testing.T, r *recorder, want ...string) {
	t.Helper()
	if len(r.log) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(r.log, want) {
		t.Fatalf("log\n got %q\nwant %q", r.log, want)
	}
}

func TestTapChord(t *testing.T) {
	r := &recorder{}
	if err := newExec(r).Tap([]Step{Chord{Keys: []Key{LAlt, N}}}); err != nil {
		t.Fatal(err)
	}
	check(t, r, "down 0xe2", "down 0x11", "sleep 40ms", "up 0x11", "up 0xe2")
}

func TestTapTwoStepsSleepBetween(t *testing.T) {
	r := &recorder{}
	err := newExec(r).Tap([]Step{Chord{Keys: []Key{A}}, Chord{Keys: []Key{B}}})
	if err != nil {
		t.Fatal(err)
	}
	check(t, r, "down 0x4", "sleep 40ms", "up 0x4", "sleep 20ms",
		"down 0x5", "sleep 40ms", "up 0x5")
}

func TestTapWaitRunOpen(t *testing.T) {
	r := &recorder{}
	err := newExec(r).Tap([]Step{
		Wait{D: 600 * time.Millisecond},
		Run{Program: "prog", Args: []string{"a b"}},
		Open{Target: "https://example.org"},
	})
	if err != nil {
		t.Fatal(err)
	}
	check(t, r, "sleep 600ms", "sleep 20ms", "run prog [a b]", "sleep 20ms",
		"open https://example.org")
}

func TestTapInjectorErrorReleasesPressed(t *testing.T) {
	r := &recorder{failing: true, failDn: N}
	err := newExec(r).Tap([]Step{Chord{Keys: []Key{LCtrl, LAlt, N}}, Chord{Keys: []Key{A}}})
	if err == nil {
		t.Fatal("expected an error")
	}
	check(t, r, "down 0xe0", "down 0xe2", "up 0xe2", "up 0xe0")
}

func TestHoldAndRelease(t *testing.T) {
	r := &recorder{}
	e := newExec(r)
	c := Chord{Keys: []Key{LCtrl, A}}
	if err := e.HoldDown(c); err != nil {
		t.Fatal(err)
	}
	if err := e.HoldUp(c); err != nil {
		t.Fatal(err)
	}
	check(t, r, "down 0xe0", "down 0x4", "up 0x4", "up 0xe0")
	r.log = nil
	if err := e.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	check(t, r) // nothing is held any more
}

func TestReleaseAllReleasesOnlyHeld(t *testing.T) {
	r := &recorder{}
	e := newExec(r)
	_ = e.HoldDown(Chord{Keys: []Key{LShift, B}})
	_ = e.HoldDown(Chord{Keys: []Key{C}})
	r.log = nil
	if err := e.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	check(t, r, "up 0x6", "up 0x5", "up 0xe1")
	r.log = nil
	_ = e.ReleaseAll()
	check(t, r)
}

func TestHoldUpFailedUpStaysHeld(t *testing.T) {
	r := &recorder{}
	e := newExec(r)
	c := Chord{Keys: []Key{LCtrl, A}}
	_ = e.HoldDown(c)
	r.log = nil
	r.upFails, r.failUp = true, A
	if err := e.HoldUp(c); err == nil {
		t.Fatal("expected an error")
	}
	check(t, r, "up 0xe0")
	r.upFails = false
	r.log = nil
	if err := e.ReleaseAll(); err != nil {
		t.Fatal(err)
	}
	check(t, r, "up 0x4")
}
