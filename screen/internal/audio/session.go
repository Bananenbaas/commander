// SPDX-License-Identifier: GPL-2.0-or-later

// Package audio describes the computer's sound for the keyboard's volume
// mixer page: the system volume and the programs with sound sessions.
//
// It imports nothing of kb7screen, so that both the run loop (as
// run.Session) and the code that reads the volumes can use Session.
package audio

// Session is the system volume or one program's volume.
type Session struct {
	// ID tells programs apart, also two with the same name; it is the same
	// for as long as the program plays.
	ID string
	// Name is what the mixer page shows.
	Name string
	// Level is the volume, 0 to 100.
	Level int
	// Muted is set while the sound is muted.
	Muted bool
	// Active is set while the program plays sound; a program that only
	// holds a session, such as a paused player, is not active. It is unset
	// for the system volume.
	Active bool
}
