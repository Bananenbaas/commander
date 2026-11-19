// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"fmt"
	"io"
	"time"

	"github.com/Bananenbaas/kb7-commander/screen/internal/audio"
)

// audioSeconds is how long the audio command follows the changes.
const audioSeconds = 20

// doAudio is a developer check of the volume mixer's audio side: it prints
// the system volume, the number of output devices followed and the programs
// with sound sessions on any of them, then again after every change Windows
// reports, for audioSeconds. It does not touch the keyboard and changes no
// volume.
func doAudio(stdout, stderr io.Writer) error {
	a, err := audio.New(func(format string, args ...any) {
		fmt.Fprintf(stderr, format+"\n", args...)
	})
	if err != nil {
		return err
	}
	defer a.Close()
	ctx, stop := stopOnSignal()
	defer stop()

	start := time.Now()
	printSessions(stdout, a, 0)
	end := time.After(audioSeconds * time.Second)
	fmt.Fprintf(stdout, "following changes for %d s\n", audioSeconds)
	for {
		select {
		case <-a.Changes():
			printSessions(stdout, a, time.Since(start))
		case <-end:
			return nil
		case <-ctx.Done():
			return nil
		}
	}
}

func printSessions(w io.Writer, a *audio.Windows, at time.Duration) {
	sys, progs, err := a.Sessions()
	if err != nil {
		fmt.Fprintf(w, "[%5.1fs] %v\n", at.Seconds(), err)
		return
	}
	fmt.Fprintf(w, "[%5.1fs] system %3d%s, devices %d\n", at.Seconds(), sys.Level, muted(sys.Muted), a.Devices())
	for _, p := range progs {
		fmt.Fprintf(w, "         %-8s %3d%s  %s\n", p.ID, p.Level, muted(p.Muted), p.Name)
	}
}

func muted(m bool) string {
	if m {
		return " muted"
	}
	return ""
}
