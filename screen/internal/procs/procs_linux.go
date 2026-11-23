// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux

package procs

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
)

// list returns the comm and the argv[0] of every readable process. Entries
// that vanish or cannot be read are skipped.
func list() ([]string, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		dir := filepath.Join("/proc", e.Name())
		if b, err := os.ReadFile(filepath.Join(dir, "comm")); err == nil {
			if s := string(bytes.TrimRight(b, "\n")); s != "" {
				out = append(out, s)
			}
		}
		if b, err := os.ReadFile(filepath.Join(dir, "cmdline")); err == nil {
			argv0, _, _ := bytes.Cut(b, []byte{0})
			if len(argv0) > 0 {
				out = append(out, string(argv0))
			}
		}
	}
	return out, nil
}

// listProcs returns every readable process: its comm as the name and, when
// /proc/<pid>/exe may be read, the full path.
func listProcs() ([]Proc, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []Proc
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		dir := filepath.Join("/proc", e.Name())
		b, err := os.ReadFile(filepath.Join(dir, "comm"))
		if err != nil {
			continue
		}
		name := string(bytes.TrimRight(b, "\n"))
		if name == "" {
			continue
		}
		path, _ := os.Readlink(filepath.Join(dir, "exe"))
		out = append(out, Proc{Name: name, Path: path})
	}
	return out, nil
}
