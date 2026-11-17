// SPDX-License-Identifier: GPL-2.0-or-later

// Package request carries a request for a running loop (stop or reload)
// through a one-word file in the config folder.
package request

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileName is the request file in the config folder.
const FileName = ".kb7screen-request"

// Word is the content of a request file.
type Word string

// The requests a loop understands.
const (
	Stop   Word = "stop"
	Reload Word = "reload"
)

// Write puts the request in dir, atomically: the word is written to a
// temporary file in the same folder, which is then renamed into place.
func Write(dir string, w Word) error {
	tmp, err := os.CreateTemp(dir, FileName+".tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.WriteString(string(w) + "\n")
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, filepath.Join(dir, FileName))
	}
	if err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Take reads and removes the request in dir. Without a request it returns
// false; a file with any other content is removed and also gives false.
func Take(dir string) (Word, bool, error) {
	path := filepath.Join(dir, FileName)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", false, err
	}
	switch w := Word(strings.TrimSpace(string(data))); w {
	case Stop, Reload:
		return w, true, nil
	}
	return "", false, nil
}

// Clear removes a request in dir, if there is one.
func Clear(dir string) error {
	err := os.Remove(filepath.Join(dir, FileName))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Send asks the loop that holds dir to carry out w and waits up to wait for
// it to take the request. It refuses, writing nothing, when held reports that
// nothing holds dir. When nobody took the request in time it is withdrawn, so
// it cannot fire later.
func Send(dir string, w Word, wait time.Duration, held func(string) (bool, error)) (taken bool, err error) {
	ok, err := held(dir)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("no kb7screen run or Commander is running for %s", dir)
	}
	if err := Write(dir, w); err != nil {
		return false, err
	}
	path := filepath.Join(dir, FileName)
	deadline := time.Now().Add(wait)
	for {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return true, nil
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Withdraw the request, but only if it is still the one written here.
	if data, err := os.ReadFile(path); err == nil && Word(strings.TrimSpace(string(data))) == w {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}
