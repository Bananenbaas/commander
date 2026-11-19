// SPDX-License-Identifier: GPL-2.0-or-later

//go:build amd64

package audio

import (
	"errors"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestGUIDs(t *testing.T) {
	for _, c := range []struct {
		name string
		got  *windows.GUID
		want string
	}{
		{"CLSID_MMDeviceEnumerator", &clsidMMDeviceEnumerator, "{BCDE0395-E52F-467C-8E3D-C4579291692E}"},
		{"IUnknown", &iidIUnknown, "{00000000-0000-0000-C000-000000000046}"},
		{"IAgileObject", &iidIAgileObject, "{94EA2B94-E9CC-49E0-C0FF-EE64CA8F5B90}"},
		{"IMMDeviceEnumerator", &iidIMMDeviceEnumerator, "{A95664D2-9614-4F35-A746-DE8DB63617E6}"},
		{"IMMNotificationClient", &iidIMMNotificationClient, "{7991EEC9-7E89-4D85-8390-6C703CEC60C0}"},
		{"IAudioEndpointVolume", &iidIAudioEndpointVolume, "{5CDF2C82-841E-4546-9722-0CF74078229A}"},
		{"IAudioEndpointVolumeCallback", &iidIAudioEndpointVolumeCallback, "{657804FA-D6AD-4496-8A60-352752AF4F89}"},
		{"IAudioSessionManager2", &iidIAudioSessionManager2, "{77AA99A0-1BD6-484F-8BC7-2C654C9A9B6F}"},
		{"IAudioSessionNotification", &iidIAudioSessionNotification, "{641DD20B-4D41-49CC-ABA3-174B9477BB08}"},
		{"IAudioSessionControl2", &iidIAudioSessionControl2, "{BFB7FF88-7239-4FC9-8FA2-07C950BE9C6D}"},
		{"IAudioSessionEvents", &iidIAudioSessionEvents, "{24918ACC-64B3-37C1-8CA9-74A66E9957A8}"},
		{"ISimpleAudioVolume", &iidISimpleAudioVolume, "{87CE5498-68D6-44E5-9215-6DA47EF883D8}"},
	} {
		want, err := windows.GUIDFromString(c.want)
		if err != nil {
			t.Fatal(err)
		}
		if *c.got != want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestVtables(t *testing.T) {
	vtablesOnce.Do(makeVtables)
	for kind, n := range [callbackKinds]int{
		kindNotificationClient:  notificationClientSlots,
		kindVolumeCallback:      volumeCallbackSlots,
		kindSessionNotification: sessionNotificationSlots,
		kindSessionEvents:       sessionEventsSlots,
	} {
		if len(vtables[kind]) != n {
			t.Errorf("vtable %d has %d slots, want %d", kind, len(vtables[kind]), n)
		}
		for i, fn := range vtables[kind] {
			if fn == 0 {
				t.Errorf("vtable %d slot %d is empty", kind, i)
			}
		}
	}
	// COM finds the vtable through the object's first word.
	if off := unsafe.Offsetof(callback{}.vtbl); off != 0 {
		t.Errorf("the vtable pointer is at offset %d", off)
	}
}

func TestCallbackRefsAndQuery(t *testing.T) {
	signals := 0
	h := &hub{signal: func() { signals++ }}
	c := newCallback(kindSessionEvents, h)
	var out uintptr
	if hr := cbQueryInterface(c, &iidIAudioSessionEvents, &out); hr != uintptr(sOK) || out != c.ptr() {
		t.Errorf("QueryInterface(own interface) = %#x, %#x", hr, out)
	}
	if hr := cbQueryInterface(c, &iidIUnknown, &out); hr != uintptr(sOK) || out != c.ptr() {
		t.Errorf("QueryInterface(IUnknown) = %#x, %#x", hr, out)
	}
	if hr := cbQueryInterface(c, &iidIMMDeviceEnumerator, &out); hr != uintptr(eNoInterface) || out != 0 {
		t.Errorf("QueryInterface(other) = %#x, %#x", hr, out)
	}
	if n := c.refs.Load(); n != 3 {
		t.Errorf("refs = %d, want 3", n)
	}
	cbRelease(c)
	cbRelease(c)
	if _, ok := live.Load(c); !ok {
		t.Error("released while referenced")
	}
	c.release()
	if _, ok := live.Load(c); ok {
		t.Error("still kept after the last release")
	}

	d := newCallback(kindNotificationClient, h)
	defer d.release()
	cbDefaultDeviceChanged(d, 1, eConsole, 0) // capture flow: ignored
	if h.deviceChanged.Load() || signals != 0 {
		t.Error("a capture device change was noted")
	}
	cbDefaultDeviceChanged(d, 0xffffffff00000000|eRender, eConsole, 0)
	if !h.deviceChanged.Load() || signals != 1 {
		t.Error("a render device change was not noted")
	}
}

// TestLive reads the real audio once; it changes no volume.
func TestLive(t *testing.T) {
	w, err := New(t.Logf)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sys, progs, err := w.Sessions()
	if cerr := w.Close(); cerr != nil {
		t.Errorf("Close: %v", cerr)
	}
	if errors.Is(err, ErrNoDevice) {
		t.Skip("no default output device")
	}
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if sys.ID != "system" || sys.Level < 0 || sys.Level > 100 {
		t.Errorf("system session %+v", sys)
	}
	for _, p := range progs {
		if p.ID == "" || p.Name == "" || p.Level < 0 || p.Level > 100 {
			t.Errorf("program %+v", p)
		}
	}
	t.Logf("system %d, %d programs", sys.Level, len(progs))
	if _, _, err := w.Sessions(); !errors.Is(err, ErrClosed) {
		t.Errorf("Sessions after Close: %v", err)
	}
}
