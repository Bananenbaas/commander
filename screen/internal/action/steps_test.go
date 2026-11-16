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
