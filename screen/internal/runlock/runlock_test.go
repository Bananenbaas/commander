// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows || linux

package runlock

import (
	"errors"
	"testing"
)

func TestSecondAcquireIsHeld(t *testing.T) {
	dir := t.TempDir()
	release, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := Acquire(dir); !errors.Is(err, ErrHeld) {
		t.Fatalf("second Acquire = %v, want ErrHeld", err)
	}
	held, err := Held(dir)
	if err != nil || !held {
		t.Fatalf("Held = %v, %v; want true, nil", held, err)
	}
}

func TestReleaseFreesTheLock(t *testing.T) {
	dir := t.TempDir()
	release, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	held, err := Held(dir)
	if err != nil || held {
		t.Fatalf("Held after release = %v, %v; want false, nil", held, err)
	}
	again, err := Acquire(dir)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	again()
}

func TestDifferentDirsDoNotInterfere(t *testing.T) {
	a, err := Acquire(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a()
	b, err := Acquire(t.TempDir())
	if err != nil {
		t.Fatalf("other dir: %v", err)
	}
	b()
}
