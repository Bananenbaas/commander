// SPDX-License-Identifier: GPL-2.0-or-later

package audio

import (
	"errors"
	"fmt"
	"slices"
)

// endpoint is one output device whose sessions are followed.
type endpoint interface {
	// read follows the device's sessions and returns them in the order
	// Windows lists them.
	read() ([]rawSession, error)
	// setProgram sets the volume of every session of the process on the
	// device; found tells whether it has any.
	setProgram(pid uint32, level int) (found bool, err error)
	// close unregisters from the device and releases it.
	close()
}

// errDeviceLost is wrapped by the errors of a device that is gone, such as
// one unplugged or disabled while followed.
var errDeviceLost = errors.New("the output device is gone")

// deviceSet is the output devices followed, by device ID, the default one
// first and the others in the order Windows lists them. The zero value is an
// empty set.
type deviceSet struct {
	ids  []string
	devs map[string]endpoint
	lost bool // a device was dropped by setProgram; the set is to be updated
}

// update makes the set follow the devices ids: it closes those no longer
// listed and opens the new ones, and keeps those that stayed as they are, so
// their sessions are not lost. The default device, when listed, comes first:
// a program that plays on several devices shows the level it has there, and
// Windows does not promise a stable order of the devices. A device that
// cannot be opened is left out and its error returned; the others are
// followed all the same.
func (s *deviceSet) update(ids []string, defaultID string, open func(id string) (endpoint, error)) error {
	if i := slices.Index(ids, defaultID); i > 0 {
		ids = append([]string{defaultID}, slices.Delete(slices.Clone(ids), i, i+1)...)
	}
	if s.devs == nil {
		s.devs = map[string]endpoint{}
	}
	for id, d := range s.devs {
		if !slices.Contains(ids, id) {
			d.close()
			delete(s.devs, id)
		}
	}
	var errs []error
	s.ids = s.ids[:0]
	for _, id := range ids {
		if _, ok := s.devs[id]; !ok {
			d, err := open(id)
			if err != nil {
				errs = append(errs, fmt.Errorf("output device %s: %w", id, err))
				continue
			}
			s.devs[id] = d
		}
		if !slices.Contains(s.ids, id) {
			s.ids = append(s.ids, id)
		}
	}
	return errors.Join(errs...)
}

// read returns the sessions of every device, device by device. A device that
// cannot be read, such as one just unplugged, is closed and left out, and
// lost is set so that the set is updated again.
func (s *deviceSet) read(logf func(string, ...any)) (raw []rawSession, lost bool) {
	for _, id := range slices.Clone(s.ids) {
		r, err := s.devs[id].read()
		if err != nil {
			logf("audio: output device %s: %v", id, err)
			s.remove(id)
			lost = true
			continue
		}
		raw = append(raw, r...)
	}
	return raw, lost
}

// setProgram sets the volume of every session of the process on every
// device; found tells whether any device has one. The first error is
// returned, after the other devices were set. A device that is gone is
// closed and left out, and lost is set.
func (s *deviceSet) setProgram(pid uint32, level int) (found bool, err error) {
	for _, id := range slices.Clone(s.ids) {
		f, e := s.devs[id].setProgram(pid, level)
		found = found || f
		if err == nil {
			err = e
		}
		if errors.Is(e, errDeviceLost) {
			s.remove(id)
			s.lost = true
		}
	}
	return found, err
}

// len is the number of devices followed.
func (s *deviceSet) len() int { return len(s.ids) }

// closeAll closes every device and empties the set.
func (s *deviceSet) closeAll() {
	for _, id := range slices.Clone(s.ids) {
		s.remove(id)
	}
}

// remove closes one device and leaves it out.
func (s *deviceSet) remove(id string) {
	if d, ok := s.devs[id]; ok {
		d.close()
		delete(s.devs, id)
	}
	s.ids = slices.DeleteFunc(s.ids, func(x string) bool { return x == id })
}
