// SPDX-License-Identifier: GPL-2.0-or-later

// Package bridge is the line protocol between Commander and its window: one
// JSON object per line over any byte stream (a named pipe in practice). The
// window sends calls and Commander answers them; Commander also pushes events.
package bridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// Message is one line on the wire: a call, its answer, or an event.
type Message struct {
	ID     int64           `json:"id,omitempty"`     // calls and answers
	Call   string          `json:"call,omitempty"`   // a call
	Args   json.RawMessage `json:"args,omitempty"`
	Result json.RawMessage `json:"result,omitempty"` // an answer
	Error  string          `json:"error,omitempty"`  // an answer that failed
	Event  string          `json:"event,omitempty"`  // an event (no ID)
	Data   json.RawMessage `json:"data,omitempty"`
}

// MaxLine is the most bytes one line may hold, newline excluded.
const MaxLine = 1 << 20

// ErrLineTooLong ends a connection whose peer sent a line over MaxLine.
var ErrLineTooLong = errors.New("bridge: line longer than MaxLine")

// ErrClosed is returned by calls on a connection that has ended.
var ErrClosed = errors.New("bridge: connection closed")

// Handler answers one call. A returned error becomes the answer's Error text.
type Handler func(ctx context.Context, call string, args json.RawMessage) (any, error)

// writer serialises writes of whole lines to a connection.
type writer struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *writer) send(m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	_, err = w.w.Write(b)
	return err
}

// readLine returns the next line without its newline. A line over MaxLine
// yields ErrLineTooLong.
func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if len(bytes.TrimRight(line, "\r\n")) > MaxLine {
			return nil, ErrLineTooLong
		}
		switch {
		case err == nil:
			return bytes.TrimRight(line, "\r\n"), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		default:
			return nil, err
		}
	}
}

// Serve answers the calls arriving on conn with h (each call on its own
// goroutine, answers written one at a time) and writes every event sent on
// events, until conn fails or ctx ends; it then closes conn.
func Serve(ctx context.Context, conn io.ReadWriteCloser, h Handler, events <-chan Message) error {
	ctx, cancel := context.WithCancel(ctx)
	out := &writer{w: conn}
	var wg sync.WaitGroup

	readErr := make(chan error, 1)
	go func() {
		r := bufio.NewReaderSize(conn, 64<<10)
		for {
			line, err := readLine(r)
			if err != nil {
				readErr <- err
				return
			}
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var m Message
			if err := json.Unmarshal(line, &m); err != nil {
				if out.send(Message{Error: "bad message: " + err.Error()}) != nil {
					readErr <- io.ErrClosedPipe
					return
				}
				continue
			}
			if m.Call == "" {
				if out.send(Message{ID: m.ID, Error: "bad message: neither a call nor a known answer"}) != nil {
					readErr <- io.ErrClosedPipe
					return
				}
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := h(ctx, m.Call, m.Args)
				ans := Message{ID: m.ID}
				if err != nil {
					ans.Error = err.Error()
				} else if res != nil {
					b, merr := json.Marshal(res)
					if merr != nil {
						ans.Error = "bad result: " + merr.Error()
					} else {
						ans.Result = b
					}
				}
				_ = out.send(ans)
			}()
		}
	}()

	var result error
loop:
	for {
		select {
		case <-ctx.Done():
			result = ctx.Err()
			break loop
		case err := <-readErr:
			if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
				result = err
			}
			break loop
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if err := out.send(ev); err != nil {
				break loop
			}
		}
	}
	cancel()
	conn.Close()
	wg.Wait()
	return result
}

// Client is the calling side of the protocol.
type Client struct {
	conn    io.ReadWriteCloser
	out     *writer
	nextID  atomic.Int64
	mu      sync.Mutex
	pending map[int64]chan Message
	events  chan Message
	done    chan struct{}
	once    sync.Once
}

// NewClient starts reading from conn and returns the client.
func NewClient(conn io.ReadWriteCloser) *Client {
	c := &Client{
		conn:    conn,
		out:     &writer{w: conn},
		pending: map[int64]chan Message{},
		events:  make(chan Message, 64),
		done:    make(chan struct{}),
	}
	go c.read()
	return c
}

func (c *Client) read() {
	r := bufio.NewReaderSize(c.conn, 64<<10)
	for {
		line, err := readLine(r)
		if err != nil {
			break
		}
		var m Message
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		switch {
		case m.Event != "":
			select {
			case c.events <- m:
			case <-c.done:
			}
		case m.ID != 0:
			c.mu.Lock()
			ch := c.pending[m.ID]
			delete(c.pending, m.ID)
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
	c.conn.Close()
	c.once.Do(func() { close(c.done) })
	close(c.events)
}

// Call sends a call and waits for its answer; an answer with Error returns
// an error with that text.
func (c *Client) Call(ctx context.Context, call string, args any) (json.RawMessage, error) {
	select {
	case <-c.done:
		return nil, ErrClosed
	default:
	}
	m := Message{ID: c.nextID.Add(1), Call: call}
	if args != nil {
		b, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("bridge: encode args: %w", err)
		}
		m.Args = b
	}
	ch := make(chan Message, 1)
	c.mu.Lock()
	c.pending[m.ID] = ch
	c.mu.Unlock()
	forget := func() {
		c.mu.Lock()
		delete(c.pending, m.ID)
		c.mu.Unlock()
	}
	if err := c.out.send(m); err != nil {
		forget()
		return nil, err
	}
	select {
	case ans := <-ch:
		if ans.Error != "" {
			return nil, errors.New(ans.Error)
		}
		return ans.Result, nil
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	case <-c.done:
		forget()
		return nil, ErrClosed
	}
}

// Events delivers events; it is closed when the connection ends.
func (c *Client) Events() <-chan Message { return c.events }

// Done is closed when the connection has ended.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close ends the connection and waits for the reader to stop.
func (c *Client) Close() error {
	err := c.conn.Close()
	<-c.done
	return err
}
