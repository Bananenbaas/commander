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

func (c *clock) now() time.Time            { return c.t }
func (c *clock) add(d time.Duration)       { c.t = c.t.Add(d) }
func newClock() *clock                     { return &clock{t: time.Date(2026, 11, 22, 20, 0, 0, 0, time.UTC)} }
func streamFrame(b0, b1 byte) (f [64]byte) { f[0], f[1] = b0, b1; return f }
func filled(n int, b byte) []byte          { return slices.Repeat([]byte{b}, n) }

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

func TestTraceCollapsesRepeatedStreamWrites(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	d := tr.Wrap(&Fake{})
	for i := 0; i < 30; i++ {
		for bank := byte(1); bank <= 6; bank++ {
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
	if es[1].first.Equal(es[1].last) {
		t.Error("a collapsed entry keeps the time of its first and last call")
	}
}

func TestTraceErrorBreaksACollapsedRun(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	f := &Fake{}
	f.FailAt(OpWriteStream, 3, errors.New("device: write failed"))
	d := tr.Wrap(f)
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
	d := tr.Wrap(&Fake{})
	for i := 0; i < 300; i++ { // ten seconds at 30 a second
		d.WriteStream(streamFrame(0xa1, 0x01))
		c.add(time.Second / 30)
	}
	if n := len(tr.entries()); n != 3 {
		t.Errorf("%d entries, want the open marker and two of five seconds", n)
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
	d := tr.Wrap(f)

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
		{kindInput, [3]byte{0x33, 0xdd, 0xdd}, 3, 12},
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

func TestTraceRingWraps(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	d := tr.Wrap(&Fake{})
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
	f.QueueFeature([]byte{0x05, 0x01}, []byte{0x05, 0x01})
	f.FailAt(OpSetFeature, 2, fmt.Errorf("%w: the semaphore timeout period has expired", ErrTimeout))
	d := tr.Wrap(f)
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
		"trace -5000ms mark opened",
		"trace -4000ms set 0x06 4B 0ms ok",
		"trace -3000ms get 0x05 4B 0ms ok",
		"trace -3000ms mark program StarCitizen.exe ended",
		"trace -2500..-1500ms stream a1 01 64B x3 0ms ok",
		"trace: end",
	}
	if !slices.Equal(log, want) {
		t.Errorf("dump\n got %q\nwant %q", log, want)
	}

	// The first control timeout after a success writes the trace by itself.
	d.GetFeature(0x05, 4)
	log = nil
	d.SetFeature([]byte{0x06, 1, 2, 3})
	if len(log) == 0 || !strings.HasPrefix(log[0], "trace: set feature 0x06 timed out; failures: set 1,") {
		t.Fatalf("no dump at the control timeout: %q", log)
	}
	if got := log[len(log)-2]; got != "trace -0ms set 0x06 4B 0ms device: timed out: the semaphore timeout period has expired" {
		t.Errorf("last entry %q", got)
	}
}

// The trace is written once per episode: not again until a control transfer
// succeeded in between.
func TestTraceDumpsOncePerEpisode(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	f := &Fake{}
	f.FailAt(OpSetFeature, 2, ErrTimeout)
	f.FailAt(OpSetFeature, 3, ErrTimeout)
	f.FailAt(OpSetFeature, 5, ErrTimeout)
	d := tr.Wrap(f)
	headers := func() int {
		n := 0
		for _, l := range log {
			if strings.HasPrefix(l, "trace: ") && l != "trace: end" {
				n++
			}
		}
		return n
	}
	d.SetFeature([]byte{0x06, 0}) // ok
	d.SetFeature([]byte{0x06, 0}) // timeout: dump
	d.SetFeature([]byte{0x06, 0}) // timeout again: same episode
	tr.Dump("the keyboard does not answer")
	if n := headers(); n != 1 {
		t.Fatalf("%d dumps in one episode, want 1", n)
	}
	d.SetFeature([]byte{0x06, 0}) // ok: the episode ends
	d.SetFeature([]byte{0x06, 0}) // timeout: a new one
	if n := headers(); n != 2 {
		t.Errorf("%d dumps, want 2", n)
	}
}

// A control timeout before any success is not the first after a success: it
// is left to the run loop's report.
func TestTraceNoDumpWithoutAnEarlierSuccess(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	f := &Fake{}
	f.FailAt(OpSetFeature, 1, ErrTimeout)
	tr.Wrap(f).SetFeature([]byte{0x06, 0})
	if len(log) != 0 {
		t.Errorf("logged %q", log)
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
	d := tr.Wrap(nopDevice{})
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
	if d := tr.Wrap(f); d != Device(f) {
		t.Error("a nil trace wraps the device")
	}
	tr.Mark("x")
	tr.Dump("y")
}

func TestTraceCloseIsMarked(t *testing.T) {
	c := newClock()
	var log []string
	tr := newTestTrace(c, &log)
	tr.Wrap(&Fake{}).Close()
	es := tr.entries()
	if len(es) != 2 || es[1].kind != kindMark || es[1].text != "closed" {
		t.Errorf("entries %+v", es)
	}
}
