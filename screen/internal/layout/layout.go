// SPDX-License-Identifier: GPL-2.0-or-later

// Package layout loads the configuration folder: one TOML file per profile.
package layout

import (
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/Bananenbaas/commander/screen/internal/action"
	"github.com/Bananenbaas/commander/screen/internal/light"
	"github.com/Bananenbaas/commander/screen/internal/proto"
)

// Mode says how a button's action is carried out.
type Mode int

const (
	// Tap presses each chord and releases it shortly after.
	Tap Mode = iota
	// Hold keeps one chord down for as long as the touch lasts.
	Hold
)

// Button is one configured button.
type Button struct {
	Page, Position int
	Title, Label   string
	Symbol         string // absolute path, or empty
	Picture        string // absolute path, or empty
	Color          color.NRGBA
	Mode           Mode
	Action         []action.Step
	State          string        // name of a lighting state this button flips, or empty
	Confirm        time.Duration // how long the touch must last before a tap acts; 0 = acts at once
}

// Profile is the content of one layout file.
type Profile struct {
	Number            byte
	File              string
	Name              string // the profile's name on the keyboard, or empty to leave it
	SwitchWhenRunning string
	Buttons           []Button
	Light             *light.Stack // nil when the layout has no [light]
	StoredLight       *StoredLight // nil when the layout has no [stored_light]
}

// StoredLight is the [stored_light] table: what the keyboard's stored
// lighting of the profile is to hold.
type StoredLight struct {
	Color      color.NRGBA // every LED; alpha 0 when not given
	Brightness int         // percent, 0 to 100; -1 when not given
}

// Config is a loaded configuration folder.
type Config struct {
	Dir      string
	Profiles []Profile
	Games    []Game // without Desktop; games/ files by file name, then games built from layouts by profile number
	Desktop  *Game  // from games/desktop.toml, nil when there is none; when set, its KB7 is set
}

type rawFile struct {
	Profile           *int        `toml:"profile"`
	Name              *string     `toml:"name"`
	SwitchWhenRunning string      `toml:"switch_when_running"`
	Light             *rawLight   `toml:"light"`
	StoredLight       *rawStored  `toml:"stored_light"`
	Buttons           []rawButton `toml:"button"`
}

type rawLight struct {
	FPS    int64            `toml:"fps"`
	Layers []map[string]any `toml:"layer"`
}

type rawStored struct {
	Color      string `toml:"color"`
	Brightness *int64 `toml:"brightness"`
}

type rawButton struct {
	Page     *int     `toml:"page"`
	Position *int     `toml:"position"`
	Title    string   `toml:"title"`
	Label    string   `toml:"label"`
	Color    string   `toml:"color"`
	Symbol   string   `toml:"symbol"`
	Picture  string   `toml:"picture"`
	Action   []string `toml:"action"`
	Mode     string   `toml:"mode"`
	State    string   `toml:"state"`
	Confirm  string   `toml:"confirm"`
}

var defaultColor = color.NRGBA{R: 0x00, G: 0xAA, B: 0xFF, A: 0xFF}

// maxProfile is the highest of the keyboard's five profiles.
const maxProfile = 4

// Load reads every *.toml file directly in dir, sorted by name.
func Load(dir string) (*Config, error) {
	return LoadWith(dir, nil)
}

// LoadWith loads dir as Load does, but takes the bytes of the files named in
// files (paths relative to dir with forward slashes, such as "kb7-sc.toml" or
// "games/sc.toml") from the map instead of the disk; a name that does not
// exist on disk is added. Pictures and symbols are still looked up on disk.
func LoadWith(dir string, files map[string][]byte) (*Config, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	var names []string
	seen := map[string]bool{}
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".toml") {
			names = append(names, e.Name())
			seen[e.Name()] = true
		}
	}
	for n := range files {
		if !strings.Contains(n, "/") && strings.EqualFold(filepath.Ext(n), ".toml") && !seen[n] {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("no *.toml layout files in %s", abs)
	}
	cfg := &Config{Dir: abs}
	owner := map[byte]string{}
	for _, name := range names {
		data, err := fileBytes(abs, name, files)
		if err != nil {
			return nil, err
		}
		p, err := loadFile(abs, filepath.Join(abs, name), data)
		if err != nil {
			return nil, err
		}
		if prev, ok := owner[p.Number]; ok {
			return nil, fmt.Errorf("%s and %s both define profile %d", prev, name, p.Number)
		}
		owner[p.Number] = name
		cfg.Profiles = append(cfg.Profiles, *p)
	}
	cfg.Games, cfg.Desktop, err = loadGames(abs, cfg.Profiles, files)
	if err != nil {
		return nil, err
	}
	if cfg.Desktop == nil && len(cfg.claimed()) == maxProfile+1 {
		return nil, fmt.Errorf("every profile 0 to %d is the kb7 profile of a game (switch_when_running or games/), so no profile is left for the desktop; free one or add games/desktop.toml", maxProfile)
	}
	return cfg, nil
}

// fileBytes returns the bytes of the file rel (slashes, relative to dir): from
// files when it is there, else from the disk.
func fileBytes(dir, rel string, files map[string][]byte) ([]byte, error) {
	if b, ok := files[rel]; ok {
		return b, nil
	}
	return os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
}

func loadFile(dir, path string, data []byte) (*Profile, error) {
	name := filepath.Base(path)
	var raw rawFile
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if un := md.Undecoded(); len(un) > 0 {
		keys := make([]string, len(un))
		for i, k := range un {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("%s: unknown key(s): %s", name, strings.Join(keys, ", "))
	}
	if raw.Profile == nil {
		return nil, fmt.Errorf("%s: profile is required", name)
	}
	if *raw.Profile < 0 || *raw.Profile > maxProfile {
		return nil, fmt.Errorf("%s: profile must be 0 to %d, got %d", name, maxProfile, *raw.Profile)
	}
	p := &Profile{Number: byte(*raw.Profile), File: path, SwitchWhenRunning: raw.SwitchWhenRunning}
	if raw.Name != nil {
		if err := proto.CheckName(*raw.Name); err != nil {
			return nil, fmt.Errorf("%s: name: %w", name, err)
		}
		p.Name = *raw.Name
	}
	if raw.Light != nil {
		stack, err := buildLight(raw.Light)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		p.Light = stack
	}
	if raw.StoredLight != nil {
		sl, err := buildStored(raw.StoredLight)
		if err != nil {
			return nil, fmt.Errorf("%s: stored_light: %w", name, err)
		}
		p.StoredLight = sl
	}
	seen := map[[2]int]bool{}
	for i, rb := range raw.Buttons {
		if rb.Page == nil || rb.Position == nil {
			return nil, fmt.Errorf("%s: button %d: page and position are required", name, i+1)
		}
		where := fmt.Sprintf("%s: page %d position %d", name, *rb.Page, *rb.Position)
		b, err := buildButton(dir, rb, false)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
		key := [2]int{b.Page, b.Position}
		if seen[key] {
			return nil, fmt.Errorf("%s: duplicate button", where)
		}
		seen[key] = true
		if b.State != "" {
			if err := checkState(stateNames(p.Light), b.State); err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
		}
		p.Buttons = append(p.Buttons, b)
	}
	return p, nil
}

// stateNames lists the lighting states a stack declares; nil for no stack.
func stateNames(st *light.Stack) []string {
	if st == nil {
		return nil
	}
	names := []string{}
	for _, d := range st.States() {
		names = append(names, d.Name)
	}
	return names
}

// layerError is a light.LayerError with the file's layer number; its text is
// the form Load has always printed, "light.layer N: ...".
type layerError struct{ le *light.LayerError }

func (e *layerError) Error() string { return "light." + e.le.Error() }
func (e *layerError) Unwrap() error { return e.le }

// LightError reads the lighting layer (1-based, counting every layer in the
// file, switched-off ones included) and the setting a load error is about.
// setting is "" when the error concerns the layer as a whole; ok is false for
// an error that is not about one lighting layer.
func LightError(err error) (layer int, setting string, ok bool) {
	var le *light.LayerError
	if !errors.As(err, &le) {
		return 0, "", false
	}
	return le.Layer, le.Setting, true
}

var declaredBy = regexp.MustCompile(`is already declared by layer (\d+)$`)

// buildLight turns the [light] table into a stack. Errors name light.layer N
// (1-based, as the file numbers its layers) and the setting. A layer with
// enabled = false stays out of the stack.
func buildLight(rl *rawLight) (*light.Stack, error) {
	cfg := light.Config{FPS: int(rl.FPS)}
	if rl.FPS != int64(cfg.FPS) {
		return nil, fmt.Errorf("light: fps %d is out of range", rl.FPS)
	}
	var fileNo []int // file layer number of each layer passed to Build
	fail := func(n int, setting, format string, args ...any) error {
		return &layerError{&light.LayerError{Layer: n, Setting: setting, Err: fmt.Errorf(format, args...)}}
	}
	for i, m := range rl.Layers {
		n := i + 1
		lc := light.LayerConfig{Settings: map[string]any{}}
		off := false
		for k, v := range m {
			switch k {
			case "enabled":
				on, ok := v.(bool)
				if !ok {
					return nil, fail(n, k, "enabled must be true or false")
				}
				off = !on
			case "effect":
				e, ok := v.(string)
				if !ok {
					return nil, fail(n, k, "effect must be a string")
				}
				lc.Effect = e
			case "keys":
				list, ok := v.([]any)
				if !ok {
					return nil, fail(n, k, "keys must be a list of key names")
				}
				for _, x := range list {
					ks, ok := x.(string)
					if !ok {
						return nil, fail(n, k, "keys must be a list of key names")
					}
					lc.Keys = append(lc.Keys, ks)
				}
			default:
				lc.Settings[k] = v
			}
		}
		if lc.Effect == "" {
			return nil, fail(n, "effect", "effect is required")
		}
		if off {
			continue
		}
		cfg.Layers = append(cfg.Layers, lc)
		fileNo = append(fileNo, n)
	}
	if len(rl.Layers) > 0 && len(cfg.Layers) == 0 {
		return nil, fmt.Errorf("light: every lighting layer is off; turn one on or remove the lighting")
	}
	stack, err := light.Build(cfg)
	if err != nil {
		var le *light.LayerError
		if errors.As(err, &le) && le.Layer >= 1 && le.Layer <= len(fileNo) {
			out := *le
			out.Layer = fileNo[le.Layer-1]
			// "state ... is already declared by layer K" names a Build layer too.
			if m := declaredBy.FindStringSubmatch(le.Err.Error()); m != nil {
				if k, _ := strconv.Atoi(m[1]); k >= 1 && k <= len(fileNo) {
					text := le.Err.Error()
					out.Err = errors.New(text[:len(text)-len(m[1])] + strconv.Itoa(fileNo[k-1]))
				}
			}
			return nil, &layerError{&out}
		}
		return nil, fmt.Errorf("light: %s", err.Error())
	}
	return stack, nil
}

// buildStored checks the [stored_light] table. It must give a colour, a
// brightness or both; the LEDs have no transparency, so the colour must be
// opaque.
func buildStored(rs *rawStored) (*StoredLight, error) {
	sl := &StoredLight{Brightness: -1}
	if rs.Color == "" && rs.Brightness == nil {
		return nil, fmt.Errorf("give a color, a brightness or both")
	}
	if rs.Color != "" {
		c, err := parseColor(rs.Color)
		if err != nil {
			return nil, err
		}
		if c.A != 0xFF {
			return nil, fmt.Errorf("color must be opaque, got %q", rs.Color)
		}
		sl.Color = c
	}
	if rs.Brightness != nil {
		if *rs.Brightness < 0 || *rs.Brightness > 100 {
			return nil, fmt.Errorf("brightness must be 0 to 100, got %d", *rs.Brightness)
		}
		sl.Brightness = int(*rs.Brightness)
	}
	return sl, nil
}

// checkState makes sure a button's state is one the profile's [light]
// declares; states is nil when the layout has no [light].
func checkState(states []string, name string) error {
	if states == nil {
		return fieldErr("state", fmt.Errorf("state %q: the layout has no [light]", name))
	}
	for _, d := range states {
		if d == name {
			return nil
		}
	}
	return fieldErr("state", fmt.Errorf("state %q is not declared by any light layer of this layout", name))
}

func buildButton(dir string, rb rawButton, preview bool) (Button, error) {
	b := Button{Page: *rb.Page, Position: *rb.Position, Title: rb.Title, Label: rb.Label, Color: defaultColor, State: rb.State}
	if b.Page < 1 || b.Page > 3 {
		return b, fieldErr("position", fmt.Errorf("page must be 1 to 3"))
	}
	if b.Position < 0 || b.Position > 11 {
		return b, fieldErr("position", fmt.Errorf("position must be 0 to 11"))
	}
	if b.Page == 3 && b.Position == 11 {
		return b, fieldErr("position", fmt.Errorf("page 3 has positions 0 to 10 only"))
	}
	if len(b.Label) > 11 {
		return b, fieldErr("label", fmt.Errorf("label must be at most 11 characters"))
	}
	for _, r := range b.Label {
		if r < 0x20 || r > 0x7e {
			return b, fieldErr("label", fmt.Errorf("label must be printable ASCII"))
		}
	}
	if rb.Color != "" {
		c, err := parseColor(rb.Color)
		if err != nil {
			return b, fieldErr("color", err)
		}
		b.Color = c
	}
	if rb.Symbol != "" && rb.Picture != "" {
		return b, fieldErr("picture", fmt.Errorf("symbol and picture cannot both be set"))
	}
	var err error
	if rb.Symbol != "" {
		if b.Symbol, err = resolveFile(dir, "symbol", rb.Symbol); err != nil {
			return b, fieldErr("symbol", err)
		}
	}
	if rb.Picture != "" {
		if b.Picture, err = resolveFile(dir, "picture", rb.Picture); err != nil {
			return b, fieldErr("picture", err)
		}
	}
	if preview {
		return b, nil
	}
	if len(rb.Action) == 0 && rb.State == "" {
		return b, fieldErr("action", fmt.Errorf("action must hold at least one step"))
	}
	for i, s := range rb.Action {
		st, err := action.ParseStep(s)
		if err != nil {
			return b, &FieldError{Field: "action", Step: i, Err: fmt.Errorf("action %q: %w", s, err)}
		}
		b.Action = append(b.Action, st)
	}
	switch rb.Mode {
	case "", "tap":
		b.Mode = Tap
	case "hold":
		b.Mode = Hold
		if len(b.Action) == 0 {
			return b, fieldErr("mode", fmt.Errorf("hold needs an action; a button with only a state is a tap"))
		}
		if _, ok := b.Action[0].(action.Chord); len(b.Action) != 1 || !ok {
			return b, fieldErr("mode", fmt.Errorf("hold needs exactly one step and it must be a key chord"))
		}
	default:
		return b, fieldErr("mode", fmt.Errorf("mode must be \"tap\" or \"hold\", got %q", rb.Mode))
	}
	if rb.Confirm != "" {
		if b.Mode == Hold {
			return b, fieldErr("confirm", fmt.Errorf("confirm cannot be used with mode \"hold\""))
		}
		if len(b.Action) == 0 {
			return b, fieldErr("confirm", fmt.Errorf("confirm needs an action; a button with only a state has nothing to confirm"))
		}
		if b.Confirm, err = parseConfirm(rb.Confirm); err != nil {
			return b, fieldErr("confirm", err)
		}
	}
	return b, nil
}

// FieldError is a button error tied to the field it concerns. Its Error()
// is Err's text, unchanged, so the messages of Load stay as they are.
type FieldError struct {
	Field string // "position", "title", "label", "color", "symbol", "picture", "action", "mode", "state", "confirm"
	Step  int    // index into the action for Field "action" when one step is wrong, else -1
	Err   error
}

func (e *FieldError) Error() string { return e.Err.Error() }
func (e *FieldError) Unwrap() error { return e.Err }

func fieldErr(field string, err error) *FieldError {
	return &FieldError{Field: field, Step: -1, Err: err}
}

// PreviewButton builds what a button's picture shows (position, title, label,
// colour, symbol and picture, the files resolved against dir) with the rules
// of Load. Its steps, mode, state and confirm are not looked at, so a button
// that is still being made can be drawn. It returns nil or a *FieldError.
func PreviewButton(dir string, b FileButton) (Button, error) {
	page, pos := b.Page, b.Position
	return buildButton(dir, rawButton{
		Page: &page, Position: &pos,
		Title: b.Title, Label: b.Label, Color: b.Color, Symbol: b.Symbol, Picture: b.Picture,
	}, true)
}

// CheckButton checks one button as Load would, against the light states
// the profile declares (nil = no [light]). It returns nil or a *FieldError.
func CheckButton(dir string, b FileButton, states []string) error {
	page, pos := b.Page, b.Position
	rb := rawButton{
		Page: &page, Position: &pos,
		Title: b.Title, Label: b.Label, Color: b.Color, Symbol: b.Symbol, Picture: b.Picture,
		Action: b.Action, Mode: b.Mode, State: b.State, Confirm: b.Confirm,
	}
	built, err := buildButton(dir, rb, false)
	if err != nil {
		return err
	}
	if built.State != "" {
		if err := checkState(states, built.State); err != nil {
			return err
		}
	}
	return nil
}

// Limits of a confirm duration.
const (
	minConfirm = 200 * time.Millisecond
	maxConfirm = 5 * time.Second
)

// parseConfirm reads "hold <duration>".
func parseConfirm(s string) (time.Duration, error) {
	bad := fmt.Errorf("confirm must be \"hold <duration>\", e.g. \"hold 1s\"")
	rest, ok := strings.CutPrefix(s, "hold ")
	if !ok || rest == "" || strings.TrimSpace(rest) != rest {
		return 0, bad
	}
	d, err := time.ParseDuration(rest)
	if err != nil {
		return 0, bad
	}
	if d < minConfirm || d > maxConfirm {
		return 0, fmt.Errorf("confirm duration must be from %v to %v, got %v", minConfirm, maxConfirm, d)
	}
	return d, nil
}

func resolveFile(dir, what, rel string) (string, error) {
	p := filepath.Join(dir, filepath.FromSlash(rel))
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return "", fmt.Errorf("%s file %q does not exist", what, rel)
	}
	return p, nil
}

func parseColor(s string) (color.NRGBA, error) {
	h, ok := strings.CutPrefix(s, "#")
	if !ok || (len(h) != 6 && len(h) != 8) {
		return color.NRGBA{}, fmt.Errorf("color must be #RRGGBB or #RRGGBBAA, got %q", s)
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return color.NRGBA{}, fmt.Errorf("color must be #RRGGBB or #RRGGBBAA, got %q", s)
	}
	if len(h) == 6 {
		v = v<<8 | 0xFF
	}
	return color.NRGBA{R: byte(v >> 24), G: byte(v >> 16), B: byte(v >> 8), A: byte(v)}, nil
}

// claimed returns the profiles that a program is tied to: by a layout's
// switch_when_running or by the kb7 of a game.
func (c *Config) claimed() map[byte]bool {
	m := map[byte]bool{}
	for _, p := range c.Profiles {
		if p.SwitchWhenRunning != "" {
			m[p.Number] = true
		}
	}
	for _, g := range c.Games {
		if g.KB7 != nil {
			m[*g.KB7] = true
		}
	}
	return m
}

// DesktopProfile returns the profile the keyboard is to show while no game
// runs. With a games/desktop.toml it is that file's kb7. Otherwise it is the
// lowest profile that has a layout and that no program is tied to, by its
// switch_when_running or by a game's kb7. When every layout's profile has a
// program, it is the lowest profile, 0 to 4, that has none; that profile has
// no layout, and its touches are not acted on. Only when all five profiles
// have a program is it profile 0.
func (c *Config) DesktopProfile() byte {
	if c.Desktop != nil {
		return *c.Desktop.KB7
	}
	claimed := c.claimed()
	best := -1
	for _, p := range c.Profiles {
		if !claimed[p.Number] && (best < 0 || int(p.Number) < best) {
			best = int(p.Number)
		}
	}
	if best >= 0 {
		return byte(best)
	}
	for n := byte(0); n <= maxProfile; n++ {
		if !claimed[n] {
			return n
		}
	}
	return 0
}
