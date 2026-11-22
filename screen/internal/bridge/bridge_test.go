// SPDX-License-Identifier: GPL-2.0-or-later

package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func echoHandler(ctx context.Context, call string, args json.RawMessage) (any, error) {
	switch call {
	case "double":
		var a struct{ N int }
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		return map[string]int{"n": a.N * 2}, nil
	case "fail":
		return nil, errors.New("nope")
	case "slow":
		time.Sleep(10 * time.Millisecond)
		var a struct{ N int }
		_ = json.Unmarshal(args, &a)
		return map[string]int{"n": a.N}, nil
	}
	return nil, fmt.Errorf("unknown call %q", call)
}

// start serves one end of a pipe and returns a client on the other end.
func start(t *testing.T, events <-chan Message) (*Client, <-chan error, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	errc := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { errc <- Serve(ctx, a, echoHandler, events) }()
	c := NewClient(b)
	t.Cleanup(func() { cancel(); c.Close() })
	return c, errc, b
}

func TestCallAndAnswer(t *testing.T) {
	c, _, _ := start(t, nil)
	got, err := c.Call(context.Background(), "double", map[string]int{"n": 1})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"n":2}` {
		t.Fatalf("got %s", got)
	}
}

func TestCallError(t *testing.T) {
	c, _, _ := start(t, nil)
	_, err := c.Call(context.Background(), "fail", nil)
	if err == nil || err.Error() != "nope" {
		t.Fatalf("err = %v", err)
	}
}

func TestEventsArrive(t *testing.T) {
	ev := make(chan Message, 2)
	c, _, _ := start(t, ev)
	ev <- Message{Event: "one", Data: json.RawMessage(`1`)}
	ev <- Message{Event: "two"}
	for _, want := range []string{"one", "two"} {
		select {
		case m := <-c.Events():
			if m.Event != want {
				t.Fatalf("event %q, want %q", m.Event, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no event")
		}
	}
}

func TestConcurrentCalls(t *testing.T) {
	c, _, _ := start(t, nil)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := c.Call(context.Background(), "slow", map[string]int{"n": i})
			if err != nil {
				t.Errorf("call %d: %v", i, err)
				return
			}
			if want := fmt.Sprintf(`{"n":%d}`, i); string(got) != want {
				t.Errorf("call %d: got %s, want %s", i, got, want)
			}
		}()
	}
	wg.Wait()
}

func TestBadLineIsAnsweredAndServingGoesOn(t *testing.T) {
	raw := startRaw(t)
	r := bufio.NewReader(raw)
	go raw.Write([]byte("not json\n"))
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var m Message
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	if m.ID != 0 || !strings.Contains(m.Error, "bad message") {
		t.Fatalf("got %+v", m)
	}
	go raw.Write([]byte(`{"id":7,"call":"double","args":{"n":4}}` + "\n"))
	line, err = r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	m = Message{}
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	if m.ID != 7 || string(m.Result) != `{"n":8}` {
		t.Fatalf("got %+v", m)
	}
}

func TestUnknownShapeIsRefused(t *testing.T) {
	raw := startRaw(t)
	r := bufio.NewReader(raw)
	go raw.Write([]byte(`{"id":99,"result":1}` + "\n"))
	line, err := r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var m Message
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatal(err)
	}
	if m.Error == "" {
		t.Fatalf("expected an error answer, got %+v", m)
	}
	go raw.Write([]byte(`{"id":1,"call":"double","args":{"n":1}}` + "\n"))
	line, err = r.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	m = Message{}
	if err := json.Unmarshal([]byte(line), &m); err != nil || m.ID != 1 || m.Error != "" {
		t.Fatalf("got %+v, %v", m, err)
	}
}

func TestLongLineEndsTheConnection(t *testing.T) {
	c, errc, raw := start(t, nil)
	pending := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "block", nil)
		pending <- err
	}()
	// Let the pending call get on the wire before the oversized line.
	time.Sleep(50 * time.Millisecond)
	go func() {
		raw.Write([]byte(strings.Repeat("x", MaxLine+10) + "\n"))
	}()
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("Serve returned nil, want an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not end")
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("client Done not closed")
	}
	select {
	case <-pending:
	case <-time.After(5 * time.Second):
		t.Fatal("pending call not released")
	}
	if _, err := c.Call(context.Background(), "double", nil); err == nil {
		t.Fatal("call on ended connection succeeded")
	}
}

func TestCallAfterCloseFails(t *testing.T) {
	c, _, _ := start(t, nil)
	c.Close()
	if _, err := c.Call(context.Background(), "double", nil); err == nil {
		t.Fatal("expected an error")
	}
	select {
	case <-c.Done():
	default:
		t.Fatal("Done not closed")
	}
	if _, ok := <-c.Events(); ok {
		t.Fatal("Events not closed")
	}
}

func TestCtxEndStopsServe(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- Serve(ctx, a, echoHandler, nil) }()
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not stop")
	}
	b.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := b.Read(make([]byte, 1)); err == nil {
		t.Fatal("conn not closed")
	}
}

// startRaw serves one end of a pipe and returns the bare other end, with no
// client reading from it.
func startRaw(t *testing.T) net.Conn {
	t.Helper()
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	go Serve(ctx, a, echoHandler, nil)
	t.Cleanup(func() { cancel(); b.Close() })
	return b
}
