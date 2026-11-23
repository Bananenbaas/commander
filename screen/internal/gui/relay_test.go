// SPDX-License-Identifier: GPL-2.0-or-later

package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/Bananenbaas/commander/screen/internal/bridge"
)

func fakeHandler(ctx context.Context, call string, args json.RawMessage) (any, error) {
	switch call {
	case "state":
		return map[string]string{"status": "connected"}, nil
	case "echo":
		var v any
		if len(args) > 0 {
			if err := json.Unmarshal(args, &v); err != nil {
				return nil, err
			}
		}
		return map[string]any{"args": v}, nil
	case "nothing":
		return nil, nil
	case "fail":
		return nil, errors.New("no keyboard")
	}
	return nil, fmt.Errorf("unknown call %q", call)
}

// client serves fakeHandler on one end of a pipe and returns a client on the
// other end, plus the channel that feeds events to the client.
func client(t *testing.T) (*bridge.Client, chan<- bridge.Message) {
	t.Helper()
	a, b := net.Pipe()
	events := make(chan bridge.Message, 4)
	ctx, cancel := context.WithCancel(context.Background())
	go bridge.Serve(ctx, a, fakeHandler, events)
	c := bridge.NewClient(b)
	t.Cleanup(func() { cancel(); c.Close() })
	return c, events
}

// answer decodes a line from Relay.
type answer struct {
	ID     *int64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *string         `json:"error"`
}

func decode(t *testing.T, line string) answer {
	t.Helper()
	var a answer
	if err := json.Unmarshal([]byte(line), &a); err != nil {
		t.Fatalf("line %q is not JSON: %v", line, err)
	}
	if a.ID == nil {
		t.Fatalf("line %q has no id", line)
	}
	return a
}

func TestRelayAnswer(t *testing.T) {
	c, _ := client(t)
	a := decode(t, Relay(context.Background(), c, `{"id":7,"call":"state"}`))
	if *a.ID != 7 || a.Error != nil || string(a.Result) != `{"status":"connected"}` {
		t.Fatalf("got id %d result %s error %v", *a.ID, a.Result, a.Error)
	}
	a = decode(t, Relay(context.Background(), c, `{"id":8,"call":"echo","args":{"x":[1,2]}}`))
	if *a.ID != 8 || string(a.Result) != `{"args":{"x":[1,2]}}` {
		t.Fatalf("echo: got id %d result %s", *a.ID, a.Result)
	}
	// An answer without a result still carries "result", so the page can
	// tell it from an error.
	if line := Relay(context.Background(), c, `{"id":9,"call":"nothing"}`); line != `{"id":9,"result":null}` {
		t.Fatalf("nothing: got %s", line)
	}
}

func TestRelayError(t *testing.T) {
	c, _ := client(t)
	a := decode(t, Relay(context.Background(), c, `{"id":3,"call":"fail"}`))
	if *a.ID != 3 || a.Error == nil || *a.Error != "no keyboard" || a.Result != nil {
		t.Fatalf("got id %d result %s error %v", *a.ID, a.Result, a.Error)
	}
	// A cancelled context ends the wait.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if a := decode(t, Relay(ctx, c, `{"id":5,"call":"state"}`)); *a.ID != 5 || a.Error == nil {
		t.Fatalf("cancelled: got id %d error %v", *a.ID, a.Error)
	}
	// A call after the connection ended is answered with an error, too.
	c.Close()
	if a := decode(t, Relay(context.Background(), c, `{"id":4,"call":"state"}`)); *a.ID != 4 || a.Error == nil {
		t.Fatalf("closed: got id %d error %v", *a.ID, a.Error)
	}
}

func TestRelayBadJSON(t *testing.T) {
	c, _ := client(t)
	for _, raw := range []string{
		``, `{`, `null`, `[]`, `"state"`, `{"id":"x","call":"state"}`,
		`{"id":1.5,"call":"state"}`, `{"call":"state"}{"call":"state"}`,
		string([]byte{0xff, 0xfe}), `{"call":5}`, `{"call":""}`,
	} {
		line := Relay(context.Background(), c, raw)
		if a := decode(t, line); a.Error == nil || *a.ID != 0 {
			t.Errorf("%q: got %s, want an error with id 0", raw, line)
		}
	}
	// A message that decodes but carries no call keeps its id.
	if a := decode(t, Relay(context.Background(), c, `{"id":2}`)); *a.ID != 2 || a.Error == nil {
		t.Errorf("no call: got id %d error %v", *a.ID, a.Error)
	}
	// A nil client does not panic.
	if a := decode(t, Relay(context.Background(), nil, `{"id":6,"call":"state"}`)); *a.ID != 6 || a.Error == nil {
		t.Errorf("nil client: got id %d error %v", *a.ID, a.Error)
	}
}

func TestEventLine(t *testing.T) {
	cases := []struct {
		m    bridge.Message
		want string
	}{
		{bridge.Message{Event: "state", Data: json.RawMessage(`{"status":"ok"}`)}, `{"event":"state","data":{"status":"ok"}}`},
		{bridge.Message{Event: "show"}, `{"event":"show","data":null}`},
		{bridge.Message{Event: "x", Data: json.RawMessage(`{bad`)}, `{"event":"x","data":null}`},
	}
	for _, c := range cases {
		if got := EventLine(c.m); got != c.want {
			t.Errorf("EventLine(%+v) = %s, want %s", c.m, got, c.want)
		}
	}
}

func TestEventLineThroughClient(t *testing.T) {
	c, events := client(t)
	events <- bridge.Message{Event: "state", Data: json.RawMessage(`{"n":1}`)}
	m := <-c.Events()
	if got := EventLine(m); got != `{"event":"state","data":{"n":1}}` {
		t.Fatalf("got %s", got)
	}
}

func TestThemeOf(t *testing.T) {
	cases := []struct {
		raw      string
		dark, ok bool
	}{
		{`{"call":"__theme","args":"dark"}`, true, true},
		{`{"id":4,"call":"__theme","args":"light"}`, false, true},
		{`{"call":"__theme","args":"blue"}`, false, true},
		{`{"call":"__theme"}`, false, true},
		{`{"id":1,"call":"state"}`, false, false},
		{`{`, false, false},
	}
	for _, c := range cases {
		dark, ok := themeOf(c.raw)
		if dark != c.dark || ok != c.ok {
			t.Errorf("themeOf(%s) = %v %v, want %v %v", c.raw, dark, ok, c.dark, c.ok)
		}
	}
}

func TestFromApp(t *testing.T) {
	for src, want := range map[string]bool{
		AppOrigin + "/":                      true,
		AppOrigin + "/index.html":            true,
		AppOrigin + "/?x=1":                  true,
		AppOrigin:                            false,
		AppOrigin + ".evil.example/":         false,
		AppOrigin + ":8443/":                 false,
		"http://commander.invalid/":          false,
		"file:///C:/Users/someone/drop.html": false,
		"about:blank":                        false,
		"":                                   false,
	} {
		if got := fromApp(src); got != want {
			t.Errorf("fromApp(%q) = %v, want %v", src, got, want)
		}
	}
}

func TestPickOf(t *testing.T) {
	cases := []struct {
		raw  string
		id   int64
		kind pickKind
		ok   bool
	}{
		{`{"id":7,"call":"__pickPicture"}`, 7, pickPNG, true},
		{`{"id":8,"call":"__pickPicture","args":null}`, 8, pickPNG, true},
		{`{"id":4,"call":"__pickProgram"}`, 4, pickEXE, true},
		{`{"id":9,"call":"__theme","args":"dark"}`, 0, 0, false},
		{`{"id":1,"call":"state"}`, 0, 0, false},
		{`{"id":1,"call":"__pickPictures"}`, 0, 0, false},
		{`{"id":1,"call":"__pickPrograms"}`, 0, 0, false},
		{`{"id":1}`, 0, 0, false},
		{`{`, 0, 0, false},
		{``, 0, 0, false},
	}
	for _, c := range cases {
		id, kind, ok := pickOf(c.raw)
		if id != c.id || kind != c.kind || ok != c.ok {
			t.Errorf("pickOf(%s) = %d %v %v, want %d %v %v", c.raw, id, kind, ok, c.id, c.kind, c.ok)
		}
	}
}

func TestPathLine(t *testing.T) {
	for _, p := range []string{``, `C:\Pictures\a.png`, `C:\odd "name"\b.png`} {
		var got struct {
			ID     int64 `json:"id"`
			Result struct {
				Path string `json:"path"`
			} `json:"result"`
		}
		line := pathLine(5, p)
		if err := json.Unmarshal([]byte(line), &got); err != nil {
			t.Fatalf("pathLine(%q) = %s: %v", p, line, err)
		}
		if got.ID != 5 || got.Result.Path != p {
			t.Errorf("pathLine(%q) = %s", p, line)
		}
	}
}

func TestPickKinds(t *testing.T) {
	cases := []struct {
		kind                 pickKind
		name, pattern, title string
	}{
		{pickPNG, "PNG pictures (*.png)", "*.png", "Choose a picture"},
		{pickEXE, "Programs (*.exe)", "*.exe", "Choose a program"},
	}
	for _, c := range cases {
		name, pattern := c.kind.filter()
		if name != c.name || pattern != c.pattern || c.kind.title() != c.title {
			t.Errorf("kind %d = %q %q %q", c.kind, name, pattern, c.kind.title())
		}
	}
}

func TestPickGuard(t *testing.T) {
	var g pickGuard
	opened := 0
	var inner string
	line := g.answer(3, pickEXE, func(k pickKind) (string, error) {
		opened++
		if k != pickEXE {
			t.Errorf("dialog got kind %v", k)
		}
		inner = g.answer(4, pickPNG, func(pickKind) (string, error) {
			opened++
			return "never.png", nil
		})
		return `C:\Games\x.exe`, nil
	})
	if opened != 1 {
		t.Errorf("dialog opened %d times, want 1", opened)
	}
	if inner != pathLine(4, "") {
		t.Errorf("busy answer = %s", inner)
	}
	if line != pathLine(3, `C:\Games\x.exe`) {
		t.Errorf("answer = %s", line)
	}
	// Free again after success, cancel and error.
	steps := []struct {
		path string
		err  error
		want string
	}{
		{"", nil, pathLine(5, "")},
		{"", errors.New("boom"), errorLine(5, "boom")},
		{"a.png", nil, pathLine(5, "a.png")},
	}
	for _, s := range steps {
		got := g.answer(5, pickPNG, func(pickKind) (string, error) { return s.path, s.err })
		if got != s.want {
			t.Errorf("answer = %s, want %s", got, s.want)
		}
	}
}
