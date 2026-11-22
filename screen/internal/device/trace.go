// SPDX-License-Identifier: GPL-2.0-or-later

package device

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// traceSize is the number of entries a Trace keeps.
	traceSize = 512
	// keptMarks is the number of the last markers kept outside the ring, for
	// the header of a write.
	keptMarks = 8
	// dumpEvery is the shortest time between two writes of the whole trace.
	dumpEvery = 5 * time.Minute
	maxText   = 100
)

// Collapsing: a stream or input call that went as the same kind of call did
// shortly before joins that call's entry, if the entry is one of the last
// collapseBack, nothing that cannot be collapsed came after it, it was not
// written to the log yet and it spans less than collapseSpan. A set of
// lighting frames interleaves about a dozen kinds of call, and the span keeps
// some resolution in time: the ring holds a few minutes of a lighting stream.
// A call more than slowFactor times as slow as the entry's slowest, and
// slower than slowMin, starts an entry of its own.
const (
	collapseBack = 32
	collapseSpan = 5 * time.Second
	slowFactor   = 4
	slowMin      = 20 * time.Millisecond
)

// traceKind is the kind of a trace entry.
type traceKind uint8

const (
	kindSet   traceKind = iota // SetFeature
	kindGet                    // GetFeature
	kindWrite                  // WriteStream
	kindRead                   // ReadStream
	kindInput                  // ReadInput
	kindMark                   // a marker; text says what happened
	numKinds
)

var kindNames = [numKinds]string{"set", "get", "stream", "stream-in", "input", "mark"}

func (k traceKind) String() string {
	if k < numKinds {
		return kindNames[k]
	}
	return fmt.Sprintf("kind(%d)", uint8(k))
}

// traceResult is how a traced call went.
type traceResult uint8

const (
	resultOK      traceResult = iota
	resultTimeout             // a stream or input read that got nothing in time
	resultFailed              // any other error; text holds it
)

func (r traceResult) String() string {
	switch r {
	case resultOK:
		return "ok"
	case resultTimeout:
		return "timeout"
	}
	return "failed"
}

// traceEntry is one call, or a run of calls that went alike, or a marker
// (repeated markers are counted too).
type traceEntry struct {
	kind   traceKind
	result traceResult
	nhead  uint8   // bytes of head that are set
	head   [3]byte // the report ID, or the first two bytes of a stream frame
	n      int     // the length of the report or frame
	count  int     // calls in this entry
	seq    uint64  // the entry's number, counting from 1
	first  time.Time
	last   time.Time // the start of the last call
	slowAt time.Time // the start of the slowest call
	dmin   time.Duration
	dmax   time.Duration
	text   string // the error of a failed call, or a marker's text
}

// collapsible reports whether later calls may join e.
func (e *traceEntry) collapsible() bool {
	return (e.kind == kindWrite || e.kind == kindRead || e.kind == kindInput) && e.result != resultFailed
}

// keptMark is one of the last markers.
type keptMark struct {
	first, last time.Time
	text        string
	count       int
}

// Trace keeps the last calls made to the keyboard, for the log when the
// keyboard stops answering. It never holds a payload: of every call only its
// kind, the report ID or the first two bytes of a stream frame, the length,
// the duration and the error are kept. Of input reports only the report ID
// is kept, and for notification reports (0x03) the first three bytes, which
// say what kind of notification it is; the page and position of a touch
// report would tell which key was tapped. Stream and input calls that go
// alike are collapsed into one entry with their count.
//
// Calls are traced through Wrap or Open. The first control transfer
// (SetFeature or GetFeature) that fails after one that succeeded writes the
// whole trace to the log, and so does Dump; that happens once per episode,
// which only Answered ends, and at most once per dumpEvery. A Dump that may
// not write the whole trace writes the entries since the last write instead,
// once per episode.
//
// A nil *Trace traces nothing. Its methods are safe for concurrent use.
type Trace struct {
	logf func(string, ...any)
	now  func() time.Time

	mu      sync.Mutex
	ring    [traceSize]traceEntry
	head    int    // index of the oldest entry
	size    int    // entries in the ring
	seq     uint64 // entries added so far
	marks   [keptMarks]keptMark
	nmarks  int       // markers kept, the newest last
	opened  time.Time // when Wrap was last called
	lastSet time.Time // the last SetFeature that succeeded
	lastGet time.Time // the last GetFeature that succeeded
	ctlOK   bool      // the last control transfer succeeded

	written   uint64    // seq of the newest entry at the last write
	wroteAt   time.Time // the last write
	fullAt    time.Time // the last write of the whole trace
	fullDone  bool      // the whole trace was written in this episode
	sinceDone bool      // the entries since the last write were written in this episode
}

// NewTrace returns an empty trace that writes to logf (nil: nowhere).
func NewTrace(logf func(string, ...any)) *Trace {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Trace{logf: logf, now: time.Now}
}

// Wrap returns d with every call traced, and marks the opening with label,
// which says who opened it. With a nil trace it returns d.
func (t *Trace) Wrap(d Device, label string) Device {
	if t == nil {
		return d
	}
	now := t.now()
	t.mu.Lock()
	t.opened = now
	t.mark(now, "opened ("+label+")")
	t.mu.Unlock()
	return &traced{d: d, t: t, label: label}
}

// Open calls open and returns its device wrapped (see Wrap). A failure is
// marked.
func (t *Trace) Open(label string, open func() (Device, error)) (Device, error) {
	d, err := open()
	if err != nil {
		t.Mark(fmt.Sprintf("open failed (%s): %v", label, err))
		return nil, err
	}
	return t.Wrap(d, label), nil
}

// Mark puts a marker with text into the trace. It is kept on one line and
// cut short.
func (t *Trace) Mark(text string) {
	if t == nil {
		return
	}
	now := t.now()
	text = oneLine(text)
	t.mu.Lock()
	t.mark(now, text)
	t.mu.Unlock()
}

// mark adds a marker, or counts it when it repeats the newest entry. t.mu
// must be held.
func (t *Trace) mark(now time.Time, text string) {
	if t.size > 0 {
		if e := t.newest(); e.kind == kindMark && e.text == text && e.seq > t.written {
			e.count++
			e.last = now
			t.keepMark(now, text)
			return
		}
	}
	e := t.add()
	*e = traceEntry{kind: kindMark, count: 1, seq: e.seq, first: now, last: now, slowAt: now, text: text}
	t.keepMark(now, text)
}

// keepMark keeps a marker among the last keptMarks. t.mu must be held.
func (t *Trace) keepMark(now time.Time, text string) {
	if t.nmarks > 0 {
		if m := &t.marks[t.nmarks-1]; m.text == text {
			m.count++
			m.last = now
			return
		}
	}
	if t.nmarks == len(t.marks) {
		copy(t.marks[:], t.marks[1:])
		t.nmarks--
	}
	t.marks[t.nmarks] = keptMark{first: now, last: now, text: text, count: 1}
	t.nmarks++
}

// Answered ends the episode: the keyboard has been opened and has confirmed
// a switch.
func (t *Trace) Answered() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.fullDone, t.sinceDone = false, false
	t.mu.Unlock()
}

// Dump writes the trace to the log, with why it is written (see Trace).
func (t *Trace) Dump(reason string) {
	if t == nil {
		return
	}
	now := t.now()
	t.mu.Lock()
	var lines []string
	switch {
	case t.fullAllowed(now):
		lines = t.writeFull(reason, now)
	case !t.sinceDone:
		lines = t.writeSince(reason, now)
	}
	t.mu.Unlock()
	t.log(lines)
}

func (t *Trace) log(lines []string) {
	for _, l := range lines {
		t.logf("%s", l)
	}
}

// fullAllowed reports whether the whole trace may be written. t.mu must be
// held.
func (t *Trace) fullAllowed(now time.Time) bool {
	return !t.fullDone && (t.fullAt.IsZero() || now.Sub(t.fullAt) >= dumpEvery)
}

// writeFull returns the whole trace for the log and notes the write. t.mu
// must be held.
func (t *Trace) writeFull(reason string, now time.Time) []string {
	lines := t.lines(reason, now, t.size, false)
	t.fullDone, t.fullAt = true, now
	t.wrote(now)
	return lines
}

// writeSince returns the entries since the last write for the log and notes
// the write. t.mu must be held.
func (t *Trace) writeSince(reason string, now time.Time) []string {
	n := int(min(t.seq-t.written, uint64(t.size)))
	lines := t.lines(reason, now, n, true)
	t.sinceDone = true
	t.wrote(now)
	return lines
}

// wrote notes a write: later calls do not join the entries written. t.mu
// must be held.
func (t *Trace) wrote(now time.Time) {
	t.written, t.wroteAt = t.seq, now
}

// newest returns the newest entry; the ring must not be empty. t.mu must be
// held.
func (t *Trace) newest() *traceEntry {
	return &t.ring[(t.head+t.size-1)%len(t.ring)]
}

// add returns the slot of a new entry, the oldest one when the ring is full,
// with its seq set. t.mu must be held.
func (t *Trace) add() *traceEntry {
	if t.size < len(t.ring) {
		t.size++
	} else {
		t.head = (t.head + 1) % len(t.ring)
	}
	t.seq++
	e := t.newest()
	e.seq = t.seq
	return e
}

// record adds one call that started at start and returned err.
func (t *Trace) record(kind traceKind, head [3]byte, nhead uint8, n int, start time.Time, err error) {
	now := t.now()
	d := now.Sub(start)
	result := resultOK
	switch {
	case err == nil:
	case (kind == kindRead || kind == kindInput) && errors.Is(err, ErrTimeout):
		result = resultTimeout
	default:
		result = resultFailed
	}
	t.mu.Lock()
	if result != resultFailed && (kind == kindWrite || kind == kindRead || kind == kindInput) {
		if e := t.alike(kind, head, nhead, n, result, start); e != nil && (d <= slowFactor*e.dmax || d <= slowMin) {
			e.count++
			e.last = start
			if d > e.dmax {
				e.dmax, e.slowAt = d, start
			}
			e.dmin = min(e.dmin, d)
			t.mu.Unlock()
			return
		}
	}
	e := t.add()
	*e = traceEntry{kind: kind, result: result, nhead: nhead, head: head, n: n, count: 1, seq: e.seq,
		first: start, last: start, slowAt: start, dmin: d, dmax: d}
	if result == resultFailed {
		e.text = oneLine(err.Error())
	}
	var lines []string
	if kind == kindSet || kind == kindGet {
		switch {
		case err == nil:
			if kind == kindSet {
				t.lastSet = now
			} else {
				t.lastGet = now
			}
			t.ctlOK = true
		default:
			if t.ctlOK && t.fullAllowed(now) {
				lines = t.writeFull(fmt.Sprintf("%s feature 0x%02x failed", kind, head[0]), now)
			}
			t.ctlOK = false
		}
	}
	t.mu.Unlock()
	t.log(lines)
}

// alike returns the recent entry a call that started at start joins, nil if
// none (see collapseBack). t.mu must be held.
func (t *Trace) alike(kind traceKind, head [3]byte, nhead uint8, n int, result traceResult, start time.Time) *traceEntry {
	for i := 0; i < t.size && i < collapseBack; i++ {
		e := &t.ring[(t.head+t.size-1-i)%len(t.ring)]
		if !e.collapsible() || e.seq <= t.written {
			return nil
		}
		if e.kind == kind && e.head == head && e.nhead == nhead && e.n == n && e.result == result && start.Sub(e.first) < collapseSpan {
			return e
		}
	}
	return nil
}

// oneLine is s on one line, cut short.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxText {
		s = s[:maxText] + "..."
	}
	return s
}

// ago is how long before now at was, for the log.
func ago(now, at time.Time) string {
	if at.IsZero() {
		return "never"
	}
	return fmt.Sprintf("-%dms", now.Sub(at).Milliseconds())
}

// span is the time of a run from first to last, for the log.
func span(now, first, last time.Time, count int) string {
	if count > 1 {
		return fmt.Sprintf("-%d..-%dms", now.Sub(first).Milliseconds(), now.Sub(last).Milliseconds())
	}
	return ago(now, first)
}

// lines formats the newest n entries for the log, times relative to now;
// since says they are the entries since the last write. t.mu must be held.
func (t *Trace) lines(reason string, now time.Time, n int, since bool) []string {
	from := t.size - n
	var fails [numKinds]int
	for i := from; i < t.size; i++ {
		e := &t.ring[(t.head+i)%len(t.ring)]
		if e.result == resultFailed {
			fails[e.kind] += e.count
		}
	}
	failures := fmt.Sprintf("failures: set %d, get %d, stream %d, stream-in %d, input %d",
		fails[kindSet], fails[kindGet], fails[kindWrite], fails[kindRead], fails[kindInput])
	out := make([]string, 0, n+3)
	if since {
		out = append(out, fmt.Sprintf("trace: %s; entries since the trace written %s; %s; %d entries",
			reason, ago(now, t.wroteAt), failures, n))
	} else {
		out = append(out, fmt.Sprintf("trace: %s; %s; last set ok %s, last get ok %s, opened %s; %d entries",
			reason, failures, ago(now, t.lastSet), ago(now, t.lastGet), ago(now, t.opened), n))
	}
	if t.nmarks > 0 {
		ms := make([]string, t.nmarks)
		for i, m := range t.marks[:t.nmarks] {
			ms[i] = span(now, m.first, m.last, m.count) + " " + m.text
			if m.count > 1 {
				ms[i] += fmt.Sprintf(" x%d", m.count)
			}
		}
		out = append(out, "trace marks: "+strings.Join(ms, "; "))
	}
	for i := from; i < t.size; i++ {
		out = append(out, t.ring[(t.head+i)%len(t.ring)].format(now))
	}
	return append(out, "trace: end")
}

// format is e as one line of the log, times relative to now.
func (e *traceEntry) format(now time.Time) string {
	var b strings.Builder
	b.WriteString("trace ")
	b.WriteString(span(now, e.first, e.last, e.count))
	b.WriteString(" " + e.kind.String())
	if e.kind == kindMark {
		b.WriteString(" " + e.text)
		if e.count > 1 {
			fmt.Fprintf(&b, " x%d", e.count)
		}
		return b.String()
	}
	switch {
	case e.kind == kindSet || e.kind == kindGet:
		fmt.Fprintf(&b, " 0x%02x", e.head[0])
	default:
		for _, h := range e.head[:e.nhead] {
			fmt.Fprintf(&b, " %02x", h)
		}
	}
	fmt.Fprintf(&b, " %dB", e.n)
	if e.count > 1 {
		fmt.Fprintf(&b, " x%d", e.count)
	}
	lo, hi := e.dmin.Milliseconds(), e.dmax.Milliseconds()
	if lo == hi {
		fmt.Fprintf(&b, " %dms", lo)
	} else {
		fmt.Fprintf(&b, " %d..%dms slowest %s", lo, hi, ago(now, e.slowAt))
	}
	if e.result == resultFailed {
		b.WriteString(" " + e.text)
	} else {
		b.WriteString(" " + e.result.String())
	}
	return b.String()
}

// traced is a Device whose calls go into a Trace.
type traced struct {
	d     Device
	t     *Trace
	label string
}

func (x *traced) SetFeature(b []byte) error {
	start := x.t.now()
	err := x.d.SetFeature(b)
	var h [3]byte
	var nh uint8
	if len(b) > 0 {
		h[0], nh = b[0], 1
	}
	x.t.record(kindSet, h, nh, len(b), start, err)
	return err
}

func (x *traced) GetFeature(id byte, n int) ([]byte, error) {
	start := x.t.now()
	r, err := x.d.GetFeature(id, n)
	x.t.record(kindGet, [3]byte{id}, 1, n, start, err)
	return r, err
}

func (x *traced) WriteStream(f [64]byte) error {
	start := x.t.now()
	err := x.d.WriteStream(f)
	x.t.record(kindWrite, [3]byte{f[0], f[1]}, 2, len(f), start, err)
	return err
}

func (x *traced) ReadStream(timeout time.Duration) ([64]byte, error) {
	start := x.t.now()
	f, err := x.d.ReadStream(timeout)
	var h [3]byte
	var nh uint8
	if err == nil {
		h, nh = [3]byte{f[0], f[1]}, 2
	}
	x.t.record(kindRead, h, nh, len(f), start, err)
	return f, err
}

func (x *traced) ReadInput(timeout time.Duration) ([]byte, error) {
	start := x.t.now()
	r, err := x.d.ReadInput(timeout)
	var h [3]byte
	var nh uint8
	switch {
	case len(r) == 0:
	case r[0] == notifyReport: // 03 00 <type>: no key data
		nh = uint8(copy(h[:], r))
	default: // touch and keyboard reports: the ID only, never which key
		h[0], nh = r[0], 1
	}
	x.t.record(kindInput, h, nh, len(r), start, err)
	return r, err
}

func (x *traced) Close() error {
	err := x.d.Close()
	text := "closed (" + x.label + ")"
	if err != nil {
		text += ": " + err.Error()
	}
	x.t.Mark(text)
	return err
}
