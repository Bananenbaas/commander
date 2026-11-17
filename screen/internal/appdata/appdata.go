// SPDX-License-Identifier: GPL-2.0-or-later

// Package appdata keeps Commander's own files: the settings in
// %APPDATA%\Commander\settings.toml and the log in
// %LOCALAPPDATA%\Commander\commander.log.
package appdata

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	appName     = "Commander"
	stampLayout = "2006-01-02 15:04:05"
	// maxLog is the size the log may reach before it is rotated.
	maxLog = 1 << 20
)

// roamingDir is %APPDATA%, or the user's configuration folder where that is
// not set (Linux).
func roamingDir() (string, error) {
	if d := os.Getenv("APPDATA"); d != "" {
		return d, nil
	}
	return os.UserConfigDir()
}

// localDir is %LOCALAPPDATA%, or the user's cache folder where that is not
// set (Linux), or the temporary folder as a last resort.
func localDir() string {
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return d
	}
	if d, err := os.UserCacheDir(); err == nil {
		return d
	}
	return os.TempDir()
}

func settingsPath() (string, error) {
	d, err := roamingDir()
	if err != nil {
		return "", fmt.Errorf("finding the settings folder: %w", err)
	}
	return filepath.Join(d, appName, "settings.toml"), nil
}

type settings struct {
	Folder string `toml:"folder"`
}

// LoadFolder returns the configuration folder remembered in the settings, or
// "" when there are no settings yet.
func LoadFolder() (string, error) {
	path, err := settingsPath()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	var s settings
	if err := toml.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return s.Folder, nil
}

// SaveFolder remembers dir as the configuration folder. The file is replaced
// in one step, so a crash never leaves half a file.
func SaveFolder(dir string) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(settings{Folder: dir}); err != nil {
		return fmt.Errorf("writing the settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("writing the settings: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "settings-*.tmp")
	if err != nil {
		return fmt.Errorf("writing the settings: %w", err)
	}
	_, werr := tmp.Write(b.Bytes())
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("writing %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// LogPath is the path of Commander's log file.
func LogPath() string {
	return filepath.Join(localDir(), appName, "commander.log")
}

// OpenLog opens the log for appending. Every line written gets the date and
// time in front; when the next line would take the file past 1 MB, the file
// is renamed to commander.log.1 (replacing an older one) and a new one is
// started. The writer is safe for concurrent use.
func OpenLog() (io.WriteCloser, error) {
	path := LogPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("opening the log: %w", err)
	}
	l := &logFile{path: path, lineStart: true}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

type logFile struct {
	mu        sync.Mutex
	path      string
	f         *os.File
	size      int64
	lineStart bool // the next byte written starts a line
}

func (l *logFile) open() error {
	f, err := os.OpenFile(l.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening the log: %w", err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("opening the log: %w", err)
	}
	l.f, l.size = f, fi.Size()
	return nil
}

// rotate moves the full log aside and starts a new one. When the rename
// fails the old file is opened again and the log keeps growing.
func (l *logFile) rotate() error {
	l.f.Close()
	l.f = nil
	os.Rename(l.path, l.path+".1")
	return l.open()
}

// Write writes p, putting a timestamp in front of every line. n counts the
// bytes of p written, not the timestamps.
func (l *logFile) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return 0, os.ErrClosed
	}
	for len(p) > 0 {
		seg := p
		if i := bytes.IndexByte(p, '\n'); i >= 0 {
			seg = p[:i+1]
		}
		var chunk []byte
		if l.lineStart {
			chunk = append(time.Now().AppendFormat(chunk, stampLayout), ' ')
			if l.size > 0 && l.size+int64(len(chunk)+len(seg)) > maxLog {
				if err := l.rotate(); err != nil {
					return n, err
				}
			}
		}
		chunk = append(chunk, seg...)
		w, err := l.f.Write(chunk)
		l.size += int64(w)
		if err != nil {
			return n, err
		}
		n += len(seg)
		p = p[len(seg):]
		l.lineStart = seg[len(seg)-1] == '\n'
	}
	return n, nil
}

// Close closes the log file.
func (l *logFile) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return os.ErrClosed
	}
	err := l.f.Close()
	l.f = nil
	return err
}
