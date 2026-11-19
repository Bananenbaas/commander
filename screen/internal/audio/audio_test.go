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

func TestProgramName(t *testing.T) {
	const path = `C:\Program Files (x86)\Steam\steam.exe`
	for _, c := range []struct {
		name string
		info map[string]string
		want string
	}{
		{"the description wins over the executable", map[string]string{"FileDescription": "Steam", "ProductName": "Steam Client"}, "Steam"},
		{"spaces trimmed", map[string]string{"FileDescription": "  Steam \t"}, "Steam"},
		{"an empty description falls back to the product", map[string]string{"FileDescription": "  ", "ProductName": "Steam Client"}, "Steam Client"},
		{"no version information", nil, "steam"},
		{"empty strings", map[string]string{"FileDescription": "", "ProductName": " "}, "steam"},
	} {
		if got := programName(path, func(key string) string { return c.info[key] }); got != c.want {
			t.Errorf("%s: programName = %q, want %q", c.name, got, c.want)
		}
	}
	if got := programName("", func(string) string { return "Steam" }); got != "" {
		t.Errorf("programName of no path = %q, want \"\"", got)
	}
}

func TestStringBlocks(t *testing.T) {
	for _, c := range []struct {
		trans []uint16
		want  []string
	}{
		{nil, []string{`\StringFileInfo\040904b0\`}},
		{[]uint16{0x0413, 0x04b0}, []string{`\StringFileInfo\041304b0\`, `\StringFileInfo\040904b0\`}},
		{[]uint16{0x0409, 0x04e4, 0x0409, 0x04b0}, []string{`\StringFileInfo\040904e4\`, `\StringFileInfo\040904b0\`}},
		{[]uint16{0x0000, 0x04b0, 0x0409}, []string{`\StringFileInfo\000004b0\`, `\StringFileInfo\040904b0\`}},
	} {
		if got := stringBlocks(c.trans); !reflect.DeepEqual(got, c.want) {
			t.Errorf("stringBlocks(%04x) = %q, want %q", c.trans, got, c.want)
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
	raw := []rawSession{
		{PID: 100, Display: "", Exe: "opera", Level: 80},
		{PID: 200, Display: "@%SystemRoot%\\x.dll,-1", Exe: "GroverPro", Level: 30, Muted: true, Active: true},
		{PID: 100, Display: "Opera", Exe: "opera", Level: 20, Muted: true, Active: true},
		{PID: 300, Display: "  ", Level: 5},
	}
	got := merge(raw)
	want := []Session{
		// Two sessions of one process are one program: the first session's
		// level and mute, the first usable display name, active when any of
		// them is.
		{ID: "100", Name: "Opera", Level: 80, Active: true},
		{ID: "200", Name: "GroverPro", Level: 30, Muted: true, Active: true},
		{ID: "300", Name: "Program 300", Level: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merge =\n%+v\nwant\n%+v", got, want)
	}
	if got := merge(nil); len(got) != 0 {
		t.Errorf("merge(nil) = %+v", got)
	}
}

// fakeEngine stands in for the Windows audio; it records the goroutine-free
// calls the Windows type forwards to it.
type fakeEngine struct {
	mu      sync.Mutex
	level   int
	program map[string]int
	devs    int
	closed  bool
}

func (f *fakeEngine) devices() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.devs
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

func TestDevices(t *testing.T) {
	fe := &fakeEngine{program: map[string]int{}, devs: 2}
	w, err := start(nil, func(func(), func(string, ...any)) (engine, error) { return fe, nil })
	if err != nil {
		t.Fatal(err)
	}
	if n := w.Devices(); n != 2 {
		t.Errorf("Devices = %d, want 2", n)
	}
	w.Close()
	if n := w.Devices(); n != 0 {
		t.Errorf("Devices after Close = %d, want 0", n)
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
