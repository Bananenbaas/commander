// SPDX-License-Identifier: GPL-2.0-or-later

package action

import (
	"sync"
	"time"
)

// Injector presses and releases single keys on the PC.
type Injector interface {
	Down(k Key) error
	Up(k Key) error
}

// Launcher starts programs and opens URLs, files and folders.
type Launcher interface {
	Run(program string, args []string) error
	Open(target string) error
}

const (
	tapHold     = 40 * time.Millisecond
	stepSpacing = 20 * time.Millisecond
)

// Executor carries out action steps. It is safe for concurrent use: a single
// mutex serialises everything, and Tap holds it for its whole duration
// (including waits), so other calls block until the tap has finished.
type Executor struct {
	In    Injector
	L     Launcher
	Sleep func(time.Duration) // time.Sleep when nil

	mu   sync.Mutex
	held []Key
}

func (e *Executor) sleep(d time.Duration) {
	if e.Sleep != nil {
		e.Sleep(d)
		return
	}
	time.Sleep(d)
}

// Tap runs the steps in order. A chord presses its keys in order, holds them
// for 40 ms and releases them in reverse; consecutive steps are 20 ms apart.
// If the injector fails, the keys of the current chord that were pressed are
// still released and the error is returned.
func (e *Executor) Tap(steps []Step) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, s := range steps {
		if i > 0 {
			e.sleep(stepSpacing)
		}
		var err error
		switch s := s.(type) {
		case Chord:
			err = e.tapChord(s)
		case Wait:
			e.sleep(s.D)
		case Run:
			err = e.L.Run(s.Program, s.Args)
		case Open:
			err = e.L.Open(s.Target)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *Executor) tapChord(c Chord) error {
	pressed, err := e.press(c.Keys)
	if err == nil {
		e.sleep(tapHold)
	}
	if rerr := e.release(pressed); err == nil {
		err = rerr
	}
	return err
}

// press presses keys in order and returns those that went down.
func (e *Executor) press(keys []Key) ([]Key, error) {
	var pressed []Key
	for _, k := range keys {
		if err := e.In.Down(k); err != nil {
			return pressed, err
		}
		pressed = append(pressed, k)
	}
	return pressed, nil
}

// release releases keys in reverse order, continuing past errors and
// returning the first one.
func (e *Executor) release(keys []Key) error {
	var first error
	for i := len(keys) - 1; i >= 0; i-- {
		if err := e.In.Up(keys[i]); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// HoldDown presses the chord's keys in order and remembers them.
func (e *Executor) HoldDown(c Chord) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	pressed, err := e.press(c.Keys)
	e.held = append(e.held, pressed...)
	return err
}

// HoldUp releases the chord's keys in reverse order and forgets them.
func (e *Executor) HoldUp(c Chord) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var first error
	for i := len(c.Keys) - 1; i >= 0; i-- {
		if err := e.In.Up(c.Keys[i]); err != nil {
			if first == nil {
				first = err
			}
			continue // stays held so ReleaseAll can retry
		}
		e.forget(c.Keys[i])
	}
	return first
}

func (e *Executor) forget(k Key) {
	for i, h := range e.held {
		if h == k {
			e.held = append(e.held[:i], e.held[i+1:]...)
			return
		}
	}
}

// ReleaseAll releases every key still held, in reverse press order.
func (e *Executor) ReleaseAll() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	keys := e.held
	e.held = nil
	return e.release(keys)
}
