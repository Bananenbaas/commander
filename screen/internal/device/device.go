// Package device is the only code that talks to the operating system's HID
// stack. It finds the KB7, opens the parts of it kb7screen uses and moves
// feature reports, stream frames and input reports; it does not know what
// they mean.
//
// The KB7 is used through two USB interfaces:
//
//   - interface 1, usage page 0xff00: the stream, 64-byte frames in both
//     directions, without a report ID;
//   - interface 2: every feature report (usage page 0xff03), the
//     notification report 0x03 (0xff01) and the touch report 0x33 (0xff02).
//
// Interface 2 also carries the keyboard and media key reports. They are
// dropped unread wherever the operating system delivers them here.
package device

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// The KB7's USB vendor and product IDs.
const (
	VendorID  = 0x10F5
	ProductID = 0x5038
)

var (
	// ErrNotFound is returned by Open when no KB7 is connected, or when one
	// is connected but does not show the collections this package needs.
	ErrNotFound = errors.New("device: KB7 not found")
	// ErrTimeout is returned when the keyboard does not answer in time.
	ErrTimeout = errors.New("device: timed out")
	// ErrClosed is returned by every call on a Device after Close.
	ErrClosed = errors.New("device: closed")

	errMultiple = errors.New("device: more than one KB7 connected")
)

// Device is an open KB7.
//
// Its methods may be called from several goroutines. Calls that send or
// read on the stream or the feature reports run one at a time; ReadInput
// runs alongside them. Close interrupts calls that are waiting.
type Device interface {
	// SetFeature sends feature report b; b[0] is the report ID.
	SetFeature(b []byte) error
	// GetFeature reads feature report id and returns n bytes, starting with
	// the report ID.
	GetFeature(id byte, n int) ([]byte, error)
	// WriteStream sends one frame on the stream.
	WriteStream(f [64]byte) error
	// ReadStream returns the next frame the keyboard sends on the stream,
	// waiting at most timeout. It returns ErrTimeout if none arrives.
	ReadStream(timeout time.Duration) ([64]byte, error)
	// ReadInput returns the next notification (0x03) or touch (0x33)
	// report, report ID first, waiting at most timeout. It returns
	// ErrTimeout if none arrives.
	ReadInput(timeout time.Duration) ([]byte, error)
	// Close releases the keyboard. Calls after Close return ErrClosed.
	Close() error
}

// Report IDs of the input reports ReadInput returns.
const (
	notifyReport byte = 0x03
	touchReport  byte = 0x33
)

// isInputReport reports whether r is a notification or touch report.
// Everything else that arrives on interface 2 is keyboard or media key
// input and is dropped.
func isInputReport(r []byte) bool {
	return len(r) > 0 && (r[0] == notifyReport || r[0] == touchReport)
}

// streamReportLen is the length of a stream report as the operating system
// handles it on Windows: the report ID 0 followed by the 64-byte frame.
const streamReportLen = 65

// checkFeatureLen checks the length of a feature report against the largest
// one the open device can move.
func checkFeatureLen(n, max int) error {
	if n < 1 {
		return fmt.Errorf("device: feature report of %d bytes", n)
	}
	if n > max {
		return fmt.Errorf("device: feature report of %d bytes, at most %d", n, max)
	}
	return nil
}

// millis converts a timeout to whole milliseconds for the operating
// system's wait calls. A positive timeout is rounded up, so it never becomes
// a zero wait, and capped at max; zero or less means do not wait.
func millis(d time.Duration, max int64) int64 {
	if d <= 0 {
		return 0
	}
	ms := int64((d + time.Millisecond - 1) / time.Millisecond)
	return min(ms, max)
}

// collection is one HID top-level collection as Windows describes it.
// Report lengths include the report-ID byte.
type collection struct {
	UsagePage, Usage                uint16
	InputLen, OutputLen, FeatureLen int
}

// role is the job a collection of the KB7 does for this package.
type role int

const (
	roleStream  role = iota // 0xff00: the stream, 65-byte reports with ID 0
	roleFeature             // 0xff03: every feature report
	roleNotify              // 0xff01: notification report 0x03
	roleTouch               // 0xff02: touch report 0x33
	numRoles
	roleNone role = -1
)

var roleNames = [numRoles]string{"stream (0xff00)", "feature (0xff03)", "notification (0xff01)", "touch (0xff02)"}

// roleOf returns the role of a KB7 collection, or roleNone for the
// collections this package leaves alone (interface 0 on 0xff90, keyboard,
// consumer and system control).
func roleOf(c collection) role {
	switch {
	case c.UsagePage == 0xff00 && c.InputLen == streamReportLen && c.OutputLen == streamReportLen:
		return roleStream
	case c.UsagePage == 0xff03 && c.FeatureLen > 1:
		return roleFeature
	case c.UsagePage == 0xff01 && c.InputLen > 1:
		return roleNotify
	case c.UsagePage == 0xff02 && c.InputLen > 1:
		return roleTouch
	}
	return roleNone
}

// pickCollections returns, for each role, the index in cs of the collection
// that fills it. cs holds the collections of KB7s only. A role filled twice
// means two keyboards, which this package refuses rather than mixing them.
func pickCollections(cs []collection) ([numRoles]int, error) {
	var idx [numRoles]int
	for r := range idx {
		idx[r] = -1
	}
	for i, c := range cs {
		r := roleOf(c)
		if r == roleNone {
			continue
		}
		if idx[r] >= 0 {
			return idx, errMultiple
		}
		idx[r] = i
	}
	if len(cs) == 0 {
		return idx, ErrNotFound
	}
	for r, i := range idx {
		if i < 0 {
			return idx, fmt.Errorf("%w: no %s collection", ErrNotFound, roleNames[r])
		}
	}
	return idx, nil
}

// inputQueue merges the input reports of one or more readers, each of which
// keeps a read outstanding on the operating system, so that a report
// arriving on one collection is never lost while ReadInput waits on another.
type inputQueue struct {
	reports chan []byte

	failOnce sync.Once
	failed   chan struct{} // closed when a reader has stopped with an error
	err      error         // that error, set before failed is closed

	closeOnce sync.Once
	done      chan struct{} // closed by close
}

func newInputQueue() *inputQueue {
	return &inputQueue{
		reports: make(chan []byte, 64),
		failed:  make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// put queues a report, waiting while the queue is full. It returns false
// once the queue is closed.
func (q *inputQueue) put(r []byte) bool {
	select {
	case q.reports <- r:
		return true
	case <-q.done:
		return false
	}
}

// fail records the error that stopped a reader. Only the first one is kept.
func (q *inputQueue) fail(err error) {
	q.failOnce.Do(func() {
		q.err = err
		close(q.failed)
	})
}

// close wakes every waiting get and put.
func (q *inputQueue) close() {
	q.closeOnce.Do(func() { close(q.done) })
}

// get returns the next queued report, waiting at most timeout. Reports
// queued before a reader failed are returned before its error.
func (q *inputQueue) get(timeout time.Duration) ([]byte, error) {
	select {
	case <-q.done:
		return nil, ErrClosed
	default:
	}
	select {
	case r := <-q.reports:
		return r, nil
	default:
	}
	if timeout <= 0 {
		select {
		case <-q.failed:
			return nil, q.err
		default:
			return nil, ErrTimeout
		}
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-q.reports:
		return r, nil
	case <-q.failed:
		return nil, q.err
	case <-q.done:
		return nil, ErrClosed
	case <-t.C:
		return nil, ErrTimeout
	}
}
