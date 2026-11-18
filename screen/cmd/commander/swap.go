// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// swapIn puts exe+".new" in the place of exe and keeps the old file as
// exe+".old". Windows lets a running executable be renamed, so plain renames
// do. If the second rename fails, the first is undone. undo reverses both
// renames, for when the new file cannot be started.
func swapIn(exe string) (undo func() error, err error) {
	if err := os.Rename(exe, exe+".old"); err != nil {
		return nil, fmt.Errorf("move the running program aside: %w", err)
	}
	if err := os.Rename(exe+".new", exe); err != nil {
		if back := os.Rename(exe+".old", exe); back != nil {
			err = errors.Join(err, fmt.Errorf("put the old program back: %w", back))
		}
		return nil, fmt.Errorf("put the new program in place: %w", err)
	}
	undo = func() error {
		if err := os.Rename(exe, exe+".new"); err != nil {
			return fmt.Errorf("move the new program aside: %w", err)
		}
		if err := os.Rename(exe+".old", exe); err != nil {
			return fmt.Errorf("put the old program back: %w", err)
		}
		return nil
	}
	return undo, nil
}

// cleanLeftovers removes exe+".old" and exe+".new" left by an update; with
// keepOld only the .new one. Files that are not there are not errors.
func cleanLeftovers(exe string, keepOld bool) []error {
	var errs []error
	names := []string{exe + ".new"}
	if !keepOld {
		names = []string{exe + ".old", exe + ".new"}
	}
	for _, name := range names {
		if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errs
}

// removeOld removes path, trying every half second for about 5 s while the
// old Commander is still ending. If it stays, it is left for the next start.
func removeOld(ctx context.Context, path string, logf func(string, ...any)) {
	var err error
	for i := 0; i < 10; i++ {
		if err = os.Remove(path); err == nil || errors.Is(err, fs.ErrNotExist) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
	logf("the old program is left for the next start: %v", err)
}

// checkWritable creates and removes a temporary file in dir. The error wraps
// the cause: an update cannot be installed next to a program in a folder the
// user cannot write to.
func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".commander-write-test-*")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", filepath.Clean(dir), err)
	}
	name := f.Name()
	f.Close()
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("cannot write to %s: %w", filepath.Clean(dir), err)
	}
	return nil
}
