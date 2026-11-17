// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"image/gif"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/light"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
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

var lightBoard = color.NRGBA{R: 0x1A, G: 0x1A, B: 0x1A, A: 0xFF}

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
	var maxX, maxY float64
	for i := 0; i < light.NumLEDs; i++ {
		if pt, ok := light.Pos(i); ok {
			leds = append(leds, i)
			maxX, maxY = math.Max(maxX, pt.X), math.Max(maxY, pt.Y)
		}
	}
	// Positions are centres in key units; the origin is the centre of Escape.
	w := int(math.Ceil(maxX*lightCell)) + 2*lightMargin
	h := int(math.Ceil(maxY*lightCell)) + 2*lightMargin

	script := lightScript(stack)
	next := 0
	g := &gif.GIF{}
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
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.Draw(img, img.Bounds(), image.NewUniform(lightBoard), image.Point{}, draw.Src)
		for _, i := range leds {
			pt, _ := light.Pos(i)
			r, gr, b := ledColour(pl, i)
			cx := lightMargin + int(math.Round(pt.X*lightCell))
			cy := lightMargin + int(math.Round(pt.Y*lightCell))
			fillSquare(img, cx, cy, color.NRGBA{R: r, G: gr, B: b, A: 0xFF})
		}
		frame := image.NewPaletted(img.Bounds(), palette.Plan9)
		draw.FloydSteinberg.Draw(frame, frame.Bounds(), img, image.Point{})
		g.Image = append(g.Image, frame)
		g.Delay = append(g.Delay, lightDelay)
	}
	return g
}

// ledColour reads the colour of LED i from a payload.
func ledColour(pl *proto.LightPayload, i int) (r, g, b byte) {
	base := i/16*48 + i%16
	return pl[base], pl[base+16], pl[base+32]
}

// fillSquare draws a square of lightSquare pixels centred on (cx, cy) with
// rounded corners.
func fillSquare(img *image.NRGBA, cx, cy int, c color.NRGBA) {
	half := lightSquare / 2
	for y := 0; y < lightSquare; y++ {
		for x := 0; x < lightSquare; x++ {
			// Distance past the straight part of the nearest corner.
			dx := max(lightRound-x, x-(lightSquare-1-lightRound), 0)
			dy := max(lightRound-y, y-(lightSquare-1-lightRound), 0)
			if dx*dx+dy*dy > lightRound*lightRound {
				continue
			}
			img.SetNRGBA(cx-half+x, cy-half+y, c)
		}
	}
}

func writeGIF(path string, g *gif.GIF) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	return gif.EncodeAll(f, g)
}
