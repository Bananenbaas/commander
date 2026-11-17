//go:build linux

package procs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunningFindsSelf(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := filepath.Base(exe)
	got, err := Running([]string{name, "no-such-process-kb7"})
	if err != nil {
		t.Fatal(err)
	}
	if !got[name] || got["no-such-process-kb7"] {
		t.Fatalf("got %v", got)
	}
}
