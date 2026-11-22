// SPDX-License-Identifier: GPL-2.0-or-later

package keys

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Bananenbaas/commander/screen/internal/action"
)

const (
	inputClass = "/sys/class/input"
	devInput   = "/dev/input"
	chanSize   = 64
)

// readEvents is the number of input_events read at once.
const readEvents = 16

// findEventNodes lists the /dev/input/eventN nodes of the KB7 that report
// keys, sorted by name. classDir is /sys/class/input. It is an error when
// there is none.
func findEventNodes(classDir, devDir string) ([]string, error) {
	entries, err := os.ReadDir(classDir)
	if err != nil {
		return nil, fmt.Errorf("keys: the KB7's input device was not found: %w", err)
	}
	var nodes []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "event") {
			continue
		}
		dev := filepath.Join(classDir, name, "device")
		if sysfsValue(filepath.Join(dev, "id", "vendor")) != "10f5" ||
			sysfsValue(filepath.Join(dev, "id", "product")) != "5038" {
			continue
		}
		// Only nodes that can report keys; the KB7 has others.
		if caps := sysfsValue(filepath.Join(dev, "capabilities", "key")); caps == "" || strings.Trim(caps, "0 ") == "" {
			continue
		}
		nodes = append(nodes, filepath.ToSlash(filepath.Join(devDir, name)))
	}
	if len(nodes) == 0 {
		return nil, errors.New("keys: the KB7's input device was not found")
	}
	sort.Strings(nodes)
	return nodes, nil
}

// sysfsValue reads a one-line sysfs attribute, lower-cased and trimmed;
// empty when it cannot be read.
func sysfsValue(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(string(b)))
}

// Listen reports the presses and releases of the KB7's keys, read from its
// evdev nodes. The nodes are only read, never grabbed, so every key still
// reaches the system. Auto-repeat events are dropped. The channel holds 64
// events and a full channel drops events instead of blocking. It is closed
// when ctx ends, after all readers have stopped. Only one listener should run
// at a time.
//
// Each node has one goroutine with blocking reads; on ctx end the files are
// closed, which makes the blocked reads return. The input_event layout is that
// of 64-bit Linux; 32-bit is not supported. Reading needs access to the nodes,
// which the optional udev rule 71-kb7screen-keys.rules gives.
func Listen(ctx context.Context) (<-chan Event, error) {
	paths, err := findEventNodes(inputClass, devInput)
	if err != nil {
		return nil, err
	}
	var files []*os.File
	denied := false
	var firstErr error
	for _, p := range paths {
		f, err := os.OpenFile(p, os.O_RDONLY, 0)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				denied = true
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		if denied {
			return nil, fmt.Errorf("keys: no permission to read the KB7's input device (the optional udev rule 71-kb7screen-keys.rules gives it): %w", firstErr)
		}
		return nil, fmt.Errorf("keys: cannot open the KB7's input device: %w", firstErr)
	}

	out := make(chan Event, chanSize)
	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func(f *os.File) {
			defer wg.Done()
			defer f.Close()
			readNode(f, out)
		}(f)
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			for _, f := range files {
				f.Close() // makes the blocked reads return
			}
		case <-done:
		}
	}()
	go func() {
		wg.Wait()
		close(done)
		close(out)
	}()
	return out, nil
}

// readNode reads events until the file fails or is closed.
func readNode(r io.Reader, out chan<- Event) {
	buf := make([]byte, evdevSize*readEvents)
	for {
		n, err := r.Read(buf)
		for off := 0; off+evdevSize <= n; off += evdevSize {
			code, value, ok := decodeEvdev(buf[off : off+evdevSize])
			if !ok || value == 2 {
				continue
			}
			key, ok := action.KeyFromLinuxCode(code)
			if !ok {
				continue
			}
			select {
			case out <- Event{Key: key, Down: value == 1, At: time.Now()}:
			default:
			}
		}
		if err != nil {
			return
		}
	}
}
