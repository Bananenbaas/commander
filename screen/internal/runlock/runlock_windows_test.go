// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package runlock

import (
	"errors"
	"testing"

	"golang.org/x/sys/windows"
)

func TestMapCreateError(t *testing.T) {
	if err := mapCreateError("x", windows.ERROR_SHARING_VIOLATION); !errors.Is(err, ErrHeld) {
		t.Fatalf("sharing violation = %v, want ErrHeld", err)
	}
	err := mapCreateError("x", windows.ERROR_ACCESS_DENIED)
	if errors.Is(err, ErrHeld) {
		t.Fatal("access denied must not map to ErrHeld")
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("access denied = %v, want the cause wrapped", err)
	}
}
