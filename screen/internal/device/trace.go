// SPDX-License-Identifier: GPL-2.0-or-later

package device

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// traceSize is the number of entries a Trace keeps.
const traceSize = 512

// Collapsing: a stream or input call that went as the same kind of call did
// shortly before joins that call's entry, if the entry is one of the last
// collapseBack, nothing that cannot be collapsed came after it and it spans
// less than collapseSpan. A set of lighting frames interleaves about a dozen
// kinds of call, and the span keeps some resolution in time: the ring holds
// a few minutes of a lighting stream.
const (
	collapseBack = 32
	collapseSpan = 5 * time.Second
	maxErrText   = 100
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

// traceEntry is one call, or a run of calls that went alike, or a marker.
type traceEntry struct {
	kind   traceKind
	result traceResult
	nhead  uint8   // bytes of head that are set
	head   [3]byte // the report ID, or the first two bytes of a stream frame
	n      int     // the length of the report or frame
	count  int     // calls in this entry
	first  time.Time
	last   time.Time
	dmin   time.Duration
	dmax   time.Duration
	text   string // the error of a failed call, or a marker's text
}

// collapsible reports whether later calls may join e.
func (e *traceEntry) collapsible() bool {
	return (e.kind == kindWrite || e.kind == kindRead || e.kind == kindInput) && e.result != resultFailed
}

// Trace keeps the last calls made to the keyboard, for the log when the
// keyboard stops answering. It never holds a payload: of every call only its
// kind, the report ID or the first two bytes of a stream frame, the length,
// the duration and the error are kept; of input reports the report ID, and
// for notification (0x03) and touch (0x33) reports the first three bytes.
// Stream and input calls that go alike are collapsed into one entry with
// their count.
//
// Calls are traced through Wrap. The first control transfer (SetFeature or
// GetFeature) that times out after one that succeeded writes the trace to
// the log; Dump writes it on request. Either writes it once per episode: not
// again until a control transfer succeeded.
//
// A nil *Trace traces nothing. Its methods are safe for concurrent use.
type Trace struct {
	logf func(string, ...any)
	now  func() time.Time

	mu      sync.Mutex
	ring    [traceSize]traceEntry
	head    int // index of the oldest entry
	size    int
	opened  time.Time // when Wrap was last called
	lastSet time.Time // the last SetFeature that succeeded
	lastGet time.Time // the last GetFeature that succeeded
	ctlOK   bool      // the last control transfer succeeded
	dumped  bool      // the trace was written since the last control transfer that succeeded
}

// NewTrace returns an empty trace that writes to logf (nil: nowhere).
func NewTrace(logf func(string, ...any)) *Trace {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Trace{logf: logf, now: time.Now}
}

// Wrap returns d with every call traced, and marks the opening. With a nil
// trace it returns d.
func (t *Trace) Wrap(d Device) Device {
	if t == nil {
		return d
	}
	now := t.now()
	t.mu.Lock()
	t.opened = now
	t.mark(now, "opened")
	t.mu.Unlock()
	return &traced{d: d, t: t}
}

// Mark puts a marker with text into the trace.
func (t *Trace) Mark(text string) {
	if t == nil {
		return
	}
	now := t.now()
	t.mu.Lock()
	t.mark(now, text)
	t.mu.Unlock()
}

// mark adds a marker. t.mu must be held.
func (t *Trace) mark(now time.Time, text string) {
	e := t.add()
	*e = traceEntry{kind: kindMark, count: 1, first: now, last: now, text: text}
}

// Dump writes the trace to the log, with why it is written, unless it was
// written already in this episode (see Trace).
func (t *Trace) Dump(reason string) {
	if t == nil {
		return
	}
	now := t.now()
	t.mu.Lock()
	if t.dumped {
		t.mu.Unlock()
		return
	}
	t.dumped = true
	lines := t.lines(reason, now)
	t.mu.Unlock()
	for _, l := range lines {
		t.logf("%s", l)
	}
}

// add returns the slot of a new entry, the oldest one when the ring is full.
// t.mu must be held.
func (t *Trace) add() *traceEntry {
	if t.size < len(t.ring) {
		t.size++
	} else {
		t.head = (t.head + 1) % len(t.ring)
	}
	return &t.ring[(t.head+t.size-1)%len(t.ring)]
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
	var reason string
	t.mu.Lock()
	if result != resultFailed && (kind == kindWrite || kind == kindRead || kind == kindInput) {
		if e := t.alike(kind, head, nhead, n, result, now); e != nil {
			e.count++
			e.last = now
			e.dmin, e.dmax = min(e.dmin, d), max(e.dmax, d)
			t.mu.Unlock()
			return
		}
	}
	e := t.add()
	*e = traceEntry{kind: kind, result: result, nhead: nhead, head: head, n: n, count: 1, first: start, last: start, dmin: d, dmax: d}
	if result == resultFailed {
		e.text = errText(err)
	}
	if kind == kindSet || kind == kindGet {
		switch {
		case err == nil:
			if kind == kindSet {
				t.lastSet = now
			} else {
				t.lastGet = now
			}
			t.ctlOK, t.dumped = true, false
		case errors.Is(err, ErrTimeout) && t.ctlOK && !t.dumped:
			t.ctlOK, t.dumped = false, true
			reason = fmt.Sprintf("%s feature 0x%02x timed out", kind, head[0])
		default:
			t.ctlOK = false
		}
	}
	var lines []string
	if reason != "" {
		lines = t.lines(reason, now)
	}
	t.mu.Unlock()
	for _, l := range lines {
		t.logf("%s", l)
	}
}

// alike returns the recent entry a call joins, nil if none (see
// collapseBack). t.mu must be held.
func (t *Trace) alike(kind traceKind, head [3]byte, nhead uint8, n int, result traceResult, now time.Time) *traceEntry {
	for i := 0; i < t.size && i < collapseBack; i++ {
		e := &t.ring[(t.head+t.size-1-i)%len(t.ring)]
		if !e.collapsible() {
			return nil
		}
		if e.kind == kind && e.head == head && e.nhead == nhead && e.n == n && e.result == result && now.Sub(e.first) < collapseSpan {
			return e
		}
	}
	return nil
}

// errText is err's text on one line, cut short.
func errText(err error) string {
	s := strings.Join(strings.Fields(err.Error()), " ")
	if len(s) > maxErrText {
		s = s[:maxErrText] + "..."
	}
	return s
}

// lines formats the trace for the log, times relative to now. t.mu must be
// held.
func (t *Trace) lines(reason string, now time.Time) []string {
	var fails [numKinds]int
	for i := 0; i < t.size; i++ {
		e := &t.ring[(t.head+i)%len(t.ring)]
		if e.result == resultFailed {
			fails[e.kind] += e.count
		}
	}
	ago := func(at time.Time) string {
		if at.IsZero() {
			return "never"
		}
		return fmt.Sprintf("-%dms", now.Sub(at).Milliseconds())
	}
	out := make([]string, 0, t.size+2)
	out = append(out, fmt.Sprintf("trace: %s; failures: set %d, get %d, stream %d, stream-in %d, input %d; last set ok %s, last get ok %s, opened %s; %d entries",
		reason, fails[kindSet], fails[kindGet], fails[kindWrite], fails[kindRead], fails[kindInput],
		ago(t.lastSet), ago(t.lastGet), ago(t.opened), t.size))
	for i := 0; i < t.size; i++ {
		out = append(out, t.ring[(t.head+i)%len(t.ring)].format(now))
	}
	return append(out, "trace: end")
}

// format is e as one line of the log, times relative to now.
func (e *traceEntry) format(now time.Time) string {
	var b strings.Builder
	b.WriteString("trace ")
	if e.count > 1 {
		fmt.Fprintf(&b, "-%d..-%dms ", now.Sub(e.first).Milliseconds(), now.Sub(e.last).Milliseconds())
	} else {
		fmt.Fprintf(&b, "-%dms ", now.Sub(e.first).Milliseconds())
	}
	b.WriteString(e.kind.String())
	if e.kind == kindMark {
		b.WriteString(" " + e.text)
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
		fmt.Fprintf(&b, " %d..%dms", lo, hi)
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
	d Device
	t *Trace
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
	case r[0] == notifyReport || r[0] == touchReport:
		nh = uint8(copy(h[:], r))
	default: // keyboard input: its ID only, never the keys
		h[0], nh = r[0], 1
	}
	x.t.record(kindInput, h, nh, len(r), start, err)
	return r, err
}

func (x *traced) Close() error {
	err := x.d.Close()
	text := "closed"
	if err != nil {
		text = "closed: " + errText(err)
	}
	x.t.Mark(text)
	return err
}
