package proto

import "testing"

func TestParseReport(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want any
		ok   bool
	}{
		{"touch down", []byte{0x33, 1, 7, 5, 0, 0, 1, 1}, Touch{7, 5, true}, true},
		{"touch up", []byte{0x33, 1, 7, 5, 0, 0, 1, 0}, Touch{7, 5, false}, true},
		{"touch 12 bytes", []byte{0x33, 1, 2, 3, 9, 9, 1, 1, 0, 0, 0, 0}, Touch{2, 3, true}, true},
		{"touch short", []byte{0x33, 1, 7, 5, 0, 0, 1}, nil, false},
		{"profile", []byte{3, 0, 1, 1, 0, 0, 0, 0}, ProfileChanged{1}, true},
		{"redraw widget", []byte{3, 0, 0x31, 1, 6, 0, 0, 0}, Redraw{1, 6}, true},
		{"redraw main", []byte{3, 0, 0x31, 0x20, 0x95, 0x39, 0, 0}, Redraw{0x20, 0}, true},
		{"redraw settings", []byte{3, 0, 0x31, 4, 0x77}, Redraw{4, 0}, true},
		{"redraw widget short", []byte{3, 0, 0x31, 1}, nil, false},
		{"app tap down", []byte{3, 0, 0xf1, 1, 0x63, 0x12, 1, 0}, AppTap{0x1263, true}, true},
		{"app tap up", []byte{3, 0, 0xf1, 1, 0x63, 0x12, 0, 0}, AppTap{0x1263, false}, true},
		{"f1 other sub", []byte{3, 0, 0xf1, 8, 0x57, 0x11, 1, 0}, nil, false},
		{"f1 short", []byte{3, 0, 0xf1, 1, 0x63, 0x12}, nil, false},
		{"cc", []byte{3, 0, 0xcc, 1, 0, 0, 0, 0}, nil, false},
		{"fc", []byte{3, 0, 0xfc, 1, 0, 0, 0, 0}, nil, false},
		{"consumer", []byte{1, 0x40, 0, 0}, nil, false},
		{"empty", nil, nil, false},
		{"one byte", []byte{3}, nil, false},
		{"two bytes", []byte{3, 0}, nil, false},
	}
	for _, c := range cases {
		got, ok := ParseReport(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

// TestAppKinds ties the exported constants to recorded frames: the touch of
// an application button at position 3 (PROTOCOL.md, "Application buttons")
// and the 0x29 entry apply writes.
func TestAppKinds(t *testing.T) {
	ev, ok := ParseReport([]byte{0x33, 0x01, 0x03, 0x05, 0x00, 0x00, 0x01, 0x01})
	if tc, _ := ev.(Touch); !ok || tc.Kind != TypeApp {
		t.Errorf("touch %+v, want kind %#x", ev, TypeApp)
	}
	if AppType.Type != TypeApp {
		t.Errorf("AppType %+v, want type %#x", AppType, TypeApp)
	}
	if e := AppEntry(3, ""); e.Kind != KindApp || KindApp != 0x0b {
		t.Errorf("AppEntry kind %#x, KindApp %#x, want 0x0b", e.Kind, KindApp)
	}
}
