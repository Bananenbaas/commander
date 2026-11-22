// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/action"
	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/light"
	"github.com/Bananenbaas/commander/screen/internal/proto"
)

const (
	lightFrames   = 80                    // 4 s at 20 fps
	lightStep     = 50 * time.Millisecond // one frame
	lightDelay    = 5                     // 1/100 s per frame
	lightCell     = 40                    // pixels per key unit
	lightSquare   = 34                    // pixels per LED square
	lightRound    = 3                     // corner rounding in pixels
	lightMargin   = 30                    // pixels around the outer LEDs
	lightPressGap = 100 * time.Millisecond
)

var lightBoard = color.RGBA{R: 0x1A, G: 0x1A, B: 0x1A, A: 0xFF}

// lightCanvas maps key units to pixels so that every mapped LED fits: some
// lights (media keys, top buttons) lie above the main block, at negative Y.
type lightCanvas struct {
	minX, minY float64
	w, h       int
}

func newLightCanvas() lightCanvas {
	first := true
	var minX, minY, maxX, maxY float64
	for i := 0; i < light.NumLEDs; i++ {
		pt, ok := light.Pos(i)
		if !ok {
			continue
		}
		if first {
			minX, minY, maxX, maxY = pt.X, pt.Y, pt.X, pt.Y
			first = false
		}
		minX, maxX = math.Min(minX, pt.X), math.Max(maxX, pt.X)
		minY, maxY = math.Min(minY, pt.Y), math.Max(maxY, pt.Y)
	}
	return lightCanvas{
		minX: minX, minY: minY,
		w: int(math.Ceil((maxX-minX)*lightCell)) + 2*lightMargin,
		h: int(math.Ceil((maxY-minY)*lightCell)) + 2*lightMargin,
	}
}

// pixel returns the centre of an LED in the image.
func (c lightCanvas) pixel(pt light.Point) (x, y int) {
	return lightMargin + int(math.Round((pt.X-c.minX)*lightCell)),
		lightMargin + int(math.Round((pt.Y-c.minY)*lightCell))
}

// lightEvent is a key event or a state flip at a time of the simulation.
type lightEvent struct {
	at   time.Duration
	key  action.Key
	down bool
	flip string // when set, flip this state instead of pressing a key
}

func doLightPreview(c command, stdout io.Writer) error {
	cfg, err := layout.Load(c.config)
	if err != nil {
		return err
	}
	var stack *light.Stack
	for _, p := range cfg.Profiles {
		if int(p.Number) == c.profile {
			stack = p.Light
			break
		}
	}
	if stack == nil {
		return fmt.Errorf("profile %d has no [light] part in %s", c.profile, cfg.Dir)
	}
	out := c.out
	if out == "" {
		out = filepath.Join(cfg.Dir, "previews", fmt.Sprintf("light-%d.gif", c.profile))
	}
	g := renderLightGIF(byte(c.profile), stack)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := writeGIF(out, g); err != nil {
		return err
	}
	fmt.Fprintln(stdout, out)
	return nil
}

// lightScript lists what the simulation does: W at 0.5 s, Space at 1.5 s and,
// at 2.5 s, the toggle of every state (or a flip for a state without one).
func lightScript(stack *light.Stack) []lightEvent {
	ev := []lightEvent{
		{at: 500 * time.Millisecond, key: action.W, down: true},
		{at: 600 * time.Millisecond, key: action.W},
		{at: 1500 * time.Millisecond, key: action.Space, down: true},
		{at: 1600 * time.Millisecond, key: action.Space},
	}
	const t = 2500 * time.Millisecond
	for _, s := range stack.States() {
		if s.Toggle == nil || len(s.Toggle.Keys) == 0 {
			ev = append(ev, lightEvent{at: t, flip: s.Name})
			continue
		}
		for _, k := range s.Toggle.Keys {
			ev = append(ev, lightEvent{at: t, key: k, down: true})
		}
		for _, k := range s.Toggle.Keys {
			ev = append(ev, lightEvent{at: t + lightPressGap, key: k})
		}
	}
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].at < ev[j].at })
	return ev
}

// renderLightGIF runs the engine on a fixed clock and draws one frame per
// step from the payload the keyboard would receive.
func renderLightGIF(p byte, stack *light.Stack) *gif.GIF {
	t0 := time.Unix(1_000_000, 0)
	e := light.NewEngine(map[byte]*light.Stack{p: stack}, t0)
	e.Start(p)
	e.SetActive(p)
	e.SetBrightness(100)

	var leds []int
	for i := 0; i < light.NumLEDs; i++ {
		if _, ok := light.Pos(i); ok {
			leds = append(leds, i)
		}
	}
	cv := newLightCanvas()
	w, h := cv.w, cv.h

	script := lightScript(stack)
	next := 0
	var colours [][]color.RGBA
	for k := 0; k < lightFrames; k++ {
		at := time.Duration(k) * lightStep
		for next < len(script) && script[next].at <= at {
			ev := script[next]
			next++
			if ev.flip != "" {
				e.Flip(p, ev.flip)
			} else {
				e.Key(ev.key, ev.down, t0.Add(ev.at))
			}
		}
		pl := e.Payload(t0.Add(at))
		cols := make([]color.RGBA, len(leds))
		for n, i := range leds {
			r, gr, b := ledColour(pl, i)
			cols[n] = color.RGBA{R: r, G: gr, B: b, A: 0xFF}
		}
		colours = append(colours, cols)
	}

	pal, index := lightPalette(colours)
	g := &gif.GIF{}
	for _, cols := range colours {
		frame := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		bg := index(lightBoard)
		for k := range frame.Pix {
			frame.Pix[k] = bg
		}
		for n, i := range leds {
			pt, _ := light.Pos(i)
			cx, cy := cv.pixel(pt)
			fillSquare(frame, cx, cy, index(cols[n]))
		}
		g.Image = append(g.Image, frame)
		g.Delay = append(g.Delay, lightDelay)
	}
	return g
}

// lightPalette returns the palette for the whole GIF and a function that maps
// a colour to its index. The squares are flat colours, so no dithering is
// needed: when the board and all LED colours of all frames number at most 256,
// the palette holds exactly those colours; otherwise it is palette.Plan9 and
// each colour maps to its nearest entry (looked up once per distinct colour).
func lightPalette(frames [][]color.RGBA) (color.Palette, func(color.RGBA) uint8) {
	board := lightBoard
	seen := map[color.RGBA]uint8{board: 0}
	pal := color.Palette{board}
	exact := true
	for _, f := range frames {
		for _, c := range f {
			if _, ok := seen[c]; ok {
				continue
			}
			if len(pal) == 256 {
				exact = false
				break
			}
			seen[c] = uint8(len(pal))
			pal = append(pal, c)
		}
		if !exact {
			break
		}
	}
	if exact {
		return pal, func(c color.RGBA) uint8 { return seen[c] }
	}
	plan9 := color.Palette(palette.Plan9)
	cache := map[color.RGBA]uint8{}
	return plan9, func(c color.RGBA) uint8 {
		if i, ok := cache[c]; ok {
			return i
		}
		i := uint8(plan9.Index(c))
		cache[c] = i
		return i
	}
}

// ledColour reads the colour of LED i from a payload.
func ledColour(pl *proto.LightPayload, i int) (r, g, b byte) {
	base := i/16*48 + i%16
	return pl[base], pl[base+16], pl[base+32]
}

// fillSquare draws a square of lightSquare pixels centred on (cx, cy) with
// rounded corners.
func fillSquare(img *image.Paletted, cx, cy int, c uint8) {
	half := lightSquare / 2
	for y := 0; y < lightSquare; y++ {
		for x := 0; x < lightSquare; x++ {
			// Distance past the straight part of the nearest corner.
			dx := max(lightRound-x, x-(lightSquare-1-lightRound), 0)
			dy := max(lightRound-y, y-(lightSquare-1-lightRound), 0)
			if dx*dx+dy*dy > lightRound*lightRound {
				continue
			}
			img.SetColorIndex(cx-half+x, cy-half+y, c)
		}
	}
}

// writeGIF encodes g into a temporary file next to path and renames it into
// place, so a failed encode leaves no partial GIF.
func writeGIF(path string, g *gif.GIF) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".light-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	err = gif.EncodeAll(f, g)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
