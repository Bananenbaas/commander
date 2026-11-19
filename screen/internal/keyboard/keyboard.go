// SPDX-License-Identifier: GPL-2.0-or-later

// Package keyboard performs the operations kb7screen needs on the KB7 and
// keeps to the rules the protocol sets: one frame at a time with its
// acknowledgement, a pause after a button table is written, and no retry of a
// frame whose effect is unknown.
package keyboard

import (
	"errors"
	"fmt"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/device"
	"github.com/Bananenbaas/kb7-commander/screen/internal/proto"
)

var (
	// ErrOpenerRefused means the keyboard answered the opening frame of a
	// picture upload with something else than its echo. Nothing else was
	// sent and the keyboard is still in its normal state.
	ErrOpenerRefused = errors.New("keyboard: the keyboard did not accept the upload; nothing else was sent")

	// ErrUploadLeftOpen means a picture upload stopped half way, or without
	// knowing whether it started, so the keyboard may be waiting for the rest.
	ErrUploadLeftOpen = errors.New("keyboard: the picture upload was interrupted and the keyboard may be left in upload mode; unplug the keyboard and plug it back in")

	// ErrStreamStalled means the keyboard did not take a lighting frame: the
	// write itself timed out, after up to 5 s. It does not match
	// device.ErrTimeout, which StreamLights keeps for a missed
	// acknowledgement.
	ErrStreamStalled = errors.New("keyboard: the keyboard did not take the lighting frame")
)

const (
	ackTimeout     = 5 * time.Second        // wait for the acknowledgement of one upload frame
	drainTimeout   = time.Millisecond       // wait when emptying stale frames or reports
	maxDrain       = 64                     // most stale frames or reports dropped
	selectDelay    = 150 * time.Millisecond // between a Select frame and reading the table
	writeGap       = 200 * time.Millisecond // after a 0x29 write before the next report
	profileTimeout = 3 * time.Second
	redrawTimeout  = 5 * time.Second
	redrawAll      = 0x20 // Redraw view of the whole screen
	profiles       = 5

	namesID = 0x16
)

// KB is an open keyboard. It is not safe for concurrent use.
type KB struct {
	d     device.Device
	now   func() time.Time
	sleep func(time.Duration)

	wroteButtons bool
	lastButtons  time.Time
	uploadOpen   bool // an upload was left open; no further upload is tried
}

// New returns a KB that uses d and the real clock.
func New(d device.Device) *KB { return newKB(d, time.Now, time.Sleep) }

func newKB(d device.Device, now func() time.Time, sleep func(time.Duration)) *KB {
	return &KB{d: d, now: now, sleep: sleep}
}

// Close closes the device.
func (k *KB) Close() error { return k.d.Close() }

// NextEvent returns the next input report that proto.ParseReport understands,
// skipping the others. It returns device.ErrTimeout if none arrives within
// timeout; other device errors are returned as they are.
func (k *KB) NextEvent(timeout time.Duration) (any, error) {
	deadline := k.now().Add(timeout)
	for {
		remaining := deadline.Sub(k.now())
		if remaining <= 0 {
			return nil, device.ErrTimeout
		}
		r, err := k.d.ReadInput(remaining)
		if err != nil {
			return nil, err
		}
		if ev, ok := proto.ParseReport(r); ok {
			return ev, nil
		}
	}
}

// drainInput drops reports that arrived earlier, so that they are not taken
// for the answer to what is sent next.
func (k *KB) drainInput() error {
	for i := 0; i < maxDrain; i++ {
		if _, err := k.d.ReadInput(drainTimeout); err != nil {
			if errors.Is(err, device.ErrTimeout) {
				return nil
			}
			return fmt.Errorf("keyboard: dropping old input: %w", err)
		}
	}
	return errors.New("keyboard: input not quiet, nothing was sent")
}

// drainStream drops stale stream frames.
func (k *KB) drainStream() error {
	for i := 0; i < maxDrain; i++ {
		if _, err := k.d.ReadStream(drainTimeout); err != nil {
			if errors.Is(err, device.ErrTimeout) {
				return nil
			}
			return fmt.Errorf("keyboard: dropping old frames: %w", err)
		}
	}
	return errors.New("keyboard: stream not quiet, upload not started")
}

// readAck waits up to ackTimeout for the next frame that is an a5 frame or
// 64 bytes of ff. Anything else is ignored; the deadline does not move.
func (k *KB) readAck() ([64]byte, error) {
	deadline := k.now().Add(ackTimeout)
	for {
		remaining := deadline.Sub(k.now())
		if remaining <= 0 {
			return [64]byte{}, device.ErrTimeout
		}
		fr, err := k.d.ReadStream(remaining)
		if err != nil {
			return fr, err
		}
		if isAck(fr) {
			return fr, nil
		}
	}
}

func isAck(fr [64]byte) bool {
	if fr[0] == 0xa5 {
		return true
	}
	for _, b := range fr {
		if b != 0xff {
			return false
		}
	}
	return true
}

// Upload sends a picture (proto.PictureSize bytes) to slot. Each frame is
// sent only after the previous one was acknowledged, and none is repeated.
//
// If the keyboard refuses the opening frame, ErrOpenerRefused is returned and
// nothing else was sent. Once the opening frame may have been accepted, any
// failure returns ErrUploadLeftOpen, wrapping the cause where there is one.
func (k *KB) Upload(slot byte, data []byte) error {
	if k.uploadOpen {
		return fmt.Errorf("%w (an earlier upload was not completed; nothing was sent)", ErrUploadLeftOpen)
	}
	frames, err := proto.UploadFrames(slot, data)
	if err != nil {
		return fmt.Errorf("keyboard: upload: %w", err)
	}
	return k.send(frames)
}

// UploadBackground sends a screen background (proto.BackgroundSize bytes)
// and its thumbnail (proto.PictureSize bytes), the way Swarm II does. The
// keyboard stores it; it shows once it is chosen on the keyboard's screen.
// It takes about half a minute and fails like Upload.
func (k *KB) UploadBackground(full, thumb []byte) error {
	if k.uploadOpen {
		return fmt.Errorf("%w (an earlier upload was not completed; nothing was sent)", ErrUploadLeftOpen)
	}
	frames, err := proto.BackgroundFrames(full, thumb)
	if err != nil {
		return fmt.Errorf("keyboard: background: %w", err)
	}
	return k.send(frames)
}

// send writes upload frames one by one, each after the previous one was
// acknowledged.
func (k *KB) send(frames []proto.UploadFrame) error {
	if err := k.drainStream(); err != nil {
		return err
	}
	for i, f := range frames {
		if err := k.d.WriteStream(f.Out); err != nil {
			if i == 0 && errors.Is(err, device.ErrDisconnected) {
				return fmt.Errorf("keyboard: starting the upload: %w", err)
			}
			return k.leftOpen(i, len(frames), err)
		}
		ack, err := k.readAck()
		switch {
		case err != nil:
			return k.leftOpen(i, len(frames), err)
		case ack != f.Ack && i == 0:
			return fmt.Errorf("%w (reply % x)", ErrOpenerRefused, ack[:8])
		case ack != f.Ack:
			return k.leftOpen(i, len(frames), fmt.Errorf("unexpected acknowledgement % x", ack[:8]))
		}
	}
	return nil
}

func (k *KB) leftOpen(i, n int, cause error) error {
	k.uploadOpen = true
	return fmt.Errorf("%w (frame %d of %d: %w)", ErrUploadLeftOpen, i+1, n, cause)
}

// selectTable asks for a table of a profile and checks the keyboard is ready.
func (k *KB) selectTable(profile, selector byte) error {
	k.settle()
	if err := k.d.SetFeature(proto.Select(profile, selector)); err != nil {
		return fmt.Errorf("keyboard: selecting table: %w", err)
	}
	k.sleep(selectDelay)
	r, err := k.d.GetFeature(0x05, 4)
	if err != nil {
		return fmt.Errorf("keyboard: reading ready reply: %w", err)
	}
	if len(r) < 2 || r[0] != 0x05 || r[1] != 0x01 {
		n := min(len(r), 4)
		return fmt.Errorf("keyboard: ready reply % x, want 05 01", r[:n])
	}
	return nil
}

// ReadButtons reads the button table (report 0x29) of profile p.
func (k *KB) ReadButtons(p byte) (*proto.Table29, error) {
	if err := k.selectTable(p, proto.SelButtons); err != nil {
		return nil, err
	}
	b, err := k.d.GetFeature(0x29, 584)
	if err != nil {
		return nil, fmt.Errorf("keyboard: reading button table: %w", err)
	}
	t, err := proto.ParseTable29(b)
	if err != nil {
		return nil, fmt.Errorf("keyboard: %w", err)
	}
	if t.Profile() != p {
		return nil, fmt.Errorf("keyboard: button table is for profile %d, asked for %d", t.Profile(), p)
	}
	return t, nil
}

// ReadTypes reads the widget type table (report 0x25) of profile p.
func (k *KB) ReadTypes(p byte) (*proto.Table25, error) {
	if err := k.selectTable(p, proto.SelTypes); err != nil {
		return nil, err
	}
	b, err := k.d.GetFeature(0x25, 164)
	if err != nil {
		return nil, fmt.Errorf("keyboard: reading type table: %w", err)
	}
	t, err := proto.ParseTable25(b)
	if err != nil {
		return nil, fmt.Errorf("keyboard: %w", err)
	}
	if t.Profile() != p {
		return nil, fmt.Errorf("keyboard: type table is for profile %d, asked for %d", t.Profile(), p)
	}
	return t, nil
}

// ProfileNames reads the names of the five profiles.
func (k *KB) ProfileNames() ([5]string, error) {
	var names [profiles]string
	t, err := k.ReadNamesTable()
	if err != nil {
		return names, err
	}
	for i := range names {
		names[i] = t.Name(i)
	}
	return names, nil
}

// ReadNamesTable reads report 0x16 whole, for writing back with
// WriteNamesTable. It holds all five names, whichever profile is asked for.
func (k *KB) ReadNamesTable() (*proto.Table16, error) {
	if err := k.selectTable(0, proto.SelNames); err != nil {
		return nil, err
	}
	b, err := k.d.GetFeature(namesID, proto.Len16)
	if err != nil {
		return nil, fmt.Errorf("keyboard: reading profile names: %w", err)
	}
	t, err := proto.ParseTable16(b)
	if err != nil {
		return nil, fmt.Errorf("keyboard: %w", err)
	}
	return t, nil
}

// WriteNamesTable writes report 0x16, the names of all five profiles, and
// then reads the status register as Swarm II does, which must read
// 05 01 00 00.
func (k *KB) WriteNamesTable(t *proto.Table16) error {
	k.settle()
	if err := k.d.SetFeature(t.Bytes()); err != nil {
		return fmt.Errorf("keyboard: writing profile names: %w", err)
	}
	return k.checkStatus("profile names")
}

// SwitchProfile makes p the active profile and waits up to 3 s for the
// keyboard to say so. Input that is already queued is dropped first, and
// other events that arrive while waiting are discarded.
func (k *KB) SwitchProfile(p byte) error {
	k.settle()
	if err := k.drainInput(); err != nil {
		return err
	}
	if err := k.d.SetFeature(proto.SwitchProfile(p)); err != nil {
		return fmt.Errorf("keyboard: switching profile: %w", err)
	}
	return k.waitFor(profileTimeout, "profile change", func(ev any) bool {
		c, ok := ev.(proto.ProfileChanged)
		return ok && c.Profile == p
	})
}

// SwitchProfileFresh makes p the active profile even when it already is: the
// keyboard does not confirm a switch to the active profile, so it first
// switches to the next profile and waits for that, then to p and waits for
// that. Use it when the active profile is not known. When the first switch is
// not confirmed the keyboard was most likely already on that profile, so the
// switch to p is sent anyway; any other error from the first switch is
// returned.
func (k *KB) SwitchProfileFresh(p byte) error {
	if p >= profiles {
		return fmt.Errorf("keyboard: profile %d: profiles are 0 to %d", p, profiles-1)
	}
	if err := k.SwitchProfile((p + 1) % profiles); err != nil && !errors.Is(err, device.ErrTimeout) {
		return err
	}
	return k.SwitchProfile(p)
}

// EnableNotifications switches touch notifications on and waits up to 5 s for
// the keyboard's redraw request. Other events that arrive meanwhile are
// discarded.
func (k *KB) EnableNotifications() error {
	k.settle()
	if err := k.d.SetFeature(proto.Notifications(true)); err != nil {
		return fmt.Errorf("keyboard: enabling notifications: %w", err)
	}
	return k.WaitRedraw(redrawTimeout)
}

// WaitRedraw reads events until the keyboard asks for a redraw of the whole
// screen, or timeout passes. The keyboard asks for it once notifications are
// enabled and once it has taken over a written type table. Other events that
// arrive meanwhile are discarded.
func (k *KB) WaitRedraw(timeout time.Duration) error {
	return k.waitFor(timeout, "redraw request", func(ev any) bool {
		r, ok := ev.(proto.Redraw)
		return ok && r.View == redrawAll
	})
}

// waitFor reads events until match accepts one or timeout passes.
func (k *KB) waitFor(timeout time.Duration, what string, match func(any) bool) error {
	deadline := k.now().Add(timeout)
	for {
		remaining := deadline.Sub(k.now())
		ev, err := k.NextEvent(remaining)
		if err != nil {
			return fmt.Errorf("keyboard: waiting for the %s: %w", what, err)
		}
		if match(ev) {
			return nil
		}
	}
}

// WriteButtons writes a button table. The keyboard needs 200 ms before the
// next report, which every later report waits for.
func (k *KB) WriteButtons(t *proto.Table29) error {
	k.settle()
	if err := k.d.SetFeature(t.Bytes()); err != nil {
		return fmt.Errorf("keyboard: writing button table: %w", err)
	}
	k.wroteButtons = true
	k.lastButtons = k.now()
	return nil
}

// settle sleeps out the rest of the 200 ms after the last button table write.
func (k *KB) settle() {
	if !k.wroteButtons {
		return
	}
	if rest := writeGap - k.now().Sub(k.lastButtons); rest > 0 {
		k.sleep(rest)
	}
}

// WriteTypes writes a whole widget type table. The keyboard takes it over
// some time later and then asks for a redraw of the whole screen; until it
// has, a profile switch may move the change to the profile switched to, so
// the caller waits with WaitRedraw. (The per-position type report 0x35 is not
// sent: it names no profile and is queued in the same way.) Input that is already queued
// is dropped first, so that an older redraw request is not taken for the
// keyboard's answer to this table; see WaitRedraw.
func (k *KB) WriteTypes(t *proto.Table25) error {
	k.settle()
	if err := k.drainInput(); err != nil {
		return err
	}
	if err := k.d.SetFeature(t.Bytes()); err != nil {
		return fmt.Errorf("keyboard: writing type table: %w", err)
	}
	return nil
}

const (
	lightAckTimeout = 50 * time.Millisecond
	selLights       = 0xb0
	lightsID        = 0x11
)

// StreamLights writes the six a1 frames of p, each followed by its
// acknowledgement. Anything else read while waiting is dropped. A set is never
// left half-written, because the keyboard then waits for the rest and ignores
// later sets: once one acknowledgement is missing, the remaining banks are
// written without waiting for theirs, frames still queued are dropped, and an
// error wrapping device.ErrTimeout names the first bank not acknowledged. A
// write that fails returns at once: a write that times out wraps
// ErrStreamStalled (and not device.ErrTimeout), any other error is passed on.
func (k *KB) StreamLights(p *proto.LightPayload) error {
	var missed error
	for i, f := range proto.LightFrames(p) {
		bank := byte(i + 1)
		if err := k.d.WriteStream(f); err != nil {
			if errors.Is(err, device.ErrTimeout) {
				return fmt.Errorf("keyboard: lighting bank %d: %w", bank, ErrStreamStalled)
			}
			return fmt.Errorf("keyboard: lighting bank %d: %w", bank, err)
		}
		if missed != nil {
			continue
		}
		deadline := k.now().Add(lightAckTimeout)
		for acked := false; !acked && missed == nil; {
			remaining := deadline.Sub(k.now())
			if remaining <= 0 {
				missed = fmt.Errorf("keyboard: lighting bank %d not acknowledged: %w", bank, device.ErrTimeout)
				break
			}
			fr, err := k.d.ReadStream(remaining)
			if errors.Is(err, device.ErrTimeout) {
				missed = fmt.Errorf("keyboard: lighting bank %d acknowledgement: %w", bank, err)
			} else if err != nil {
				return fmt.Errorf("keyboard: lighting bank %d acknowledgement: %w", bank, err)
			} else {
				acked = proto.IsLightAck(fr, bank)
			}
		}
	}
	if missed != nil {
		for i := 0; i < maxDrain; i++ {
			if _, err := k.d.ReadStream(drainTimeout); err != nil {
				break
			}
		}
	}
	return missed
}

// ReadLights reads report 0x11 of profile p, in one selector read: the
// lighting stored for the profile, unscaled, and its brightness percentage.
func (k *KB) ReadLights(p byte) (*proto.LightPayload, int, error) {
	if err := k.selectTable(p, selLights); err != nil {
		return nil, 0, err
	}
	b, err := k.d.GetFeature(lightsID, proto.Len11)
	if err != nil {
		return nil, 0, fmt.Errorf("keyboard: reading lighting report: %w", err)
	}
	stored, v, err := proto.Lights11(b)
	if err != nil {
		return nil, 0, fmt.Errorf("keyboard: %w", err)
	}
	if v > 100 {
		return nil, 0, fmt.Errorf("keyboard: brightness %d is out of range 0 to 100", v)
	}
	return stored, v, nil
}

// ReadLightsTable reads report 0x11 of profile p whole, for writing back
// with WriteLightsTable.
func (k *KB) ReadLightsTable(p byte) (*proto.Table11, error) {
	if err := k.selectTable(p, selLights); err != nil {
		return nil, err
	}
	b, err := k.d.GetFeature(lightsID, proto.Len11)
	if err != nil {
		return nil, fmt.Errorf("keyboard: reading lighting report: %w", err)
	}
	t, err := proto.ParseTable11(b)
	if err != nil {
		return nil, fmt.Errorf("keyboard: %w", err)
	}
	if t.Profile() != p {
		return nil, fmt.Errorf("keyboard: lighting report is for profile %d, asked for %d", t.Profile(), p)
	}
	if v := t.Brightness(); v > 100 {
		return nil, fmt.Errorf("keyboard: brightness %d is out of range 0 to 100", v)
	}
	return t, nil
}

// WriteLightsTable writes report 0x11, the stored lighting of the profile it
// names, and then reads the status register as Swarm II does, which must
// read 05 01 00 00.
func (k *KB) WriteLightsTable(t *proto.Table11) error {
	k.settle()
	if err := k.d.SetFeature(t.Bytes()); err != nil {
		return fmt.Errorf("keyboard: writing lighting report: %w", err)
	}
	return k.checkStatus("lighting report")
}

// checkStatus waits selectDelay after a configuration write and reads the
// status register, report 0x05, which must read 05 01 00 00. what names the
// write in the error.
func (k *KB) checkStatus(what string) error {
	k.sleep(selectDelay)
	r, err := k.d.GetFeature(0x05, 4)
	if err != nil {
		return fmt.Errorf("keyboard: reading the status after the %s: %w", what, err)
	}
	if len(r) < 4 || r[0] != 0x05 || r[1] != 0x01 || r[2] != 0 || r[3] != 0 {
		return fmt.Errorf("keyboard: status % x after the %s, want 05 01 00 00", r[:min(len(r), 4)], what)
	}
	return nil
}
