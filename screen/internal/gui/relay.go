// SPDX-License-Identifier: GPL-2.0-or-later

package gui

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"

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

// fromApp reports whether a message's source URI is a page of AppOrigin.
// Anything else (a file dropped onto the window, any other page) is not
// relayed.
func fromApp(source string) bool {
	return strings.HasPrefix(source, AppOrigin+"/")
}

// pickKind says which file the player is asked for.
type pickKind int

const (
	pickPNG pickKind = iota + 1 // "__pickPicture"
	pickEXE                     // "__pickProgram"
)

// filter is the name shown in the dialog and the pattern it lists.
func (k pickKind) filter() (name, pattern string) {
	if k == pickEXE {
		return "Programs (*.exe)", "*.exe"
	}
	return "PNG pictures (*.png)", "*.png"
}

// title is the dialog's caption.
func (k pickKind) title() string {
	if k == pickEXE {
		return "Choose a program"
	}
	return "Choose a picture"
}

// pickOf reports whether raw is a "__pickPicture" or "__pickProgram" call,
// with its page id and which file it asks for.
func pickOf(raw string) (id int64, kind pickKind, ok bool) {
	var pc struct {
		ID   int64  `json:"id"`
		Call string `json:"call"`
	}
	if json.Unmarshal([]byte(raw), &pc) != nil {
		return 0, 0, false
	}
	switch pc.Call {
	case "__pickPicture":
		return pc.ID, pickPNG, true
	case "__pickProgram":
		return pc.ID, pickEXE, true
	}
	return 0, 0, false
}

// pickGuard lets one file dialog be open at a time.
type pickGuard struct{ busy atomic.Bool }

// answer runs dialog and returns the line to post for the call. While a
// dialog is open, another pick is answered at once with an empty path.
func (g *pickGuard) answer(id int64, kind pickKind, dialog func(pickKind) (string, error)) string {
	if !g.busy.CompareAndSwap(false, true) {
		return pathLine(id, "")
	}
	defer g.busy.Store(false)
	path, err := dialog(kind)
	if err != nil {
		return errorLine(id, err.Error())
	}
	return pathLine(id, path)
}

// pathLine is the answer to a pick call; an empty path means the
// player cancelled.
func pathLine(id int64, path string) string {
	b, err := json.Marshal(struct {
		ID     int64 `json:"id"`
		Result struct {
			Path string `json:"path"`
		} `json:"result"`
	}{ID: id, Result: struct {
		Path string `json:"path"`
	}{path}})
	if err != nil {
		return errorLine(id, "bad result")
	}
	return string(b)
}
