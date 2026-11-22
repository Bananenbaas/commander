// SPDX-License-Identifier: GPL-2.0-or-later

package action

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseStep(t *testing.T) {
	cases := []struct {
		in   string
		want Step
	}{
		{"LAlt+N", Chord{[]Key{LAlt, N}}},
		{" lalt + n ", Chord{[]Key{LAlt, N}}},
		{"AltGr+Y", Chord{[]Key{LCtrl, RAlt, Y}}},
		{"Period", Chord{[]Key{Period}}},
		{"0", Chord{[]Key{D0}}},
		{"VolumeUp", Chord{[]Key{VolumeUp}}},
		{"wait 600ms", Wait{600 * time.Millisecond}},
		{`run "C:\Tools\x.exe" -a`, Run{`C:\Tools\x.exe`, []string{"-a"}}},
		{`run notepad "a b" c`, Run{"notepad", []string{"a b", "c"}}},
		{"open https://example.org", Open{"https://example.org"}},
	}
	for _, c := range cases {
		got, err := ParseStep(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: got %#v want %#v", c.in, got, c.want)
		}
	}
}

func TestParseStepErrors(t *testing.T) {
	cases := map[string]string{
		"Foo+N":        "Foo",
		"":             "empty",
		"A+A":          "repeats",
		"AltGr+RAlt":   "repeats",
		"wait 0ms":     "wait",
		"wait 60001ms": "wait",
		"wait 5":       "wait",
		"run":          "program",
		`run "x`:       "quote",
		"open":         "target",
	}
	for in, sub := range cases {
		_, err := ParseStep(in)
		if err == nil || !strings.Contains(err.Error(), sub) {
			t.Errorf("%q: err = %v, want containing %q", in, err, sub)
		}
	}
}

func TestKeyValues(t *testing.T) {
	if A != 0x04 || Z != 0x1d || D1 != 0x1e || D0 != 0x27 || F12 != 0x45 || F13 != 0x68 || F24 != 0x73 {
		t.Fatal("key values wrong")
	}
	if VolumeUp != 0x0CE9 || StopMedia != 0x0CB7 {
		t.Fatal("media values wrong")
	}
}

func TestFormatStepRoundTrip(t *testing.T) {
	inputs := []string{
		"LCtrl+LShift+A",
		"AltGr+Q",
		"VolumeUp",
		"wait 600ms",
		"run notepad.exe",
		`run "C:\Program Files\x\x.exe" --a "b c"`,
		"open https://example.com/a b",
	}
	for _, in := range inputs {
		first, err := ParseStep(in)
		if err != nil {
			t.Fatalf("%q: parse: %v", in, err)
		}
		text, err := FormatStep(first)
		if err != nil {
			t.Fatalf("%q: format: %v", in, err)
		}
		second, err := ParseStep(text)
		if err != nil {
			t.Fatalf("%q: formatted as %q, which does not parse: %v", in, text, err)
		}
		if !reflect.DeepEqual(first, second) {
			t.Errorf("%q: formatted as %q, parsed back as %#v, want %#v", in, text, second, first)
		}
	}
}

func TestFormatStepRefuses(t *testing.T) {
	cases := map[string]Step{
		"program with quote":  Run{Program: `a"b`},
		"argument with quote": Run{Program: "notepad", Args: []string{`x"y`}},
		"empty open target":   Open{Target: ""},
		"open with space":     Open{Target: " https://example.com"},
		"empty chord":         Chord{},
		"unknown key":         Chord{Keys: []Key{Key(0x01)}},
		"zero wait":           Wait{D: 0},
		"wait over maximum":   Wait{D: 60001 * time.Millisecond},
	}
	for name, s := range cases {
		if text, err := FormatStep(s); err == nil {
			t.Errorf("%s: got %q, want an error", name, text)
		}
	}
}
