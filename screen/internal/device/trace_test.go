// SPDX-License-Identifier: GPL-2.0-or-later

package device

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// clock is a settable clock for the trace.
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock               { return &clock{t: time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC)} }
func streamFrame(b0, b1 byte) (f [64]byte) {
	f[0], f[1] = b0, b1
	return f
}
func filled(n int, b byte) []byte { return slices.Repeat([]byte{b}, n) }

// newTestTrace returns a trace on c that logs into *log.
func newTestTrace(c *clock, log *[]string) *Trace {
	t := NewTrace(func(format string, args ...any) { *log = append(*log, fmt.Sprintf(format, args...)) })
	t.now = c.now
	return t
}

// entries returns the trace's entries, oldest first.
func (t *Trace) entries() []traceEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]traceEntry, 0, t.size)
	for i := 0; i < t.size; i++ {
		out = append(out, t.ring[(t.head+i)%len(t.ring)])
	}
	return out
}

// headers returns the header lines of the trace writes in log.
func headers(log []string) []string {
	var out []string
	for _, l := range log {
		if strings.HasPrefix(l, "trace: ") && l != "trace: end" {
			out = append(out, l)
		}
	}
	return out
}

// slowDevice is a Device whose stream writes take *delay on the clock.
type slowDevice struct {
	nopDevice
	c     *clock
	delay time.Duration
}

func (s *slowDevice) WriteStream([64]byte) error {
	s.c.add(s.delay)
	return nil
}

func TestTraceCollapsesRepeatedStreamWrites(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	d := tr.Wrap(&Fake{}, "run")
	var lastStart time.Time
	for i := 0; i < 30; i++ {
		for bank := byte(1); bank <= 6; bank++ {
			if bank == 1 {
				lastStart = c.now()
			}
			if err := d.WriteStream(streamFrame(0xa1, bank)); err != nil {
				t.Fatal(err)
			}
			c.add(time.Millisecond)
		}
		d.ReadInput(0) // a timeout, as between two sets
		c.add(27 * time.Millisecond)
	}
	es := tr.entries()
	// The open marker, six banks and the input timeouts.
	if len(es) != 8 {
		t.Fatalf("%d entries, want 8: %+v", len(es), es)
	}
	for i, e := range es[1:7] {
		if e.kind != kindWrite || e.head != [3]byte{0xa1, byte(i + 1)} || e.count != 30 || e.n != 64 {
			t.Errorf("entry %d: %+v, want bank %d written 30 times", i+1, e, i+1)
		}
	}
	if e := es[7]; e.kind != kindInput || e.count != 30 || e.result != resultTimeout {
		t.Errorf("input entry %+v, want 30 timeouts", e)
	}
	if !es[1].last.Equal(lastStart) {
		t.Errorf("last %v, want the start of the last call %v", es[1].last, lastStart)
	}
}

func TestTraceErrorBreaksACollapsedRun(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	f := &Fake{}
	f.FailAt(OpWriteStream, 3, errors.New("device: write failed"))
	d := tr.Wrap(f, "run")
	for i := 0; i < 5; i++ {
		d.WriteStream(streamFrame(0xa1, 0x01))
	}
	es := tr.entries()[1:]
	var got []string
	for _, e := range es {
		got = append(got, fmt.Sprintf("%d %s", e.count, e.result))
	}
	want := []string{"2 ok", "1 failed", "2 ok"}
	if !slices.Equal(got, want) {
		t.Errorf("entries %q, want %q", got, want)
	}
	if es[1].text != "device: write failed" {
		t.Errorf("error text %q", es[1].text)
	}
}

func TestTraceCollapsedEntriesSpanAtMostFiveSeconds(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	d := tr.Wrap(&Fake{}, "run")
	for i := 0; i < 300; i++ { // ten seconds at 30 a second
		d.WriteStream(streamFrame(0xa1, 0x01))
		c.add(time.Second / 30)
	}
	if n := len(tr.entries()); n != 3 {
		t.Errorf("%d entries, want the open marker and two of five seconds", n)
	}
}

// A call much slower than the ones before gets a line of its own, and the
// slowest call of an entry is shown with its time.
func TestTraceSlowCallStandsOut(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	s := &slowDevice{c: c, delay: time.Millisecond}
	d := tr.Wrap(s, "run")
	for i := 0; i < 10; i++ {
		d.WriteStream(streamFrame(0xa1, 0x01))
	}
	s.delay = 30 * time.Millisecond
	d.WriteStream(streamFrame(0xa1, 0x01))
	es := tr.entries()[1:]
	if len(es) != 2 || es[0].count != 10 || es[1].count != 1 || es[1].dmax != 30*time.Millisecond {
		t.Fatalf("entries %+v, want ten fast calls and the slow one on its own", es)
	}

	// Slower, but not by much: it joins and is the slowest.
	s.delay = 50 * time.Millisecond
	d.WriteStream(streamFrame(0xa1, 0x01))
	c.add(time.Second)
	log = nil
	tr.Dump("test")
	if got := log[len(log)-2]; got != "trace -1080..-1050ms stream a1 01 64B x2 30..50ms slowest -1050ms ok" {
		t.Errorf("line %q", got)
	}
}

func TestTraceStoresNoPayload(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	f := &Fake{}
	reply := filled(64, 0xee)
	reply[0] = 0x05
	f.QueueFeature(reply)
	in := filled(12, 0xdd)
	in[0] = 0x33
	note := filled(8, 0xcc)
	note[0] = 0x03
	key := filled(8, 0xbb)
	key[0] = 0x01 // a keyboard report: nothing beyond its ID may be kept
	f.QueueInput(in, note, key)
	back := filled(64, 0x99)
	back[0], back[1] = 0xa5, 0xf1
	f.QueueStream([64]byte(back))
	d := tr.Wrap(f, "run")

	set := filled(1044, 0xaa)
	set[0] = 0x06
	d.SetFeature(set)
	d.GetFeature(0x05, 64)
	out := [64]byte(filled(64, 0x77))
	out[0], out[1] = 0xa3, 0x00
	d.WriteStream(out)
	d.ReadStream(0)
	d.ReadInput(0)
	d.ReadInput(0)
	d.ReadInput(0)

	want := []struct {
		kind  traceKind
		head  [3]byte
		nhead int
		n     int
	}{
		{kindSet, [3]byte{0x06}, 1, 1044},
		{kindGet, [3]byte{0x05}, 1, 64},
		{kindWrite, [3]byte{0xa3, 0x00}, 2, 64},
		{kindRead, [3]byte{0xa5, 0xf1}, 2, 64},
		{kindInput, [3]byte{0x33}, 1, 12},
		{kindInput, [3]byte{0x03, 0xcc, 0xcc}, 3, 8},
		{kindInput, [3]byte{0x01}, 1, 8},
	}
	es := tr.entries()[1:]
	if len(es) != len(want) {
		t.Fatalf("%d entries, want %d", len(es), len(want))
	}
	for i, w := range want {
		e := es[i]
		if e.kind != w.kind || e.head != w.head || int(e.nhead) != w.nhead || e.n != w.n {
			t.Errorf("entry %d: kind %v head % x (%d bytes) n %d, want kind %v head % x (%d bytes) n %d",
				i, e.kind, e.head, e.nhead, e.n, w.kind, w.head, w.nhead, w.n)
		}
		if e.text != "" {
			t.Errorf("entry %d keeps text %q", i, e.text)
		}
	}
}

// A touch report's page, position and key code tell which key was tapped:
// none of them may reach the ring or the log.
func TestTraceKeepsNoTouchPosition(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	f := &Fake{}
	f.QueueInput([]byte{0x33, 0xab, 0xcd, 0xef, 0x9e, 0x8d, 0x7c, 0x6b, 0x5a, 0x4f, 0x3e, 0x2d})
	d := tr.Wrap(f, "run")
	if r, err := d.ReadInput(0); err != nil || len(r) != 12 {
		t.Fatalf("ReadInput: % x, %v", r, err)
	}
	e := tr.entries()[1]
	if e.head != [3]byte{0x33} || e.nhead != 1 {
		t.Errorf("stored % x (%d bytes), want only the report ID", e.head, e.nhead)
	}
	tr.Dump("test")
	all := strings.Join(log, "\n")
	for _, b := range []string{"ab", "cd", "ef", "9e", "8d", "7c", "6b", "5a", "4f", "3e", "2d"} {
		if strings.Contains(all, " "+b) {
			t.Errorf("the log shows byte %s: %q", b, log)
		}
	}
	if !strings.Contains(all, "input 33 12B") {
		t.Errorf("the touch is not in the log: %q", log)
	}
}

func TestTraceRingWraps(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	d := tr.Wrap(&Fake{}, "run")
	for i := 0; i < traceSize+10; i++ {
		d.SetFeature([]byte{byte(i), 0})
	}
	es := tr.entries()
	if len(es) != traceSize {
		t.Fatalf("%d entries, want %d", len(es), traceSize)
	}
	// The open marker and the first ten reports are gone.
	last := traceSize + 9
	if es[0].head[0] != 10 || es[traceSize-1].head[0] != byte(last) {
		t.Errorf("oldest 0x%02x, newest 0x%02x", es[0].head[0], es[traceSize-1].head[0])
	}
}

func TestTraceDumpFormat(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	f := &Fake{}
	f.QueueFeature([]byte{0x05, 0x01})
	d := tr.Wrap(f, "run")
	c.add(time.Second)
	d.SetFeature([]byte{0x06, 1, 2, 3}) // -4000ms
	c.add(time.Second)
	d.GetFeature(0x05, 4) // -3000ms
	tr.Mark("program StarCitizen.exe ended")
	for i := 0; i < 3; i++ {
		c.add(500 * time.Millisecond)
		d.WriteStream(streamFrame(0xa1, 0x01)) // -2500..-1500ms
	}
	c.add(1500 * time.Millisecond)
	log = nil
	tr.Dump("the keyboard does not answer")
	want := []string{
		"trace: the keyboard does not answer; failures: set 0, get 0, stream 0, stream-in 0, input 0; last set ok -4000ms, last get ok -3000ms, opened -5000ms; 5 entries",
		"trace marks: -5000ms opened (run); -3000ms program StarCitizen.exe ended",
		"trace -5000ms mark opened (run)",
		"trace -4000ms set 0x06 4B 0ms ok",
		"trace -3000ms get 0x05 4B 0ms ok",
		"trace -3000ms mark program StarCitizen.exe ended",
		"trace -2500..-1500ms stream a1 01 64B x3 0ms ok",
		"trace: end",
	}
	if !slices.Equal(log, want) {
		t.Errorf("dump\n got %q\nwant %q", log, want)
	}
}

// The first failed control transfer after a success writes the trace by
// itself, whatever the error: Windows reports a semaphore timeout as an
// error of its own.
func TestTraceFailedControlTransferWritesTheTrace(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	f := &Fake{}
	f.FailAt(OpSetFeature, 2, errors.New("device: set feature report 0x06: The semaphore timeout period has expired."))
	d := tr.Wrap(f, "run")
	d.SetFeature([]byte{0x06, 1, 2, 3})
	d.SetFeature([]byte{0x06, 1, 2, 3})
	h := headers(log)
	if len(h) != 1 || !strings.HasPrefix(h[0], "trace: set feature 0x06 failed; failures: set 1,") {
		t.Fatalf("headers %q", h)
	}
	if got := log[len(log)-2]; got != "trace -0ms set 0x06 4B 0ms device: set feature report 0x06: The semaphore timeout period has expired." {
		t.Errorf("last entry %q", got)
	}
}

// A failed control transfer before any success is left to the run loop's
// report.
func TestTraceNoDumpWithoutAnEarlierSuccess(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	f := &Fake{}
	f.FailAt(OpSetFeature, 1, ErrTimeout)
	tr.Wrap(f, "run").SetFeature([]byte{0x06, 0})
	if len(log) != 0 {
		t.Errorf("logged %q", log)
	}
}

// The whole trace is written once per episode, which only Answered ends, and
// at most once per five minutes. A request in an episode that had its whole
// trace, or within the five minutes, writes the entries since the last write,
// once per episode.
func TestTraceWritesOncePerEpisode(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	f := &Fake{}
	for _, n := range []int{2, 4, 6, 8} {
		f.FailAt(OpSetFeature, n, ErrTimeout)
	}
	d := tr.Wrap(f, "run")
	pair := func() {
		d.SetFeature([]byte{0x06, 0}) // ok
		d.SetFeature([]byte{0x06, 0}) // fails
	}
	pair() // the whole trace
	if h := headers(log); len(h) != 1 {
		t.Fatalf("headers %q", h)
	}
	pair() // a success is not the end of the episode
	if h := headers(log); len(h) != 1 {
		t.Fatalf("headers %q", h)
	}

	log = nil
	c.add(time.Second)
	tr.Dump("the keyboard does not answer")
	want := []string{
		"trace: the keyboard does not answer; entries since the trace written -1000ms; failures: set 1, get 0, stream 0, stream-in 0, input 0; 2 entries",
		"trace marks: -1000ms opened (run)",
		"trace -1000ms set 0x06 2B 0ms ok",
		"trace -1000ms set 0x06 2B 0ms device: timed out",
		"trace: end",
	}
	if !slices.Equal(log, want) {
		t.Errorf("dump\n got %q\nwant %q", log, want)
	}
	log = nil
	tr.Dump("again")
	if len(log) != 0 {
		t.Errorf("written twice in an episode: %q", log)
	}

	tr.Answered()
	pair() // a new episode, but within five minutes: nothing
	if len(log) != 0 {
		t.Errorf("written within five minutes: %q", log)
	}
	tr.Dump("the keyboard does not answer")
	if h := headers(log); len(h) != 1 || !strings.Contains(h[0], "entries since the trace written") {
		t.Errorf("headers %q, want the entries since", h)
	}

	log = nil
	c.add(5 * time.Minute)
	tr.Answered()
	pair()
	if h := headers(log); len(h) != 1 || !strings.HasPrefix(h[0], "trace: set feature 0x06 failed; failures: set 4,") {
		t.Errorf("headers %q, want the whole trace again", h)
	}
}

// Calls after a write never join an entry written already.
func TestTraceWriteEndsCollapsing(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	d := tr.Wrap(&Fake{}, "run")
	d.WriteStream(streamFrame(0xa1, 0x01))
	tr.Dump("first")
	d.WriteStream(streamFrame(0xa1, 0x01))
	if n := len(tr.entries()); n != 3 {
		t.Errorf("%d entries, want 3", n)
	}
}

// The last markers stay in the header after the ring has wrapped, and a
// marker repeated is counted.
func TestTraceKeepsTheLastMarkers(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	d := tr.Wrap(&Fake{}, "run")
	tr.Mark("program StarCitizen.exe ended")
	c.add(time.Second)
	tr.Mark("x")
	c.add(time.Second)
	tr.Mark("x")
	for i := 0; i < traceSize; i++ {
		d.SetFeature([]byte{0x06, 0})
	}
	c.add(time.Second)
	tr.Dump("test")
	if want := "trace marks: -3000ms opened (run); -3000ms program StarCitizen.exe ended; -2000..-1000ms x x2"; log[1] != want {
		t.Errorf("got %q, want %q", log[1], want)
	}
	for i := 0; i < keptMarks+2; i++ {
		tr.Mark(fmt.Sprintf("m%d", i))
	}
	tr.Answered()
	log = nil
	c.add(5 * time.Minute)
	tr.Dump("test")
	if !strings.HasPrefix(log[1], "trace marks: -300000ms m2; ") || strings.Count(log[1], "; ") != keptMarks-1 {
		t.Errorf("marks %q, want the last %d", log[1], keptMarks)
	}
}

func TestTraceOpen(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	if _, err := tr.Open("apply", func() (Device, error) { return nil, ErrNotFound }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Open: %v", err)
	}
	if _, err := tr.Open("apply", func() (Device, error) { return nil, ErrNotFound }); err == nil {
		t.Fatal("Open: no error")
	}
	f := &Fake{}
	d, err := tr.Open("apply", func() (Device, error) { return f, nil })
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	es := tr.entries()
	var got []string
	for _, e := range es {
		got = append(got, fmt.Sprintf("%s x%d", e.text, e.count))
	}
	want := []string{"open failed (apply): device: KB7 not found x2", "opened (apply) x1", "closed (apply) x1"}
	if !slices.Equal(got, want) {
		t.Errorf("markers %q, want %q", got, want)
	}
	if !f.Closed() {
		t.Error("the device was not closed")
	}
}

// nopDevice answers every call at once, for the allocation test.
type nopDevice struct{}

func (nopDevice) SetFeature([]byte) error                    { return nil }
func (nopDevice) GetFeature(byte, int) ([]byte, error)       { return nil, nil }
func (nopDevice) WriteStream([64]byte) error                 { return nil }
func (nopDevice) ReadStream(time.Duration) ([64]byte, error) { return [64]byte{0xa1, 0x01}, nil }
func (nopDevice) ReadInput(time.Duration) ([]byte, error)    { return nil, ErrTimeout }
func (nopDevice) Close() error                               { return nil }

func TestTraceStreamDoesNotAllocate(t *testing.T) {
	tr := NewTrace(nil)
	d := tr.Wrap(nopDevice{}, "run")
	f := streamFrame(0xa1, 0x01)
	allocs := testing.AllocsPerRun(1000, func() {
		d.WriteStream(f)
		d.ReadStream(time.Millisecond)
		d.ReadInput(time.Millisecond)
	})
	if allocs != 0 {
		t.Errorf("%v allocations per frame", allocs)
	}
}

func TestTraceNilWrapsNothing(t *testing.T) {
	var tr *Trace
	f := &Fake{}
	if d := tr.Wrap(f, "run"); d != Device(f) {
		t.Error("a nil trace wraps the device")
	}
	if d, err := tr.Open("run", func() (Device, error) { return f, nil }); err != nil || d != Device(f) {
		t.Error("a nil trace wraps the device")
	}
	tr.Mark("x")
	tr.Dump("y")
	tr.Answered()
}
