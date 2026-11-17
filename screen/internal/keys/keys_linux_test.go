// SPDX-License-Identifier: GPL-2.0-or-later

//go:build linux

package keys

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func fakeNode(t *testing.T, root, name, vendor, product, keyCaps string) {
	t.Helper()
	dev := filepath.Join(root, name, "device")
	for file, content := range map[string]string{
		"id/vendor":        vendor,
		"id/product":       product,
		"capabilities/key": keyCaps,
	} {
		p := filepath.Join(dev, file)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindEventNodes(t *testing.T) {
	root := t.TempDir()
	fakeNode(t, root, "event3", "10f5", "5038", "ffff 0")
	fakeNode(t, root, "event4", "10f5", "5038", "0")      // no keys (mouse-like interface)
	fakeNode(t, root, "event5", "046d", "c31c", "ffff 0") // other keyboard
	fakeNode(t, root, "event12", "10F5", "5038", "1 2")   // second KB7 node
	fakeNode(t, root, "mouse0", "10f5", "5038", "ffff 0") // not an event node
	got, err := findEventNodes(root, "/dev/input")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/dev/input/event12", "/dev/input/event3"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindEventNodesNone(t *testing.T) {
	if _, err := findEventNodes(t.TempDir(), "/dev/input"); err == nil {
		t.Error("no nodes must be an error")
	}
}
