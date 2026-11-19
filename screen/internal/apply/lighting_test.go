// SPDX-License-Identifier: GPL-2.0-or-later

package apply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/Bananenbaas/kb7-commander/screen/internal/layout"
	"github.com/Bananenbaas/kb7-commander/screen/internal/ledger"
	"github.com/Bananenbaas/kb7-commander/screen/internal/light"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

// stored11 is the 0x11 table the fake holds before anything is applied:
// every LED red, brightness 99, the unknown header bytes as Swarm II writes
// them.
func stored11(t *testing.T, p byte) *proto.Table11 {
	t.Helper()
	b := make([]byte, proto.Len11)
	copy(b, []byte{0x11, 0x5c, 0x01, p, 0x01, 0x06, 0x63, 0x0b, 0x01, 0x00})
	for led := range 112 {
		b[10+led/16*48+led%16] = 0xff
	}
	t11, err := proto.ParseTable11(seal(b))
	if err != nil {
		t.Fatal(err)
	}
	return t11
}

// ReadLightsTable gives a copy of the profile's 0x11 table; a table is read
// for any profile without switching to it, unless activeOnly is set: then
// the active profile's table comes back and the profile check of the real
// keyboard refuses it.
func (f *fakeKB) ReadLightsTable(p byte) (*proto.Table11, error) {
	f.record("read11 %d", p)
	if f.activeOnly && f.active != int(p) {
		return nil, fmt.Errorf("fake: lighting report is for profile %d, asked for %d", f.active, p)
	}
	if f.b11[p] == nil {
		f.b11[p] = stored11(f.t, p)
	}
	return proto.ParseTable11(f.b11[p].Bytes())
}

// WriteLightsTable stores the table for the profile it names, which must be
// active, and answers with lightStatus, as the keyboard's status register.
func (f *fakeKB) WriteLightsTable(t *proto.Table11) error {
	f.record("write11 %d", t.Profile())
	if int(t.Profile()) != f.active {
		return fmt.Errorf("fake: lighting table of profile %d written while %d is active", t.Profile(), f.active)
	}
	c, err := proto.ParseTable11(t.Bytes())
	if err != nil {
		return err
	}
	f.b11[t.Profile()] = c
	if f.lightStatus != nil && !bytes.Equal(f.lightStatus, []byte{0x05, 0x01, 0, 0}) {
		return fmt.Errorf("fake: status % x after the lighting table, want 05 01 00 00", f.lightStatus)
	}
	return nil
}

// greenStack is black with N green while its state "gear" is on, which it
// starts as, and a ripple that must not show in the still picture.
func greenStack(t *testing.T) *light.Stack {
	t.Helper()
	s, err := light.Build(light.Config{Layers: []light.LayerConfig{
		{Effect: "static", Settings: map[string]any{"color": "#000000"}},
		{Effect: "ripple", Settings: map[string]any{"color": "#FF0000"}},
		{Effect: "state", Keys: []string{"N"}, Settings: map[string]any{"name": "gear", "start": "on", "color": "#00FF00"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var white = color.NRGBA{R: 255, G: 255, B: 255, A: 255}

// lightingCfg has profile 1 with [light] and a button, profile 0 with only
// [stored_light] and profile 2 with a button and neither.
func lightingCfg(t *testing.T) *layout.Config {
	p1 := prof(1, "sc.toml", "", btn(3, "One"))
	p1.Light = greenStack(t)
	p0 := prof(0, "stored.toml", "")
	p0.StoredLight = &layout.StoredLight{Color: white, Brightness: 80}
	p2 := prof(2, "other.toml", "", btn(4, "Two"))
	return cfgOf(p1, p0, p2)
}

func lightCalls(f *fakeKB, from int) []string {
	var out []string
	for _, c := range f.calls[from:] {
		if strings.HasPrefix(c, "read11 ") || strings.HasPrefix(c, "write11 ") {
			out = append(out, c)
		}
	}
	return out
}

func TestApplyWritesStoredLighting(t *testing.T) {
	f := newFake(t)
	l := &ledger.Ledger{}
	cfg := lightingCfg(t)
	before1 := stored11(t, 1).Bytes()
	rep := mustApply(t, f, cfg, l, false)

	if !slices.Equal(rep.Lighting, []byte{0, 1}) {
		t.Errorf("Lighting = %v, want [0 1]", rep.Lighting)
	}
	got := lightCalls(f, 0)
	for _, c := range got {
		if strings.HasSuffix(c, " 2") || strings.HasSuffix(c, " 3") || strings.HasSuffix(c, " 4") {
			t.Errorf("%s: the profile has neither [light] nor [stored_light]", c)
		}
	}
	for _, want := range []string{"write11 0", "write11 1"} {
		if !slices.Contains(got, want) {
			t.Errorf("calls %v lack %s", got, want)
		}
	}

	// Profile 1: the still picture of its layers, its brightness kept.
	t1 := f.b11[1]
	if *t1.Colours() != *light.Still(cfg.Profiles[0].Light) {
		t.Error("profile 1 does not hold the still picture of its layers")
	}
	if t1.Brightness() != 99 {
		t.Errorf("profile 1 brightness %d, want the keyboard's 99", t1.Brightness())
	}
	after1 := t1.Bytes()
	if !bytes.Equal(after1[:10], before1[:10]) {
		t.Errorf("header % x, want % x", after1[:10], before1[:10])
	}

	// Profile 0: every LED white, brightness 80.
	t0 := f.b11[0]
	var want proto.LightPayload
	for led := range 112 {
		proto.SetLED(&want, led, 255, 255, 255)
	}
	if *t0.Colours() != want || t0.Brightness() != 80 {
		t.Errorf("profile 0 brightness %d, colours % x", t0.Brightness(), t0.Colours()[:48])
	}

	n := len(f.calls)
	rep = mustApply(t, f, cfg, l, false)
	if len(rep.Lighting) != 0 {
		t.Errorf("second apply: Lighting = %v", rep.Lighting)
	}
	for _, c := range lightCalls(f, n) {
		if strings.HasPrefix(c, "write11 ") {
			t.Errorf("second apply wrote: %s", c)
		}
	}
}

func TestApplyStoredLightOverridesLight(t *testing.T) {
	f := newFake(t)
	p := prof(1, "sc.toml", "", btn(3, "One"))
	p.Light = greenStack(t)
	p.StoredLight = &layout.StoredLight{Color: white, Brightness: -1}
	rep := mustApply(t, f, cfgOf(p), &ledger.Ledger{}, false)
	if !slices.Equal(rep.Lighting, []byte{1}) {
		t.Fatalf("Lighting = %v", rep.Lighting)
	}
	if c := f.b11[1].Colours(); c[0] != 255 || c[16] != 255 || c[32] != 255 || f.b11[1].Brightness() != 99 {
		t.Errorf("LED 0 % x brightness %d", []byte{c[0], c[16], c[32]}, f.b11[1].Brightness())
	}
}

func TestApplyStoredBrightnessOnly(t *testing.T) {
	f := newFake(t)
	p := prof(3, "stored.toml", "")
	p.StoredLight = &layout.StoredLight{Brightness: 20}
	mustApply(t, f, cfgOf(p), &ledger.Ledger{}, false)
	if got := f.b11[3]; got.Brightness() != 20 || *got.Colours() != *stored11(t, 3).Colours() {
		t.Errorf("brightness %d, colours changed: %v", got.Brightness(), *got.Colours() != *stored11(t, 3).Colours())
	}
}

func TestApplyNoLightingNoTable(t *testing.T) {
	f := newFake(t)
	rep := mustApply(t, f, cfgOf(prof(1, "sc.toml", "", btn(3, "One"))), &ledger.Ledger{}, false)
	if c := lightCalls(f, 0); len(c) != 0 || len(rep.Lighting) != 0 {
		t.Errorf("lighting calls %v, Lighting %v", c, rep.Lighting)
	}
}

func TestApplyLightingStatusError(t *testing.T) {
	f := newFake(t)
	f.lightStatus = []byte{0x05, 0x00, 0, 0}
	_, err := Apply(context.Background(), f, lightingCfg(t), &ledger.Ledger{}, false, nothingRuns, nil)
	if err == nil || !strings.Contains(err.Error(), "profile 0") || !strings.Contains(err.Error(), "05 01 00 00") {
		t.Fatalf("err = %v, want the status error naming profile 0", err)
	}
}

// When the keyboard gives a table only for the active profile, Apply
// switches to the profile and reads it again.
func TestApplyReadsStoredLightingAfterSwitch(t *testing.T) {
	f := newFake(t)
	f.activeOnly = true
	rep := mustApply(t, f, lightingCfg(t), &ledger.Ledger{}, false)
	if !slices.Equal(rep.Lighting, []byte{0, 1}) {
		t.Errorf("Lighting = %v, want [0 1]", rep.Lighting)
	}
	i := slices.Index(f.calls, "read11 0")
	if i < 0 || !slices.Contains(f.calls[i+1:], "read11 0") || !slices.Contains(f.calls[i+1:], "write11 0") {
		t.Errorf("calls %v: want profile 0 read again after the switch, then written", f.calls)
	}
}

// A failed switch for the stored lighting names the profile and the step.
func TestApplyStoredLightingSwitchError(t *testing.T) {
	f := newFake(t)
	switches := 0
	f.switchErr = func(p byte) error {
		if p != 0 {
			return nil
		}
		if switches++; switches > 1 { // the first one reads the buttons
			return errors.New("not confirmed")
		}
		return nil
	}
	_, err := Apply(context.Background(), f, lightingCfg(t), &ledger.Ledger{}, false, nothingRuns, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "profile 0: stored lighting: ") || !strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("err = %v, want profile 0: stored lighting: ...; calls %v", err, f.calls)
	}
}
