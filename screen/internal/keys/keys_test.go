// SPDX-License-Identifier: GPL-2.0-or-later

package keys

import (
	"testing"

	"github.com/Bananenbaas/kb7-commander/screen/internal/action"
)

func TestIsKB7(t *testing.T) {
	if !isKB7(`\?\HID#VID_10F5&PID_5038&MI_00#7&abc&0&0000#{884b96c3-56ef-11d1-bc8c-00a0c91405dd}`) {
		t.Error("KB7 not recognised")
	}
	if !isKB7(`\?\hid#vid_10f5&pid_5038&mi_02&col01#x`) {
		t.Error("case must not matter")
	}
	if isKB7(`\?\HID#VID_046D&PID_C31C&MI_00#x`) {
		t.Error("other keyboard accepted")
	}
}

func TestDecodeWindows(t *testing.T) {
	cases := []struct {
		make, flags uint16
		key         action.Key
		down, ok    bool
	}{
		{0x11, 0, action.W, true, true},
		{0x11, 1, action.W, false, true},    // RI_KEY_BREAK
		{0x38, 2, action.RAlt, true, true},  // RI_KEY_E0
		{0x38, 3, action.RAlt, false, true}, // E0 and break
		{0x1D, 4, 0, false, false},          // RI_KEY_E1 (Pause prefix)
		{0x00, 0, 0, false, false},          // unknown code
	}
	for _, c := range cases {
		k, down, ok := decodeWindows(c.make, c.flags)
		if ok != c.ok || (ok && (k != c.key || down != c.down)) {
			t.Errorf("decodeWindows(%#x, %d) = %v, %v, %v; want %v, %v, %v",
				c.make, c.flags, k, down, ok, c.key, c.down, c.ok)
		}
	}
}
