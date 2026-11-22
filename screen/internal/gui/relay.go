// SPDX-License-Identifier: GPL-2.0-or-later

package gui

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/Bananenbaas/commander/screen/internal/bridge"
)

// pageCall is what the page posts: {"id": <page id>, "call": ..., "args": ...}.
type pageCall struct {
	ID   int64           `json:"id"`
	Call string          `json:"call"`
	Args json.RawMessage `json:"args"`
}

// pageAnswer is the line posted back for a call. Exactly one of Result and
// Error is set; a call without a result answers "result": null.
type pageAnswer struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// themeCall is the call the window answers itself.
const themeCall = "__theme"

// Relay forwards a call the page posted with the client and returns the line
// to post back: {"id": <page id>, "result": ...} or {"id": <page id>,
// "error": "..."}. It never panics and always returns a JSON object; input
// that does not decode is answered with an error and id 0.
func Relay(ctx context.Context, c *bridge.Client, raw string) (line string) {
	var pc pageCall
	defer func() {
		if p := recover(); p != nil {
			line = errorLine(pc.ID, "internal error")
		}
	}()
	if err := json.Unmarshal([]byte(raw), &pc); err != nil {
		return errorLine(0, "bad message: "+err.Error())
	}
	switch {
	case pc.Call == "":
		return errorLine(pc.ID, "bad message: no call")
	case c == nil:
		return errorLine(pc.ID, "not connected")
	}
	if err := ctx.Err(); err != nil {
		return errorLine(pc.ID, err.Error())
	}
	var args any
	if len(pc.Args) > 0 {
		args = pc.Args
	}
	res, err := c.Call(ctx, pc.Call, args)
	if err != nil {
		return errorLine(pc.ID, err.Error())
	}
	if len(res) == 0 {
		res = json.RawMessage("null")
	}
	b, err := json.Marshal(pageAnswer{ID: pc.ID, Result: res})
	if err != nil {
		return errorLine(pc.ID, "bad result")
	}
	return string(b)
}

// errorLine is an answer with an error. Marshalling a string cannot fail.
func errorLine(id int64, msg string) string {
	b, err := json.Marshal(msg)
	if err != nil {
		b = []byte(`"error"`)
	}
	return `{"id":` + strconv.FormatInt(id, 10) + `,"error":` + string(b) + `}`
}

// EventLine turns an event into the line posted to the page: {"event": ...,
// "data": ...}. Data that is not valid JSON is sent as null.
func EventLine(m bridge.Message) string {
	data := m.Data
	if len(data) == 0 || !json.Valid(data) {
		data = json.RawMessage("null")
	}
	b, err := json.Marshal(struct {
		Event string          `json:"event"`
		Data  json.RawMessage `json:"data"`
	}{m.Event, data})
	if err != nil {
		return `{"event":"","data":null}`
	}
	return string(b)
}

// themeOf reports whether raw is the "__theme" call and, if so, whether it
// asks for a dark title bar. Anything but "dark" means light.
func themeOf(raw string) (dark, ok bool) {
	var pc struct {
		Call string `json:"call"`
		Args string `json:"args"`
	}
	if json.Unmarshal([]byte(raw), &pc) != nil || pc.Call != themeCall {
		// A "__theme" whose args are not a string still is a theme call.
		var bare struct {
			Call string `json:"call"`
		}
		if json.Unmarshal([]byte(raw), &bare) == nil && bare.Call == themeCall {
			return false, true
		}
		return false, false
	}
	return pc.Args == "dark", true
}
