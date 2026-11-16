package proto

import (
	"fmt"
	"strings"
	"testing"
)

// eq compares got against a space-separated lowercase hex string.
func eq(t *testing.T, got []byte, want string) {
	t.Helper()
	parts := make([]string, len(got))
	for i, b := range got {
		parts[i] = fmt.Sprintf("%02x", b)
	}
	if have := strings.Join(parts, " "); have != want {
		t.Errorf("got %q, want %q", have, want)
	}
}

func TestFrames(t *testing.T) {
	eq(t, Notifications(true), "0e 05 01 00 00")
	eq(t, Notifications(false), "0e 05 00 00 00")
	eq(t, SwitchProfile(1), "06 04 01 05")
	eq(t, Select(1, SelButtons), "05 01 b9 00")
	eq(t, WidgetType(3, 0x00, 0x00), "35 0c 01 03 01 00 00 00 00 00 46 00")
	eq(t, WidgetType(3, 0x03, 0x0b), "35 0c 01 03 01 00 03 00 0b 00 54 00")
	eq(t, WidgetType(6, 0x05, 0x06), "35 0c 01 06 01 00 05 00 06 00 54 00")
	eq(t, WidgetType(2, 0x39, 0x00), "35 0c 01 02 01 00 39 00 00 00 7e 00")
}
