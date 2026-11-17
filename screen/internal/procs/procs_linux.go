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
