// SPDX-License-Identifier: GPL-2.0-or-later

package autostart

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// keyPath is the key the value lives under; tests point it elsewhere.
var keyPath = runKey

// Enabled reports whether Windows starts exe: the value exists and names
// this program (compared without regard to case).
func Enabled(exe string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading the Run key: %w", err)
	}
	defer k.Close()
	v, _, err := k.GetStringValue(valueName)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading the Run key: %w", err)
	}
	got, _, ok := Parse(v)
	return ok && strings.EqualFold(filepath.Clean(got), filepath.Clean(exe)), nil
}

// Enable makes Windows start exe with the configuration folder dir at log on.
func Enable(exe, dir string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("opening the Run key: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue(valueName, Command(exe, dir)); err != nil {
		return fmt.Errorf("writing the Run key: %w", err)
	}
	return nil
}

// Disable removes the value, so Windows no longer starts Commander. A value
// that is not there is not an error.
func Disable() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("opening the Run key: %w", err)
	}
	defer k.Close()
	if err := k.DeleteValue(valueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("writing the Run key: %w", err)
	}
	return nil
}
