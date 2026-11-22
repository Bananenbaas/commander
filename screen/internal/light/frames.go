// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"sort"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/action"
	"github.com/Bananenbaas/commander/screen/internal/proto"
)

// FrameStep and FrameCount are the preview's clock: 80 frames of 50 ms.
const (
	FrameStep  = 50 * time.Millisecond
	FrameCount = 80
)

const pressGap = 100 * time.Millisecond

// previewEvent is a key event or a state flip at a time of the simulation.
type previewEvent struct {
	at   time.Duration
	key  action.Key
	down bool
	flip string // when set, flip this state instead of pressing a key
}

// previewScript lists what the simulation does: W at 0.5 s, Space at 1.5 s
// and, at 2.5 s, the toggle of every state (or a flip for a state without one).
func previewScript(stack *Stack) []previewEvent {
	ev := []previewEvent{
		{at: 500 * time.Millisecond, key: action.W, down: true},
		{at: 600 * time.Millisecond, key: action.W},
		{at: 1500 * time.Millisecond, key: action.Space, down: true},
		{at: 1600 * time.Millisecond, key: action.Space},
	}
	const t = 2500 * time.Millisecond
	for _, s := range stack.States() {
		if s.Toggle == nil || len(s.Toggle.Keys) == 0 {
			ev = append(ev, previewEvent{at: t, flip: s.Name})
			continue
		}
		for _, k := range s.Toggle.Keys {
			ev = append(ev, previewEvent{at: t, key: k, down: true})
		}
		for _, k := range s.Toggle.Keys {
			ev = append(ev, previewEvent{at: t + pressGap, key: k})
		}
	}
	sort.SliceStable(ev, func(i, j int) bool { return ev[i].at < ev[j].at })
	return ev
}

// Frames runs the engine for stack on a fixed clock with the preview script
// (W at 0.5 s, Space at 1.5 s, every state flipped at 2.5 s, by its toggle
// when it has one) and returns the payload the keyboard would receive at
// each step.
func Frames(stack *Stack) []*proto.LightPayload {
	const p = 1
	t0 := time.Unix(1_000_000, 0)
	e := NewEngine(map[byte]*Stack{p: stack}, t0)
	e.Start(p)
	e.SetActive(p)
	e.SetBrightness(100)

	script := previewScript(stack)
	next := 0
	frames := make([]*proto.LightPayload, 0, FrameCount)
	for k := 0; k < FrameCount; k++ {
		at := time.Duration(k) * FrameStep
		for next < len(script) && script[next].at <= at {
			ev := script[next]
			next++
			if ev.flip != "" {
				e.Flip(p, ev.flip)
			} else {
				e.Key(ev.key, ev.down, t0.Add(ev.at))
			}
		}
		frames = append(frames, e.Payload(t0.Add(at)))
	}
	return frames
}
