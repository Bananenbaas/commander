// SPDX-License-Identifier: GPL-2.0-or-later

// Package access holds what the commands that use the keyboard share: the
// check that Swarm II leaves the keyboard alone, opening the keyboard, and
// the ledger of a configuration folder.
package access

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Bananenbaas/commander/screen/internal/apply"
	"github.com/Bananenbaas/commander/screen/internal/device"
	"github.com/Bananenbaas/commander/screen/internal/keyboard"
	"github.com/Bananenbaas/commander/screen/internal/ledger"
	"github.com/Bananenbaas/commander/screen/internal/procs"
)

// LedgerName is the ledger file in the configuration folder.
const LedgerName = ".kb7screen-state.json"

// ErrApplyStopped is reported when an apply was stopped and the ledger was
// saved.
var ErrApplyStopped = errors.New("stopped after the current picture; the ledger is saved")

// SwarmRunning returns the names of the Swarm II programs that run.
func SwarmRunning() ([]string, error) {
	m, err := procs.Running(procs.Swarm)
	if err != nil {
		return nil, fmt.Errorf("cannot tell whether Swarm II runs: %w", err)
	}
	var names []string
	for _, n := range procs.Swarm {
		if m[n] {
			names = append(names, n)
		}
	}
	return names, nil
}

// RefuseWhileSwarm returns an error when Swarm II or its device service runs.
// what names the work, as in "close it first, then <what> again".
func RefuseWhileSwarm(what string) error {
	names, err := SwarmRunning()
	if err != nil {
		return err
	}
	if len(names) > 0 {
		return fmt.Errorf("%s is running; close it first, then %s again (it would overwrite the buttons and receive the taps)", strings.Join(names, " and "), what)
	}
	return nil
}

// OpenKeyboard opens the KB7.
func OpenKeyboard() (*keyboard.KB, error) {
	dev, err := device.Open()
	if err != nil {
		return nil, err
	}
	return keyboard.New(dev), nil
}

// ProgramRunning reports whether the named program runs; an error counts as
// not running.
func ProgramRunning(name string) bool {
	m, err := procs.Running([]string{name})
	return err == nil && m[name]
}

// LoadLedger loads the ledger of a configuration folder and returns a
// function that saves it again.
func LoadLedger(dir string) (*ledger.Ledger, func() error, error) {
	path := filepath.Join(dir, LedgerName)
	l, err := ledger.Load(path)
	if err != nil {
		return nil, nil, err
	}
	return l, func() error { return l.Save(path) }, nil
}

// SaveAfter saves the ledger once more, whether or not the work failed, and
// returns the error to report: the work's, the save's, or both. Work stopped
// by its context is reported as stopped once the ledger is saved.
func SaveAfter(workErr error, save func() error, stopped error) error {
	saveErr := save()
	switch {
	case workErr != nil && saveErr != nil:
		return errors.Join(workErr, fmt.Errorf("saving the ledger: %w", saveErr))
	case saveErr != nil:
		return fmt.Errorf("saving the ledger: %w", saveErr)
	case errors.Is(workErr, apply.ErrStopped):
		return stopped
	}
	return workErr
}
