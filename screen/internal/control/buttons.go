// SPDX-License-Identifier: GPL-2.0-or-later

package control

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"strings"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/action"
	"github.com/Bananenbaas/commander/screen/internal/apply"
	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/store"
)

// pictureSlots is the number of different pictures the KB7 holds.
const pictureSlots = 15

// stepJSON is one step of a button as the window sees it: exactly one of
// keys, waitMs, run or open is set.
type stepJSON struct {
	Keys   []string `json:"keys"`
	WaitMs *int64   `json:"waitMs"`
	Run    *string  `json:"run"`
	Args   []string `json:"args"`
	Open   *string  `json:"open"`
}

func (s stepJSON) MarshalJSON() ([]byte, error) {
	switch {
	case s.Keys != nil:
		return json.Marshal(map[string]any{"keys": s.Keys})
	case s.WaitMs != nil:
		return json.Marshal(map[string]any{"waitMs": *s.WaitMs})
	case s.Run != nil:
		args := s.Args
		if args == nil {
			args = []string{}
		}
		return json.Marshal(map[string]any{"run": *s.Run, "args": args})
	case s.Open != nil:
		return json.Marshal(map[string]any{"open": *s.Open})
	}
	return []byte("{}"), nil
}

// buttonJSON is one button as the window sees it.
type buttonJSON struct {
	Page      int        `json:"page"`
	Position  int        `json:"position"`
	Title     string     `json:"title"`
	Label     string     `json:"label"`
	Color     string     `json:"color"`
	Symbol    string     `json:"symbol"`
	Picture   string     `json:"picture"`
	Steps     []stepJSON `json:"steps"`
	Hold      bool       `json:"hold"`
	ConfirmMs int64      `json:"confirmMs"`
	State     string     `json:"state"`
}

// toStep turns a window step into the step text of a layout file.
func toStep(s stepJSON) (string, error) {
	var st action.Step
	set := 0
	if s.Keys != nil {
		set++
		keys := make([]action.Key, 0, len(s.Keys))
		for _, n := range s.Keys {
			k, err := action.ParseKey(n)
			if err != nil {
				return "", err
			}
			keys = append(keys, k)
		}
		st = action.Chord{Keys: keys}
	}
	if s.WaitMs != nil {
		set++
		st = action.Wait{D: time.Duration(*s.WaitMs) * time.Millisecond}
	}
	if s.Run != nil {
		set++
		st = action.Run{Program: *s.Run, Args: s.Args}
	}
	if s.Open != nil {
		set++
		st = action.Open{Target: *s.Open}
	}
	if set != 1 {
		return "", errors.New("a step is either keys, a wait, a program or something to open")
	}
	return action.FormatStep(st)
}

// toFile turns a window button into a button of a layout file. A step that
// cannot be written is a *layout.FieldError for the field "action" with its
// index.
func toFile(b buttonJSON) (layout.FileButton, error) {
	fb := layout.FileButton{
		Page: b.Page, Position: b.Position, Title: b.Title, Label: b.Label,
		Color: b.Color, Symbol: b.Symbol, Picture: b.Picture, State: b.State,
	}
	for i, s := range b.Steps {
		text, err := toStep(s)
		if err != nil {
			return fb, &layout.FieldError{Field: "action", Step: i, Err: err}
		}
		fb.Action = append(fb.Action, text)
	}
	if b.Hold {
		fb.Mode = "hold"
	}
	if b.ConfirmMs > 0 {
		fb.Confirm = fmt.Sprintf("hold %dms", b.ConfirmMs)
	}
	return fb, nil
}

// fromFile turns a button of a layout file into a window button.
func fromFile(fb layout.FileButton) (buttonJSON, error) {
	b := buttonJSON{
		Page: fb.Page, Position: fb.Position, Title: fb.Title, Label: fb.Label,
		Color: strings.ToUpper(fb.Color), Symbol: fb.Symbol, Picture: fb.Picture,
		Steps: []stepJSON{}, Hold: fb.Mode == "hold", State: fb.State,
	}
	for i, text := range fb.Action {
		st, err := action.ParseStep(text)
		if err != nil {
			return b, &layout.FieldError{Field: "action", Step: i, Err: fmt.Errorf("action %q: %w", text, err)}
		}
		switch v := st.(type) {
		case action.Chord:
			keys := make([]string, 0, len(v.Keys))
			for _, k := range v.Keys {
				keys = append(keys, action.KeyName(k))
			}
			b.Steps = append(b.Steps, stepJSON{Keys: keys})
		case action.Wait:
			ms := v.D.Milliseconds()
			b.Steps = append(b.Steps, stepJSON{WaitMs: &ms})
		case action.Run:
			p := v.Program
			b.Steps = append(b.Steps, stepJSON{Run: &p, Args: append([]string{}, v.Args...)})
		case action.Open:
			t := v.Target
			b.Steps = append(b.Steps, stepJSON{Open: &t})
		default:
			return b, &layout.FieldError{Field: "action", Step: i, Err: fmt.Errorf("unknown step %q", text)}
		}
	}
	if fb.Confirm != "" {
		rest, ok := strings.CutPrefix(fb.Confirm, "hold ")
		d, err := time.ParseDuration(rest)
		if !ok || err != nil {
			return b, &layout.FieldError{Field: "confirm", Step: -1, Err: fmt.Errorf("confirm must be \"hold <duration>\", got %q", fb.Confirm)}
		}
		b.ConfirmMs = d.Milliseconds()
	}
	return b, nil
}

// kb7Slot checks the device and profile of a call.
func kb7Slot(device string, profile int) (byte, error) {
	if device != "kb7" {
		return 0, errors.New("unknown device")
	}
	if profile < 0 || profile > 4 {
		return 0, fmt.Errorf("unknown profile %d", profile)
	}
	return byte(profile), nil
}

// layoutOf reads the layout file of KB7 profile slot from dir.
func layoutOf(dir string, slot byte) ([]byte, *layout.File, error) {
	_, data, err := store.LayoutFile(dir, slot)
	if err != nil {
		return nil, nil, err
	}
	f, err := layout.DecodeFile(data)
	if err != nil {
		return nil, nil, err
	}
	return data, f, nil
}

type deviceProfileOut struct {
	Device       string       `json:"device"`
	Profile      int          `json:"profile"`
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Buttons      []buttonJSON `json:"buttons"`
	States       []string     `json:"states"`
	PicturesUsed int          `json:"picturesUsed"`
	PictureSlots int          `json:"pictureSlots"`
}

func (s *Service) deviceProfile(args json.RawMessage) (any, error) {
	var a struct {
		Device  string `json:"device"`
		Profile int    `json:"profile"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("deviceProfile: bad arguments: %w", err)
	}
	slot, err := kb7Slot(a.Device, a.Profile)
	if err != nil {
		return nil, fmt.Errorf("deviceProfile: %w", err)
	}
	_, f, err := layoutOf(s.d.Dir, slot)
	if err != nil {
		return nil, fmt.Errorf("deviceProfile: %w", err)
	}
	out := deviceProfileOut{
		Device: "kb7", Profile: int(slot), Version: store.ButtonsVersion(f),
		Buttons: []buttonJSON{}, States: []string{}, PictureSlots: pictureSlots,
	}
	if f.Name != nil {
		out.Name = *f.Name
	}
	for _, fb := range f.Buttons {
		b, err := fromFile(fb)
		if err != nil {
			return nil, fmt.Errorf("deviceProfile: page %d position %d: %w", fb.Page, fb.Position, err)
		}
		out.Buttons = append(out.Buttons, b)
	}
	if cfg, err := s.d.Load(s.d.Dir); err == nil {
		for _, p := range cfg.Profiles {
			if p.Number != slot {
				continue
			}
			if out.Name == "" {
				out.Name = p.Name
			}
			if p.Light != nil {
				for _, d := range p.Light.States() {
					out.States = append(out.States, d.Name)
				}
			}
		}
		if n, err := apply.CheckPictures(cfg); err == nil {
			out.PicturesUsed = n
		}
	}
	return out, nil
}

type buttonErrorOut struct {
	Page     int    `json:"page"`
	Position int    `json:"position"`
	Field    string `json:"field"`
	Step     int    `json:"step"`
	Message  string `json:"message"`
}

func (s *Service) saveDeviceProfile(args json.RawMessage) (any, error) {
	var a struct {
		Device  string       `json:"device"`
		Profile int          `json:"profile"`
		Version string       `json:"version"`
		Buttons []buttonJSON `json:"buttons"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("saveDeviceProfile: bad arguments: %w", err)
	}
	slot, err := kb7Slot(a.Device, a.Profile)
	if err != nil {
		return nil, fmt.Errorf("saveDeviceProfile: %w", err)
	}
	files := make([]layout.FileButton, 0, len(a.Buttons))
	var bad []buttonErrorOut
	for _, b := range a.Buttons {
		fb, err := toFile(b)
		var fe *layout.FieldError
		if errors.As(err, &fe) {
			bad = append(bad, buttonErrorOut{b.Page, b.Position, fe.Field, fe.Step, fe.Error()})
			continue
		}
		files = append(files, fb)
	}
	if len(bad) > 0 {
		return map[string]any{"ok": false, "errors": bad}, nil
	}

	res, err := saveButtons(s.d.Dir, slot, a.Version, files)
	return s.saveAnswer("saveDeviceProfile", "This profile was changed outside Commander since you opened it.", res, err,
		func(err error) map[string]any {
			var be *store.ButtonError
			if errors.As(err, &be) {
				return map[string]any{"ok": false, "errors": []buttonErrorOut{{be.Page, be.Position, be.Err.Field, be.Err.Step, be.Err.Error()}}}
			}
			return nil
		}), nil
}

// saveAnswer turns the outcome of a save into the window's answer and has
// Commander reload or apply as the save requires. changed is the sentence
// for a section changed on disk; refused answers the refusals that concern
// the saved section, or returns nil for an error it does not know. Any other
// error is logged and answered as it is, with a reload when files were
// written before it.
func (s *Service) saveAnswer(call, changed string, res store.Result, err error, refused func(error) map[string]any) map[string]any {
	if errors.Is(err, store.ErrChanged) {
		return map[string]any{"ok": false, "changed": true, "error": changed}
	}
	if err != nil {
		if out := refused(err); out != nil {
			return out
		}
		s.logf("control: %s: %v", call, err)
		if res.Written && s.d.Reload != nil {
			// Some files changed: the running configuration is stale.
			s.d.Reload()
		}
		return map[string]any{"ok": false, "error": err.Error()}
	}
	switch res.Change {
	case store.Reload:
		if s.d.Reload != nil {
			s.d.Reload()
		}
	case store.Apply:
		if s.d.Apply != nil {
			s.d.Apply()
		}
	}
	backups := res.Backups
	if backups == nil {
		backups = []string{}
	}
	return map[string]any{"ok": true, "version": res.Version, "change": res.Change.String(), "backups": backups, "migrated": res.Migrated}
}

func (s *Service) renderButton(args json.RawMessage) (any, error) {
	var a struct {
		Button buttonJSON `json:"button"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("renderButton: bad arguments: %w", err)
	}
	fb, err := toFile(a.Button)
	if err != nil {
		return nil, fmt.Errorf("renderButton: %w", err)
	}
	b, err := layout.PreviewButton(s.d.Dir, fb)
	if err != nil {
		var fe *layout.FieldError
		if errors.As(err, &fe) {
			return nil, fmt.Errorf("renderButton: %s: %w", fe.Field, err)
		}
		return nil, fmt.Errorf("renderButton: %w", err)
	}
	img, err := apply.RenderButton(b)
	if err != nil {
		field := "symbol"
		if fb.Picture != "" {
			field = "picture"
		}
		return nil, fmt.Errorf("renderButton: the %s cannot be read: %w", field, err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("renderButton: %w", err)
	}
	return map[string]string{"png": "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())}, nil
}

func (s *Service) importPicture(args json.RawMessage) (any, error) {
	var a struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("importPicture: bad arguments: %w", err)
	}
	rel, err := store.ImportPicture(s.d.Dir, a.Path, a.Kind)
	if err != nil {
		return nil, fmt.Errorf("importPicture: %w", err)
	}
	return map[string]string{"path": rel}, nil
}

// saveButtons is store.SaveButtons; tests replace it.
var saveButtons = store.SaveButtons
