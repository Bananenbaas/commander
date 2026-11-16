package device

import (
	"errors"
	"testing"
	"time"
)

// kb7Collections is the KB7's top-level collections as Windows lists them:
// usage page, usage and input/output/feature lengths, report-ID byte
// included. The vendor collections carry what Windows reports (0xff90
// 65/65/65, 0xff00 usage 0x0001 65/65/0, 0xff01 input 8, 0xff02 input 12,
// 0xff03 feature 1044). Not recorded, and only placeholders here: the
// usages of 0xff01-0xff03 (not checked by roleOf) and the lengths of the
// keyboard, consumer and system control collections.
var kb7Collections = []collection{
	{UsagePage: 0xff90, Usage: 0xff00, InputLen: 65, OutputLen: 65, FeatureLen: 65}, // interface 0
	{UsagePage: 0xff00, Usage: 0x0001, InputLen: 65, OutputLen: 65},                 // stream
	{UsagePage: 0x000c, Usage: 0x0001, InputLen: 4},                                 // consumer
	{UsagePage: 0x0001, Usage: 0x0080, InputLen: 2},                                 // system control
	{UsagePage: 0xff01, Usage: 0x0001, InputLen: 8},                                 // notification
	{UsagePage: 0x0001, Usage: 0x0006, InputLen: 21},                                // keyboard
	{UsagePage: 0xff02, Usage: 0x0001, InputLen: 12},                                // touch
	{UsagePage: 0xff03, Usage: 0x0001, FeatureLen: 1044},                            // features
	{UsagePage: 0x0001, Usage: 0x0006, InputLen: 9, OutputLen: 2},                   // boot keyboard
}

func TestPickCollections(t *testing.T) {
	idx, err := pickCollections(kb7Collections)
	if err != nil {
		t.Fatal(err)
	}
	want := [numRoles]int{roleStream: 1, roleFeature: 7, roleNotify: 4, roleTouch: 6}
	if idx != want {
		t.Fatalf("pickCollections = %v, want %v", idx, want)
	}
}

func TestPickCollectionsMissing(t *testing.T) {
	if _, err := pickCollections(nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no collections: err = %v, want ErrNotFound", err)
	}
	for drop := range kb7Collections {
		cs := append(append([]collection(nil), kb7Collections[:drop]...), kb7Collections[drop+1:]...)
		_, err := pickCollections(cs)
		if r := roleOf(kb7Collections[drop]); r == roleNone {
			if err != nil {
				t.Errorf("without unused collection %d: %v", drop, err)
			}
		} else if !errors.Is(err, ErrNotFound) {
			t.Errorf("without %s: err = %v, want ErrNotFound", roleNames[r], err)
		}
	}
}

func TestPickCollectionsTwoKeyboards(t *testing.T) {
	cs := append(append([]collection(nil), kb7Collections...), kb7Collections...)
	_, err := pickCollections(cs)
	if !errors.Is(err, ErrNotFound) || !errors.Is(err, errMultiple) {
		t.Fatalf("err = %v, want ErrNotFound for more than one KB7", err)
	}
}

// A collection on the right usage page but laid out differently from what
// was measured is not used, so Open reports ErrNotFound.
func TestPickCollectionsExactLayout(t *testing.T) {
	changes := map[string]func(*collection){
		"input length +1":   func(c *collection) { c.InputLen++ },
		"output length +1":  func(c *collection) { c.OutputLen++ },
		"feature length +1": func(c *collection) { c.FeatureLen++ },
		"input length -1": func(c *collection) {
			if c.InputLen > 0 {
				c.InputLen--
			} else {
				c.InputLen = 64
			}
		},
		"feature length -1": func(c *collection) {
			if c.FeatureLen > 0 {
				c.FeatureLen--
			} else {
				c.FeatureLen = 1043
			}
		},
	}
	for i, c := range kb7Collections {
		r := roleOf(c)
		if r == roleNone {
			continue
		}
		for name, change := range changes {
			cs := append([]collection(nil), kb7Collections...)
			change(&cs[i])
			if _, err := pickCollections(cs); !errors.Is(err, ErrNotFound) {
				t.Errorf("%s with %s: err = %v, want ErrNotFound", roleNames[r], name, err)
			}
		}
	}

	cs := append([]collection(nil), kb7Collections...)
	cs[1].Usage = 0x0002 // the stream's usage is checked
	if _, err := pickCollections(cs); !errors.Is(err, ErrNotFound) {
		t.Errorf("stream with usage 0x0002: err = %v, want ErrNotFound", err)
	}
	cs = append([]collection(nil), kb7Collections...)
	cs[7].Usage = 0x0002 // the others' usages are not recorded, so not checked
	if _, err := pickCollections(cs); err != nil {
		t.Errorf("feature collection with another usage: %v", err)
	}
}

func TestDisconnected(t *testing.T) {
	cause := errors.New("cause")
	err := disconnected("read input", cause)
	if !errors.Is(err, ErrDisconnected) || !errors.Is(err, cause) {
		t.Fatalf("%v does not carry ErrDisconnected and its cause", err)
	}
	if got, want := err.Error(), "device: keyboard disconnected: read input: cause"; got != want {
		t.Fatalf("message %q, want %q", got, want)
	}
	if err := disconnected("read stream", nil); !errors.Is(err, ErrDisconnected) || err.Error() != "device: keyboard disconnected: read stream" {
		t.Fatalf("without cause: %v", err)
	}
}

func TestIsInputReport(t *testing.T) {
	for _, tc := range []struct {
		r    []byte
		want bool
	}{
		{[]byte{0x03, 0x00, 0xf1, 0x01, 0x00, 0x00, 0x01, 0x00}, true},
		{[]byte{0x33, 0x01, 0x05, 0x05, 0x00, 0x00, 0x01, 0x01}, true},
		{[]byte{0x04, 0x00, 0x00}, false}, // keyboard
		{[]byte{0x01, 0xe9, 0x00}, false}, // consumer
		{nil, false},
	} {
		if got := isInputReport(tc.r); got != tc.want {
			t.Errorf("isInputReport(% x) = %v, want %v", tc.r, got, tc.want)
		}
	}
}

func TestMillis(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		max  int64
		want int64
	}{
		{0, 100, 0},
		{-time.Second, 100, 0},
		{time.Nanosecond, 100, 1},
		{time.Millisecond, 100, 1},
		{1500 * time.Microsecond, 100, 2},
		{50 * time.Millisecond, 100, 50},
		{time.Hour, 100, 100},
	} {
		if got := millis(tc.d, tc.max); got != tc.want {
			t.Errorf("millis(%v, %d) = %d, want %d", tc.d, tc.max, got, tc.want)
		}
	}
}

func TestCheckFeatureLen(t *testing.T) {
	for _, tc := range []struct {
		n, max int
		ok     bool
	}{
		{0, 1044, false},
		{1, 1044, true},
		{1044, 1044, true},
		{1045, 1044, false},
	} {
		if err := checkFeatureLen(tc.n, tc.max); (err == nil) != tc.ok {
			t.Errorf("checkFeatureLen(%d, %d) = %v", tc.n, tc.max, err)
		}
	}
}

func TestInputQueueOrderAndTimeout(t *testing.T) {
	q := newInputQueue()
	if _, err := q.get(0); err != ErrTimeout {
		t.Fatalf("empty get(0): %v, want ErrTimeout", err)
	}
	start := time.Now()
	if _, err := q.get(20 * time.Millisecond); err != ErrTimeout {
		t.Fatalf("empty get: %v, want ErrTimeout", err)
	}
	if d := time.Since(start); d < 15*time.Millisecond {
		t.Fatalf("get returned after %v, before its timeout", d)
	}
	q.put([]byte{0x33, 1})
	q.put([]byte{0x03, 2})
	for _, want := range []byte{1, 2} {
		r, err := q.get(0)
		if err != nil || r[1] != want {
			t.Fatalf("get = % x, %v; want report %d", r, err, want)
		}
	}
}

func TestInputQueueWaitsForReport(t *testing.T) {
	q := newInputQueue()
	go func() {
		time.Sleep(10 * time.Millisecond)
		q.put([]byte{0x33})
	}()
	r, err := q.get(5 * time.Second)
	if err != nil || len(r) != 1 || r[0] != 0x33 {
		t.Fatalf("get = % x, %v", r, err)
	}
}

func TestInputQueueFailure(t *testing.T) {
	q := newInputQueue()
	boom := errors.New("unplugged")
	q.put([]byte{0x03})
	q.fail(boom)
	q.fail(errors.New("second"))
	if r, err := q.get(time.Second); err != nil || r[0] != 0x03 {
		t.Fatalf("report queued before the failure: % x, %v", r, err)
	}
	for _, timeout := range []time.Duration{0, time.Second} {
		if _, err := q.get(timeout); err != boom {
			t.Fatalf("get(%v) after failure: %v, want %v", timeout, err, boom)
		}
	}
}

func TestInputQueueCloseWakesPut(t *testing.T) {
	q := newInputQueue()
	for range cap(q.reports) {
		if !q.put([]byte{0x33}) {
			t.Fatal("put on an open queue with room returned false")
		}
	}
	blocked := make(chan bool)
	go func() { blocked <- q.put([]byte{0x33}) }()
	time.Sleep(10 * time.Millisecond)
	q.close()
	select {
	case ok := <-blocked:
		if ok {
			t.Fatal("put on a full queue returned true after close")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("close did not wake a put on a full queue")
	}
}

func TestInputQueueClose(t *testing.T) {
	q := newInputQueue()
	got := make(chan error)
	go func() {
		_, err := q.get(time.Minute)
		got <- err
	}()
	time.Sleep(10 * time.Millisecond)
	q.close()
	select {
	case err := <-got:
		if err != ErrClosed {
			t.Fatalf("waiting get: %v, want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("close did not wake a waiting get")
	}
	q.close()
	if _, err := q.get(0); err != ErrClosed {
		t.Fatalf("get after close: %v, want ErrClosed", err)
	}
}
