// SPDX-License-Identifier: GPL-2.0-or-later

package audio

import (
	"errors"
	"reflect"
	"testing"
)

// fakeDevice stands in for one output device of Windows.
type fakeDevice struct {
	raw    []rawSession
	levels map[uint32]int // set by setProgram
	fail   bool           // read fails, as for an unplugged device
	closed bool
}

func (d *fakeDevice) read() ([]rawSession, error) {
	if d.fail {
		return nil, errors.New("device gone")
	}
	return d.raw, nil
}

func (d *fakeDevice) setProgram(pid uint32, level int) (bool, error) {
	found := false
	for _, r := range d.raw {
		if r.PID == pid {
			found = true
			if d.levels == nil {
				d.levels = map[uint32]int{}
			}
			d.levels[pid] = level
		}
	}
	return found, nil
}

func (d *fakeDevice) close() { d.closed = true }

// fakeDevices opens the fake devices by ID and counts the opens.
type fakeDevices struct {
	devs  map[string]*fakeDevice
	opens map[string]int
}

func newFakeDevices(devs map[string]*fakeDevice) *fakeDevices {
	return &fakeDevices{devs: devs, opens: map[string]int{}}
}

func (f *fakeDevices) open(id string) (endpoint, error) {
	d, ok := f.devs[id]
	if !ok {
		return nil, errors.New("no device " + id)
	}
	f.opens[id]++
	return d, nil
}

func TestDeviceSetMergesByPID(t *testing.T) {
	speakers := &fakeDevice{raw: []rawSession{
		{PID: 1, Exe: "Steam", Level: 42},
		{PID: 2, Exe: "Spotify", Level: 80, Active: true},
	}}
	headset := &fakeDevice{raw: []rawSession{
		{PID: 3, Exe: "Discord", Level: 70, Active: true},
		{PID: 1, Exe: "Steam", Level: 10, Active: true},
	}}
	f := newFakeDevices(map[string]*fakeDevice{"speakers": speakers, "headset": headset})
	var s deviceSet
	if err := s.update([]string{"speakers", "headset"}, f.open); err != nil {
		t.Fatal(err)
	}
	if s.len() != 2 {
		t.Errorf("len = %d, want 2", s.len())
	}
	raw, lost := s.read(t.Logf)
	if lost {
		t.Error("a device was lost")
	}
	want := []Session{
		// Steam has a session on both devices: one program, the level of
		// the first, active because one of them plays.
		{ID: "1", Name: "Steam", Level: 42, Active: true},
		{ID: "2", Name: "Spotify", Level: 80, Active: true},
		{ID: "3", Name: "Discord", Level: 70, Active: true},
	}
	if got := merge(raw); !reflect.DeepEqual(got, want) {
		t.Errorf("merge =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDeviceSetRemovedDevice(t *testing.T) {
	speakers := &fakeDevice{raw: []rawSession{{PID: 1, Exe: "Steam", Level: 42}}}
	headset := &fakeDevice{raw: []rawSession{{PID: 3, Exe: "Discord", Level: 70}}}
	f := newFakeDevices(map[string]*fakeDevice{"speakers": speakers, "headset": headset})
	var s deviceSet
	if err := s.update([]string{"speakers", "headset"}, f.open); err != nil {
		t.Fatal(err)
	}
	if err := s.update([]string{"speakers"}, f.open); err != nil {
		t.Fatal(err)
	}
	if !headset.closed || speakers.closed {
		t.Errorf("closed: headset %v, speakers %v; want only the headset", headset.closed, speakers.closed)
	}
	if f.opens["speakers"] != 1 {
		t.Errorf("the speakers were opened %d times, want once", f.opens["speakers"])
	}
	raw, _ := s.read(t.Logf)
	if want := speakers.raw; !reflect.DeepEqual(raw, want) {
		t.Errorf("read = %+v, want %+v", raw, want)
	}
	if s.len() != 1 {
		t.Errorf("len = %d, want 1", s.len())
	}
}

func TestDeviceSetAddedDevice(t *testing.T) {
	speakers := &fakeDevice{raw: []rawSession{{PID: 1, Exe: "Steam", Level: 42}}}
	headset := &fakeDevice{raw: []rawSession{{PID: 3, Exe: "Discord", Level: 70}}}
	f := newFakeDevices(map[string]*fakeDevice{"speakers": speakers, "headset": headset})
	var s deviceSet
	if err := s.update([]string{"speakers"}, f.open); err != nil {
		t.Fatal(err)
	}
	if err := s.update([]string{"speakers", "headset"}, f.open); err != nil {
		t.Fatal(err)
	}
	if f.opens["speakers"] != 1 || f.opens["headset"] != 1 || speakers.closed {
		t.Errorf("opens %v, speakers closed %v; want each opened once and none closed", f.opens, speakers.closed)
	}
	raw, _ := s.read(t.Logf)
	want := append(append([]rawSession(nil), speakers.raw...), headset.raw...)
	if !reflect.DeepEqual(raw, want) {
		t.Errorf("read = %+v, want %+v", raw, want)
	}
}

func TestDeviceSetOpenFails(t *testing.T) {
	speakers := &fakeDevice{raw: []rawSession{{PID: 1, Exe: "Steam", Level: 42}}}
	f := newFakeDevices(map[string]*fakeDevice{"speakers": speakers})
	var s deviceSet
	if err := s.update([]string{"broken", "speakers"}, f.open); err == nil {
		t.Error("no error for a device that cannot be opened")
	}
	if s.len() != 1 {
		t.Errorf("len = %d, want 1", s.len())
	}
}

func TestDeviceSetLostDevice(t *testing.T) {
	speakers := &fakeDevice{raw: []rawSession{{PID: 1, Exe: "Steam", Level: 42}}}
	headset := &fakeDevice{raw: []rawSession{{PID: 3, Exe: "Discord", Level: 70}}, fail: true}
	f := newFakeDevices(map[string]*fakeDevice{"speakers": speakers, "headset": headset})
	var s deviceSet
	if err := s.update([]string{"speakers", "headset"}, f.open); err != nil {
		t.Fatal(err)
	}
	raw, lost := s.read(t.Logf)
	if !lost || !headset.closed || s.len() != 1 {
		t.Errorf("lost %v, headset closed %v, len %d; want the headset dropped", lost, headset.closed, s.len())
	}
	if !reflect.DeepEqual(raw, speakers.raw) {
		t.Errorf("read = %+v, want %+v", raw, speakers.raw)
	}
	// The headset comes back: it is opened again.
	headset.fail, headset.closed = false, false
	if err := s.update([]string{"speakers", "headset"}, f.open); err != nil {
		t.Fatal(err)
	}
	if f.opens["headset"] != 2 || s.len() != 2 {
		t.Errorf("headset opened %d times, len %d; want 2, 2", f.opens["headset"], s.len())
	}
}

func TestDeviceSetSetProgram(t *testing.T) {
	speakers := &fakeDevice{raw: []rawSession{{PID: 1, Exe: "Steam", Level: 42}, {PID: 2, Exe: "Spotify"}}}
	headset := &fakeDevice{raw: []rawSession{{PID: 1, Exe: "Steam", Level: 10}}}
	f := newFakeDevices(map[string]*fakeDevice{"speakers": speakers, "headset": headset})
	var s deviceSet
	if err := s.update([]string{"speakers", "headset"}, f.open); err != nil {
		t.Fatal(err)
	}
	found, err := s.setProgram(1, 30)
	if !found || err != nil {
		t.Fatalf("setProgram = %v, %v", found, err)
	}
	if speakers.levels[1] != 30 || headset.levels[1] != 30 {
		t.Errorf("levels: speakers %v, headset %v; want 30 on both", speakers.levels, headset.levels)
	}
	if found, _ := s.setProgram(9, 30); found {
		t.Error("found a program without sessions")
	}
	s.closeAll()
	if !speakers.closed || !headset.closed || s.len() != 0 {
		t.Error("closeAll left a device open")
	}
}
