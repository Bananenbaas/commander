// SPDX-License-Identifier: GPL-2.0-or-later

package proto

import "fmt"

// Lighting stream: the payload is 7 blocks of R[16] G[16] B[16], one LED per
// column, followed by 24 zero bytes, and goes out as six a1 frames.
const (
	LightPayloadLen = 360
	lightBlock      = 48
	lightBanks      = 6
	lightPerFrame   = 60
	lightColours    = 7 * lightBlock // the colour bytes, before the zero tail

	// Len11 is the length of report 0x11 as GetFeature returns it.
	Len11     = 348
	colours11 = 10 // where the colours start in report 0x11
)

// LightPayload is the 360 byte colour payload of one lighting update.
type LightPayload [LightPayloadLen]byte

// SetLED sets the colour of LED led. LEDs outside the payload are ignored.
func SetLED(p *LightPayload, led int, r, g, b byte) {
	if led < 0 || led >= LightPayloadLen/lightBlock*16 {
		return
	}
	base := led/16*lightBlock + led%16
	p[base], p[base+16], p[base+32] = r, g, b
}

// LightFrames splits a payload into the six a1 frames that carry it.
func LightFrames(p *LightPayload) [6][64]byte {
	var out [lightBanks][64]byte
	for n := range out {
		f := &out[n]
		f[0], f[1] = 0xa1, byte(n+1)
		if n == 0 {
			f[2], f[3] = 0x68, 0x01
		}
		copy(f[4:], p[n*lightPerFrame:(n+1)*lightPerFrame])
	}
	return out
}

// IsLightAck reports whether fr is the acknowledgement of bank.
func IsLightAck(fr [64]byte, bank byte) bool { return fr[0] == 0x32 && fr[1] == bank }

// Lights11 parses report 0x11, given as GetFeature returns it (report ID
// first): the stored colours, bytes 10 to 345, in the layout of the a1
// payload's first 336 bytes (the 24-byte tail stays zero), and the brightness
// percentage, byte 6. The colours are as configured, not scaled by the
// brightness.
func Lights11(b []byte) (*LightPayload, int, error) {
	t, err := ParseTable11(b)
	if err != nil {
		return nil, 0, err
	}
	return t.Colours(), t.Brightness(), nil
}

// brightness11 is where report 0x11 holds the brightness percentage.
const brightness11 = 6

// Table11 is report 0x11, a profile's stored lighting: byte 3 the profile,
// byte 6 the brightness in percent, the colours from byte 10 and a 16-bit
// little-endian sum of the other bytes at the end. The bytes it does not
// interpret are kept as read.
type Table11 struct{ b [Len11]byte }

// ParseTable11 parses report 0x11 as GetFeature returns it, report ID first.
func ParseTable11(b []byte) (*Table11, error) {
	if len(b) != Len11 {
		return nil, fmt.Errorf("report 0x11: %d bytes, want %d", len(b), Len11)
	}
	if b[0] != 0x11 {
		return nil, fmt.Errorf("report 0x11: report id 0x%02x", b[0])
	}
	s := Sum16(b[:Len11-2])
	if b[Len11-2] != byte(s) || b[Len11-1] != byte(s>>8) {
		return nil, fmt.Errorf("report 0x11: bad checksum")
	}
	t := new(Table11)
	copy(t.b[:], b)
	return t, nil
}

// Profile is the profile the table belongs to.
func (t *Table11) Profile() byte { return t.b[3] }

// Colours returns the stored colours in the layout of a LightPayload; the
// 24-byte tail is zero.
func (t *Table11) Colours() *LightPayload {
	p := new(LightPayload)
	copy(p[:lightColours], t.b[colours11:colours11+lightColours])
	return p
}

// SetColours stores the colours of p; its 24-byte tail is not part of the
// table.
func (t *Table11) SetColours(p *LightPayload) {
	copy(t.b[colours11:colours11+lightColours], p[:lightColours])
}

// Brightness is the stored brightness in percent.
func (t *Table11) Brightness() int { return int(t.b[brightness11]) }

// SetBrightness stores a brightness of 0 to 100 percent.
func (t *Table11) SetBrightness(pct int) error {
	if pct < 0 || pct > 100 {
		return fmt.Errorf("report 0x11: brightness %d is not from 0 to 100", pct)
	}
	t.b[brightness11] = byte(pct)
	return nil
}

// Bytes returns the report to write, with its sum recomputed.
func (t *Table11) Bytes() []byte {
	out := make([]byte, Len11)
	copy(out, t.b[:])
	s := Sum16(out[:Len11-2])
	out[Len11-2], out[Len11-1] = byte(s), byte(s>>8)
	return out
}
