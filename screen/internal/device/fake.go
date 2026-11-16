package device

import (
	"bytes"
	"fmt"
	"sync"
	"time"
)

// fakeFeatureLen is the longest feature report the Fake takes: the length
// of the KB7's feature collection as Windows reports it, report ID included.
const fakeFeatureLen = 1044

// Op names a Device method, for Fake.FailAt and Call.
type Op string

// The Device methods.
const (
	OpSetFeature  Op = "SetFeature"
	OpGetFeature  Op = "GetFeature"
	OpWriteStream Op = "WriteStream"
	OpReadStream  Op = "ReadStream"
	OpReadInput   Op = "ReadInput"
	OpClose       Op = "Close"
)

// Call is one call made to a Fake.
type Call struct {
	Op Op
	// Data is what was sent (SetFeature, WriteStream) or returned
	// (GetFeature, ReadStream, ReadInput).
	Data []byte
	ID   byte  // GetFeature: the report ID asked for
	N    int   // GetFeature: the length asked for
	Err  error // what the call returned
}

// Fake is a Device for tests. It records every call and answers from
// replies queued beforehand; it never waits, so an empty queue gives
// ErrTimeout at once. The zero value is ready to use. Its methods are safe
// for concurrent use; set Echo before using it.
type Fake struct {
	// Echo makes WriteStream queue the answer the keyboard gives to the
	// frame, as the next ReadStream reply after those already queued: a
	// control frame (a5 00, a5 40-7f, a5 f1, a5 f2, a5 ff) comes back
	// unchanged, a data frame (a5 01-3e) is answered with 64 bytes of ff,
	// any other frame gets no answer.
	Echo bool

	mu       sync.Mutex
	calls    []Call
	features map[byte][][]byte
	stream   [][64]byte
	input    [][]byte
	count    map[Op]int
	fail     map[Op]map[int]error
	closed   bool
}

// QueueFeature queues replies for GetFeature, each under its own first
// byte, the report ID. A reply shorter than the length GetFeature asks for
// is padded with zeros.
func (f *Fake) QueueFeature(replies ...[]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.features == nil {
		f.features = make(map[byte][][]byte)
	}
	for _, r := range replies {
		if len(r) == 0 {
			panic("device.Fake: empty feature reply")
		}
		f.features[r[0]] = append(f.features[r[0]], bytes.Clone(r))
	}
}

// QueueStream queues frames for ReadStream, in order.
func (f *Fake) QueueStream(frames ...[64]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stream = append(f.stream, frames...)
}

// QueueInput queues reports for ReadInput, in order.
func (f *Fake) QueueInput(reports ...[]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range reports {
		f.input = append(f.input, bytes.Clone(r))
	}
}

// FailAt makes the nth call (counting from 1) of op return err instead of
// doing anything. A failed Close still closes the Fake.
func (f *Fake) FailAt(op Op, n int, err error) {
	switch op {
	case OpSetFeature, OpGetFeature, OpWriteStream, OpReadStream, OpReadInput, OpClose:
	default:
		panic(fmt.Sprintf("device.Fake: unknown op %q", op))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail == nil {
		f.fail = make(map[Op]map[int]error)
	}
	if f.fail[op] == nil {
		f.fail[op] = make(map[int]error)
	}
	f.fail[op][n] = err
}

// Calls returns every call made so far, in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	for i, c := range f.calls {
		c.Data = bytes.Clone(c.Data)
		out[i] = c
	}
	return out
}

// Features returns the reports SetFeature accepted, in order.
func (f *Fake) Features() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][]byte
	for _, c := range f.calls {
		if c.Op == OpSetFeature && c.Err == nil {
			out = append(out, bytes.Clone(c.Data))
		}
	}
	return out
}

// Frames returns the frames WriteStream accepted, in order.
func (f *Fake) Frames() [][64]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out [][64]byte
	for _, c := range f.calls {
		if c.Op == OpWriteStream && c.Err == nil {
			out = append(out, [64]byte(c.Data))
		}
	}
	return out
}

// Closed reports whether Close has been called.
func (f *Fake) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// begin counts a call of op and returns the error it must fail with, if
// any. f.mu must be held.
func (f *Fake) begin(op Op) error {
	if f.count == nil {
		f.count = make(map[Op]int)
	}
	f.count[op]++
	if f.closed {
		return ErrClosed
	}
	if err, ok := f.fail[op][f.count[op]]; ok {
		return err
	}
	return nil
}

func (f *Fake) record(c Call) {
	f.calls = append(f.calls, c)
}

func (f *Fake) SetFeature(b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := f.begin(OpSetFeature)
	if err == nil {
		err = checkFeatureLen(len(b), fakeFeatureLen)
	}
	f.record(Call{Op: OpSetFeature, Data: bytes.Clone(b), Err: err})
	return err
}

func (f *Fake) GetFeature(id byte, n int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	reply, err := f.getFeature(id, n)
	f.record(Call{Op: OpGetFeature, Data: bytes.Clone(reply), ID: id, N: n, Err: err})
	return reply, err
}

func (f *Fake) getFeature(id byte, n int) ([]byte, error) {
	if err := f.begin(OpGetFeature); err != nil {
		return nil, err
	}
	if err := checkFeatureLen(n, fakeFeatureLen); err != nil {
		return nil, err
	}
	q := f.features[id]
	if len(q) == 0 {
		return nil, ErrTimeout
	}
	if len(q[0]) > n {
		return nil, fmt.Errorf("device.Fake: reply for report 0x%02x is %d bytes, %d asked for", id, len(q[0]), n)
	}
	reply := make([]byte, n)
	copy(reply, q[0])
	f.features[id] = q[1:]
	return reply, nil
}

func (f *Fake) WriteStream(fr [64]byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := f.begin(OpWriteStream)
	f.record(Call{Op: OpWriteStream, Data: bytes.Clone(fr[:]), Err: err})
	if err == nil && f.Echo {
		if a, ok := keyboardAnswer(fr); ok {
			f.stream = append(f.stream, a)
		}
	}
	return err
}

func (f *Fake) ReadStream(time.Duration) ([64]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var fr [64]byte
	err := f.begin(OpReadStream)
	if err == nil {
		if len(f.stream) == 0 {
			err = ErrTimeout
		} else {
			fr, f.stream = f.stream[0], f.stream[1:]
		}
	}
	c := Call{Op: OpReadStream, Err: err}
	if err == nil {
		c.Data = bytes.Clone(fr[:])
	}
	f.record(c)
	return fr, err
}

func (f *Fake) ReadInput(time.Duration) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var r []byte
	err := f.begin(OpReadInput)
	if err == nil {
		if len(f.input) == 0 {
			err = ErrTimeout
		} else {
			r, f.input = f.input[0], f.input[1:]
		}
	}
	f.record(Call{Op: OpReadInput, Data: bytes.Clone(r), Err: err})
	return r, err
}

func (f *Fake) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := f.begin(OpClose)
	f.closed = true
	f.record(Call{Op: OpClose, Err: err})
	return err
}

// keyboardAnswer returns what the keyboard sends back for a stream frame,
// if anything: control frames are echoed, data frames answered with ff.
func keyboardAnswer(fr [64]byte) ([64]byte, bool) {
	if fr[0] != 0xa5 {
		return [64]byte{}, false
	}
	switch op := fr[1]; {
	case op == 0x00, op >= 0x40 && op <= 0x7f, op == 0xf1, op == 0xf2, op == 0xff:
		return fr, true
	case op >= 0x01 && op <= 0x3e:
		var a [64]byte
		for i := range a {
			a[i] = 0xff
		}
		return a, true
	}
	return [64]byte{}, false
}
