// SPDX-License-Identifier: GPL-2.0-or-later

package light

import (
	"testing"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

var engineStart = time.Unix(1_000_000, 0)

func at(d time.Duration) time.Time { return engineStart.Add(d) }

// rgbOf reads LED led out of a payload: block led/16, offsets +0, +16, +32.
func rgbOf(p *proto.LightPayload, led int) [3]byte {
	base := led/16*48 + led%16
	return [3]byte{p[base], p[base+16], p[base+32]}
}

func ledOf(t *testing.T, k action.Key) int {
	t.Helper()
	i, ok := LEDOfKey(k)
	if !ok {
		t.Fatalf("key %v has no LED", k)
	}
	return i
}

func stateLayer(name, toggle, start, key, col string) LayerConfig {
	s := map[string]any{"name": name, "color": col, "start": start}
	if toggle != "" {
		s["toggle"] = toggle
	}
	return LayerConfig{Effect: "state", Keys: []string{key}, Settings: s}
}

func staticLayer(col string) LayerConfig {
	return LayerConfig{Effect: "static", Settings: map[string]any{"color": col}}
}

// gearStack is black with a state "gear" (toggle N) that lights N green.
func gearStack(t *testing.T, start string) *Stack {
	return build(t, Config{Layers: []LayerConfig{
		staticLayer("#000000"),
		stateLayer("gear", "N", start, "N", "#00FF00"),
	}})
}

// lit reports whether key k shows green in the payload.
func lit(t *testing.T, e *Engine, k action.Key, when time.Time) bool {
	t.Helper()
	p := e.Payload(when)
	if p == nil {
		t.Fatal("Payload: nil for a lit profile")
	}
	return rgbOf(p, ledOf(t, k))[1] == 255
}

func twoProfiles(t *testing.T) *Engine {
	e := NewEngine(map[byte]*Stack{1: gearStack(t, "off"), 2: gearStack(t, "off")}, engineStart)
	e.SetActive(1)
	return e
}

func TestEngineToggleOnlyActiveProfile(t *testing.T) {
	e := twoProfiles(t)
	e.Key(action.N, true, at(time.Second))
	if !lit(t, e, action.N, at(time.Second)) {
		t.Error("profile 1: gear not flipped on")
	}
	e.SetActive(2)
	if lit(t, e, action.N, at(time.Second)) {
		t.Error("profile 2: gear flipped by a press on profile 1")
	}
}

func TestEngineAutoRepeatFlipsOnce(t *testing.T) {
	e := twoProfiles(t)
	e.Key(action.N, true, at(time.Second))
	e.Key(action.N, true, at(time.Second+30*time.Millisecond))
	if !lit(t, e, action.N, at(2*time.Second)) {
		t.Error("gear flipped twice by a held key")
	}
	if len(e.presses) != 1 {
		t.Errorf("presses = %d, want 1", len(e.presses))
	}
	e.Key(action.N, false, at(2*time.Second))
	e.Key(action.N, true, at(3*time.Second))
	if lit(t, e, action.N, at(3*time.Second)) {
		t.Error("second physical press did not flip gear back")
	}
	if len(e.presses) != 2 || e.presses[1].N != 1 {
		t.Errorf("presses = %+v, want two numbered 0 and 1", e.presses)
	}
}

func TestEngineModifiersMustMatchExactly(t *testing.T) {
	s := build(t, Config{Layers: []LayerConfig{
		staticLayer("#000000"),
		stateLayer("plain", "N", "off", "N", "#00FF00"),
		stateLayer("alt", "LAlt+N", "off", "M", "#00FF00"),
	}})
	e := NewEngine(map[byte]*Stack{1: s}, engineStart)
	e.SetActive(1)

	e.Key(action.LAlt, true, at(time.Second))
	e.Key(action.N, true, at(time.Second))
	if lit(t, e, action.N, at(time.Second)) {
		t.Error("LAlt+N flipped the N toggle")
	}
	if !lit(t, e, action.M, at(time.Second)) {
		t.Error("LAlt+N did not flip the LAlt+N toggle")
	}

	e.Key(action.N, false, at(2*time.Second))
	e.Key(action.LAlt, false, at(2*time.Second))
	e.Key(action.RAlt, true, at(2*time.Second))
	e.Key(action.N, true, at(2*time.Second))
	if lit(t, e, action.N, at(2*time.Second)) || !lit(t, e, action.M, at(2*time.Second)) {
		t.Error("RAlt+N matched N or LAlt+N")
	}

	e.Key(action.N, false, at(3*time.Second))
	e.Key(action.RAlt, false, at(3*time.Second))
	e.Key(action.N, true, at(3*time.Second))
	if !lit(t, e, action.N, at(3*time.Second)) {
		t.Error("N alone did not flip the N toggle")
	}
	if !lit(t, e, action.M, at(3*time.Second)) {
		t.Error("N alone flipped the LAlt+N toggle")
	}
}

func TestEngineSentFlipsWithoutPress(t *testing.T) {
	e := twoProfiles(t)
	e.Sent(action.Chord{Keys: []action.Key{action.N}})
	if !lit(t, e, action.N, at(time.Second)) {
		t.Error("Sent(N) did not flip gear")
	}
	if len(e.presses) != 0 {
		t.Errorf("Sent added %d presses", len(e.presses))
	}
	e.Sent(action.Chord{Keys: []action.Key{action.LAlt, action.N}})
	if !lit(t, e, action.N, at(time.Second)) {
		t.Error("Sent(LAlt+N) flipped the N toggle")
	}
}

func TestEngineFlipAndStart(t *testing.T) {
	e := NewEngine(map[byte]*Stack{1: gearStack(t, "on")}, engineStart)
	e.SetActive(1)
	if !lit(t, e, action.N, at(0)) {
		t.Fatal("gear with start = on is off before any flip")
	}
	if !e.Flip(1, "gear") {
		t.Fatal(`Flip(1, "gear") = false`)
	}
	if e.Flip(1, "nope") {
		t.Error(`Flip(1, "nope") = true`)
	}
	if e.Flip(7, "gear") {
		t.Error(`Flip on an unknown profile = true`)
	}
	if lit(t, e, action.N, at(0)) {
		t.Fatal("Flip did not turn gear off")
	}
	e.Start(1)
	if !lit(t, e, action.N, at(0)) {
		t.Error("Start did not restore start = on")
	}
	e.Start(7) // unlit: no-op, no panic
}

func TestEngineBrightness(t *testing.T) {
	s := build(t, Config{Layers: []LayerConfig{staticLayer("#FF0000")}})
	e := NewEngine(map[byte]*Stack{1: s}, engineStart)
	e.SetActive(1)
	led := ledOf(t, action.A)
	for _, c := range []struct {
		pct  int
		want byte
	}{{50, 127}, {100, 255}, {150, 255}, {-5, 0}, {1, 2}} {
		e.SetBrightness(c.pct)
		if got := rgbOf(e.Payload(at(0)), led); got != [3]byte{c.want, 0, 0} {
			t.Errorf("brightness %d: %v, want [%d 0 0]", c.pct, got, c.want)
		}
	}
}

func TestEngineUnlit(t *testing.T) {
	e := twoProfiles(t)
	if !e.Lit(1) || e.Lit(0) {
		t.Errorf("Lit(1) = %v, Lit(0) = %v", e.Lit(1), e.Lit(0))
	}
	e.SetActive(0)
	if e.Payload(at(0)) != nil {
		t.Error("Payload of an unlit profile is not nil")
	}
	if e.Rest(0) != nil {
		t.Error("Rest of an unlit profile is not nil")
	}
	if e.FPS() != 30 {
		t.Errorf("FPS unlit = %d, want 30", e.FPS())
	}
	e.Key(action.N, true, at(time.Second)) // no active stack: no panic
}

func TestEngineFPS(t *testing.T) {
	s := build(t, Config{FPS: 12, Layers: []LayerConfig{staticLayer("#FF0000")}})
	e := NewEngine(map[byte]*Stack{1: s}, engineStart)
	e.SetActive(1)
	if e.FPS() != 12 {
		t.Errorf("FPS = %d, want 12", e.FPS())
	}
}

func TestEngineRestIgnoresPressesAndStates(t *testing.T) {
	s := build(t, Config{Layers: []LayerConfig{
		staticLayer("#000000"),
		{Effect: "ripple", Settings: map[string]any{"color": "#FFFFFF"}},
		stateLayer("gear", "N", "on", "N", "#00FF00"),
	}})
	e := NewEngine(map[byte]*Stack{1: s}, engineStart)
	e.SetActive(1)
	e.SetBrightness(50)
	e.Key(action.A, true, at(100*time.Millisecond))

	var want proto.LightPayload
	for i, c := range s.Render(0, &Input{}) {
		proto.SetLED(&want, i, c.R/2, c.G/2, c.B/2)
	}
	r := e.Rest(1)
	if r == nil || *r != want {
		t.Error("Rest differs from the stack at t = 0 with no input at half brightness")
	}
	if rgbOf(r, ledOf(t, action.N)) != [3]byte{} {
		t.Error("Rest shows a state that is on")
	}
	if p := e.Payload(at(200 * time.Millisecond)); *p == want {
		t.Error("control: Payload with a ripple and gear on equals the rest frame")
	}
}

func TestEngineStatesSurviveSwitch(t *testing.T) {
	e := twoProfiles(t)
	e.Key(action.N, true, at(time.Second))
	e.SetActive(0)
	e.SetActive(1)
	if !lit(t, e, action.N, at(time.Second)) {
		t.Error("gear lost its value across SetActive(0); SetActive(1)")
	}
}

func TestEnginePrunesPresses(t *testing.T) {
	e := twoProfiles(t)
	for i := 0; i < 40; i++ {
		when := at(time.Duration(i) * time.Millisecond)
		e.Key(action.A, true, when)
		e.Key(action.A, false, when)
	}
	if len(e.presses) != 32 {
		t.Fatalf("presses = %d, want 32", len(e.presses))
	}
	if e.presses[0].N != 8 || e.presses[31].N != 39 {
		t.Errorf("kept presses %d to %d, want 8 to 39", e.presses[0].N, e.presses[31].N)
	}
	// Presses 30 to 39 are at most 5 s old and stay, plus the new one.
	e.Key(action.B, true, at(5*time.Second+30*time.Millisecond))
	if len(e.presses) != 11 || e.presses[0].N != 30 {
		t.Errorf("after 5 s: %d presses from N %d, want 11 from 30", len(e.presses), e.presses[0].N)
	}
	e.Payload(at(11 * time.Second))
	if len(e.presses) != 0 {
		t.Errorf("Payload kept %d stale presses", len(e.presses))
	}
}

func TestEngineKeyWithoutLED(t *testing.T) {
	e := twoProfiles(t)
	var k action.Key = 0x00 // no key
	if _, ok := LEDOfKey(k); ok {
		t.Skip("key 0 has an LED")
	}
	e.Key(k, true, at(time.Second))
	if len(e.presses) != 0 {
		t.Errorf("a key without LED added %d presses", len(e.presses))
	}
}

func TestEngineDownAfterLongGapIsNewPress(t *testing.T) {
	// A lost key-up leaves N marked down; the next real press must count.
	e := twoProfiles(t)
	e.Key(action.N, true, at(time.Second))
	e.Key(action.N, true, at(3*time.Second))
	if lit(t, e, action.N, at(3*time.Second)) {
		t.Error("a down 2 s after the last one did not flip gear back")
	}
	if len(e.presses) != 2 {
		t.Errorf("presses = %d, want 2", len(e.presses))
	}
}

func TestEngineHeldKeyRepeatsAreOnePress(t *testing.T) {
	e := twoProfiles(t)
	for d := time.Second; d <= 3*time.Second; d += 30 * time.Millisecond {
		e.Key(action.N, true, at(d))
	}
	if !lit(t, e, action.N, at(3*time.Second)) {
		t.Error("a key held for 2 s flipped gear more than once")
	}
	if len(e.presses) != 1 {
		t.Errorf("presses = %d, want 1", len(e.presses))
	}
}

func TestEngineKeyWithoutLEDTriggersToggle(t *testing.T) {
	if _, ok := LEDOfKey(action.F13); ok {
		t.Fatal("F13 has an LED; pick another key without one")
	}
	s := build(t, Config{Layers: []LayerConfig{
		staticLayer("#000000"),
		stateLayer("plain", "F13", "off", "N", "#00FF00"),
		stateLayer("ctrl", "LCtrl+F13", "off", "M", "#00FF00"),
	}})
	e := NewEngine(map[byte]*Stack{1: s}, engineStart)
	e.SetActive(1)

	e.Key(action.LCtrl, true, at(time.Second))
	e.Key(action.F13, true, at(time.Second))
	if lit(t, e, action.N, at(time.Second)) || !lit(t, e, action.M, at(time.Second)) {
		t.Error("LCtrl+F13 did not flip only the LCtrl+F13 toggle")
	}
	for _, p := range e.presses {
		if p.LED != ledOf(t, action.LCtrl) {
			t.Errorf("press on LED %d; F13 has no LED and must add none", p.LED)
		}
	}

	e.Key(action.F13, false, at(2*time.Second))
	e.Key(action.LCtrl, false, at(2*time.Second))
	e.Key(action.F13, true, at(2*time.Second))
	if !lit(t, e, action.N, at(2*time.Second)) || !lit(t, e, action.M, at(2*time.Second)) {
		t.Error("F13 alone did not flip only the F13 toggle")
	}
}
