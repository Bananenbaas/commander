// SPDX-License-Identifier: GPL-2.0-or-later

package audio

import (
	"errors"
	"math"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// ErrClosed is returned by the calls made after Close.
var ErrClosed = errors.New("the audio is closed")

// ErrUnsupported is returned by New on systems other than Windows, which
// have no mixer.
var ErrUnsupported = errors.New("the mixer needs Windows")

// ErrNoDevice is returned while the computer has no default output device.
var ErrNoDevice = errors.New("no default audio output device")

// engine is the platform's audio access. All its methods, open included, are
// called on one goroutine that stays on one OS thread.
type engine interface {
	sessions() (system Session, programs []Session, err error)
	setSystem(level int) error // level is 0 to 100
	setProgram(id string, level int) error
	close()
}

// opener starts an engine. signal may be called from any thread, any number
// of times; it never blocks.
type opener func(signal func(), logf func(string, ...any)) (engine, error)

// Windows follows the system volume and the programs with sound sessions on
// the default output device of Windows. Its methods may be called from any
// goroutine; the audio calls themselves run on one goroutine of its own.
type Windows struct {
	reqs    chan request
	changes chan struct{}
	quit    chan struct{}
	done    chan struct{}
	once    sync.Once
}

// request is a call forwarded to the audio goroutine.
type request struct {
	fn    func(engine) error
	reply chan error
}

// New starts following the Windows audio. logf receives the rare messages
// worth a log line; nil discards them. Off Windows it returns ErrUnsupported,
// and on 32-bit or ARM Windows another error.
func New(logf func(string, ...any)) (*Windows, error) {
	return start(logf, openEngine)
}

func start(logf func(string, ...any), open opener) (*Windows, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	w := &Windows{
		reqs:    make(chan request),
		changes: make(chan struct{}, 1),
		quit:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	ready := make(chan error, 1)
	go w.serve(logf, open, ready)
	if err := <-ready; err != nil {
		<-w.done
		return nil, err
	}
	return w, nil
}

// serve runs the engine on a goroutine locked to its OS thread. The thread
// is not unlocked: it ends with the goroutine, together with whatever the
// platform set up on it.
func (w *Windows) serve(logf func(string, ...any), open opener, ready chan<- error) {
	defer close(w.done)
	runtime.LockOSThread()
	e, err := open(w.signal, logf)
	ready <- err
	if err != nil {
		return
	}
	defer e.close()
	for {
		select {
		case r := <-w.reqs:
			r.reply <- r.fn(e)
		case <-w.quit:
			return
		}
	}
}

// signal notes a change without waiting; a change already pending stands
// for this one too.
func (w *Windows) signal() {
	select {
	case w.changes <- struct{}{}:
	default:
	}
}

// do runs fn on the audio goroutine and returns its error.
func (w *Windows) do(fn func(engine) error) error {
	r := request{fn: fn, reply: make(chan error, 1)}
	select {
	case w.reqs <- r:
		return <-r.reply
	case <-w.done:
		return ErrClosed
	}
}

// Sessions returns the system volume and the programs that have a sound
// session on the default output device, one per process, in the order
// Windows lists them; Active tells those that play sound.
func (w *Windows) Sessions() (system Session, programs []Session, err error) {
	err = w.do(func(e engine) error {
		var err error
		system, programs, err = e.sessions()
		return err
	})
	return system, programs, err
}

// SetSystemLevel sets the volume of the default output device, 0 to 100.
func (w *Windows) SetSystemLevel(level int) error {
	level = clamp(level)
	return w.do(func(e engine) error { return e.setSystem(level) })
}

// SetProgramLevel sets the volume of every session of the program with the
// given ID, 0 to 100.
func (w *Windows) SetProgramLevel(id string, level int) error {
	level = clamp(level)
	return w.do(func(e engine) error { return e.setProgram(id, level) })
}

// Changes signals that the sessions are to be read again. It is never
// closed; a signal pending while nobody reads stands for every change since.
func (w *Windows) Changes() <-chan struct{} { return w.changes }

// Close stops following the audio and unregisters everything from Windows.
// Calls after Close return ErrClosed. Close may be called more than once.
func (w *Windows) Close() error {
	w.once.Do(func() { close(w.quit) })
	<-w.done
	return nil
}

func clamp(level int) int {
	return min(max(level, 0), 100)
}

// levelOf turns a Windows volume, 0.0 to 1.0, into 0 to 100.
func levelOf(f float32) int {
	return clamp(int(math.Round(float64(f) * 100)))
}

// rawSession is one Windows audio session, before the sessions of one
// process are merged.
type rawSession struct {
	PID     uint32
	Display string // the display name the program set; may be empty or an @-resource
	Exe     string // the executable name without ".exe"; "" when unknown
	Level   int
	Muted   bool
	Active  bool // the session plays sound
}

// usableName reports whether a display name can be shown as it is: not
// empty and not an "@file,-id" resource reference.
func usableName(name string) bool {
	name = strings.TrimSpace(name)
	return name != "" && !strings.HasPrefix(name, "@")
}

// exeName returns the base name of an executable path without ".exe".
func exeName(path string) string {
	if i := strings.LastIndexAny(path, `\/`); i >= 0 {
		path = path[i+1:]
	}
	if len(path) > 4 && strings.EqualFold(path[len(path)-4:], ".exe") {
		path = path[:len(path)-4]
	}
	return path
}

// merge turns sessions into programs, one per process, in the order of each
// process's first session. A program's level and mute are those of its
// first session, and it is active when any of its sessions is. Its name is
// the first usable display name of its sessions, else the executable name of
// its first session that has one, else "Program <pid>".
func merge(raw []rawSession) []Session {
	var out []Session
	at := map[uint32]int{}
	named := map[uint32]bool{}
	exe := map[uint32]string{}
	for _, r := range raw {
		i, seen := at[r.PID]
		if !seen {
			i = len(out)
			at[r.PID] = i
			out = append(out, Session{ID: strconv.FormatUint(uint64(r.PID), 10), Level: r.Level, Muted: r.Muted})
		}
		out[i].Active = out[i].Active || r.Active
		if exe[r.PID] == "" {
			exe[r.PID] = r.Exe
		}
		if !named[r.PID] && usableName(r.Display) {
			out[i].Name = strings.TrimSpace(r.Display)
			named[r.PID] = true
		}
	}
	for pid, i := range at {
		if named[pid] {
			continue
		}
		if name := exe[pid]; name != "" {
			out[i].Name = name
		} else {
			out[i].Name = "Program " + out[i].ID
		}
	}
	return out
}
