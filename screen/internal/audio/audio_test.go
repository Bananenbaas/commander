// SPDX-License-Identifier: GPL-2.0-or-later

package audio

import (
	"errors"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestExeName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{`C:\Program Files\Grover\GroverPro.exe`, "GroverPro"},
		{`C:\Apps\opera.EXE`, "opera"},
		{`D:\tools\player`, "player"},
		{`/usr/bin/thing.exe`, "thing"},
		{`plain.exe`, "plain"},
		{``, ""},
	} {
		if got := exeName(c.in); got != c.want {
			t.Errorf("exeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLevelOf(t *testing.T) {
	for _, c := range []struct {
		in   float32
		want int
	}{{0, 0}, {1, 100}, {0.5, 50}, {0.444, 44}, {0.446, 45}, {0.999, 100}, {-0.1, 0}, {1.5, 100}} {
		if got := levelOf(c.in); got != c.want {
			t.Errorf("levelOf(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestMergeSessions(t *testing.T) {
	exe := func(pid uint32) string {
		return map[uint32]string{100: "opera", 200: "GroverPro", 300: ""}[pid]
	}
	raw := []rawSession{
		{PID: 100, Display: "", Level: 80},
		{PID: 200, Display: "@%SystemRoot%\\x.dll,-1", Level: 30, Muted: true},
		{PID: 100, Display: "Opera", Level: 20, Muted: true},
		{PID: 300, Display: "  ", Level: 5},
	}
	got := merge(raw, exe)
	want := []Session{
		// Two sessions of one process are one program: the first session's
		// level and mute, the first usable display name.
		{ID: "100", Name: "Opera", Level: 80},
		{ID: "200", Name: "GroverPro", Level: 30, Muted: true},
		{ID: "300", Name: "Program 300", Level: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merge =\n%+v\nwant\n%+v", got, want)
	}
	if got := merge(nil, exe); len(got) != 0 {
		t.Errorf("merge(nil) = %+v", got)
	}
}

// fakeEngine stands in for the Windows audio; it records the goroutine-free
// calls the Windows type forwards to it.
type fakeEngine struct {
	mu      sync.Mutex
	level   int
	program map[string]int
	closed  bool
}

func (f *fakeEngine) sessions() (Session, []Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Session{ID: "system", Name: "SystemVolume", Level: f.level},
		[]Session{{ID: "7", Name: "player", Level: f.program["7"]}}, nil
}

func (f *fakeEngine) setSystem(level int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.level = level
	return nil
}

func (f *fakeEngine) setProgram(id string, level int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.program[id]; !ok {
		return errors.New("no such program")
	}
	f.program[id] = level
	return nil
}

func (f *fakeEngine) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}

func TestChangesFromCallbacks(t *testing.T) {
	fe := &fakeEngine{level: 40, program: map[string]int{"7": 60}}
	var signal func()
	w, err := start(nil, func(s func(), _ func(string, ...any)) (engine, error) {
		signal = s
		return fe, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	select {
	case <-w.Changes():
		t.Fatal("a change before any callback")
	default:
	}
	// A callback signals; several in a row never block and leave one
	// pending change.
	done := make(chan struct{})
	go func() {
		signal()
		signal()
		signal()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("signal blocked")
	}
	select {
	case <-w.Changes():
	case <-time.After(2 * time.Second):
		t.Fatal("no change after a callback")
	}
	select {
	case <-w.Changes():
		t.Fatal("more than one pending change")
	default:
	}

	// The fake level change is read through the forwarded calls.
	if err := w.SetSystemLevel(55); err != nil {
		t.Fatal(err)
	}
	if err := w.SetProgramLevel("7", 12); err != nil {
		t.Fatal(err)
	}
	if err := w.SetProgramLevel("8", 12); err == nil {
		t.Error("SetProgramLevel of an unknown program: no error")
	}
	sys, progs, err := w.Sessions()
	if err != nil {
		t.Fatal(err)
	}
	if sys.Level != 55 || len(progs) != 1 || progs[0].Level != 12 {
		t.Errorf("Sessions = %+v %+v", sys, progs)
	}
}

func TestSetLevelClamps(t *testing.T) {
	fe := &fakeEngine{program: map[string]int{"7": 0}}
	w, err := start(nil, func(func(), func(string, ...any)) (engine, error) { return fe, nil })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.SetSystemLevel(140); err != nil {
		t.Fatal(err)
	}
	if err := w.SetProgramLevel("7", -3); err != nil {
		t.Fatal(err)
	}
	if fe.level != 100 || fe.program["7"] != 0 {
		t.Errorf("levels %d %d, want 100 0", fe.level, fe.program["7"])
	}
}

func TestClose(t *testing.T) {
	fe := &fakeEngine{program: map[string]int{}}
	w, err := start(nil, func(func(), func(string, ...any)) (engine, error) { return fe, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	fe.mu.Lock()
	closed := fe.closed
	fe.mu.Unlock()
	if !closed {
		t.Error("the engine was not closed")
	}
	if _, _, err := w.Sessions(); !errors.Is(err, ErrClosed) {
		t.Errorf("Sessions after Close: %v, want ErrClosed", err)
	}
	if err := w.SetSystemLevel(5); !errors.Is(err, ErrClosed) {
		t.Errorf("SetSystemLevel after Close: %v, want ErrClosed", err)
	}
}

func TestStartFails(t *testing.T) {
	boom := errors.New("boom")
	if _, err := start(nil, func(func(), func(string, ...any)) (engine, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Errorf("start: %v, want boom", err)
	}
}

func TestNewNeedsWindows(t *testing.T) {
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		t.Skip("the Windows build has the real mixer")
	}
	w, err := New(nil)
	if err == nil {
		w.Close()
		t.Fatal("New: no error off Windows")
	}
	if runtime.GOOS != "windows" && err.Error() != "the mixer needs Windows" {
		t.Errorf("New: %v", err)
	}
}
