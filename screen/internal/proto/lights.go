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

	// Len11 is the length of report 0x11 as GetFeature returns it.
	Len11 = 348
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

// Brightness11 returns the brightness percentage in report 0x11, given as
// GetFeature returns it (report ID first).
func Brightness11(b []byte) (int, error) {
	if len(b) != Len11 {
		return 0, fmt.Errorf("report 0x11: %d bytes, want %d", len(b), Len11)
	}
	if b[0] != 0x11 {
		return 0, fmt.Errorf("report 0x11: report id 0x%02x", b[0])
	}
	s := Sum16(b[:Len11-2])
	if b[Len11-2] != byte(s) || b[Len11-1] != byte(s>>8) {
		return 0, fmt.Errorf("report 0x11: bad checksum")
	}
	return int(b[6]), nil
}
