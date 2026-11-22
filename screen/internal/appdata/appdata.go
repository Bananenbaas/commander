// SPDX-License-Identifier: GPL-2.0-or-later

// Package appdata keeps Commander's own files: the settings in
// %APPDATA%\Commander\settings.toml, the log in
// %LOCALAPPDATA%\Commander\commander.log and the window's log next to it.
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

// now is the clock of the log; tests replace it.
var now = time.Now

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
	Folder       string `toml:"folder"`
	CheckUpdates bool   `toml:"check_updates"`
	Theme        string `toml:"theme"`
}

// load reads the settings. found is false when there is no file yet; a file
// that cannot be read or parsed is an error.
func load() (s settings, found bool, err error) {
	path, err := settingsPath()
	if err != nil {
		return settings{}, false, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return settings{}, false, nil
	}
	if err != nil {
		return settings{}, false, fmt.Errorf("reading %s: %w", path, err)
	}
	if err := toml.Unmarshal(raw, &s); err != nil {
		return settings{}, true, fmt.Errorf("reading %s: %w", path, err)
	}
	return s, true, nil
}

// LoadFolder returns the configuration folder remembered in the settings, or
// "" when there are no settings yet.
func LoadFolder() (string, error) {
	s, _, err := load()
	if err != nil {
		return "", err
	}
	return s.Folder, nil
}

// LoadCheckUpdates reports whether Commander may look for updates by itself.
// It is false when there are no settings or the key is missing.
func LoadCheckUpdates() (bool, error) {
	s, _, err := load()
	if err != nil {
		return false, err
	}
	return s.CheckUpdates, nil
}

// LoadTheme returns the theme of the window: "system", "light" or "dark".
// It is "system" when there are no settings or the key is missing.
func LoadTheme() (string, error) {
	s, _, err := load()
	if err != nil {
		return "", err
	}
	if s.Theme == "" {
		return "system", nil
	}
	return s.Theme, nil
}

// SaveTheme remembers the theme of the window, which is "system", "light" or
// "dark", and keeps the other settings. A settings file that cannot be read
// is an error and is left alone.
func SaveTheme(theme string) error {
	switch theme {
	case "system", "light", "dark":
	default:
		return fmt.Errorf("unknown theme %q", theme)
	}
	s, _, err := load()
	if err != nil {
		return err
	}
	s.Theme = theme
	return save(s)
}

// SaveFolder remembers dir as the configuration folder and keeps the other
// settings. The file is replaced in one step, so a crash never leaves half a
// file. A settings file that cannot be read is an error and is left alone.
func SaveFolder(dir string) error {
	s, _, err := load()
	if err != nil {
		return err
	}
	s.Folder = dir
	return save(s)
}

// SaveCheckUpdates remembers whether Commander checks for updates by itself
// and keeps the other settings. A settings file that cannot be read is an
// error and is left alone.
func SaveCheckUpdates(on bool) error {
	s, _, err := load()
	if err != nil {
		return err
	}
	s.CheckUpdates = on
	return save(s)
}

// save replaces the settings file in one step.
func save(s settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	var b bytes.Buffer
	if err := toml.NewEncoder(&b).Encode(s); err != nil {
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
// started. Callers write whole lines (as log.Logger does): the size check is
// made at the start of a line, so a line is never split over two files. When
// the rename fails the current file keeps growing and the rotation is tried
// again at most once a minute; when the file cannot be opened again, the next
// Write tries once more. The writer is safe for concurrent use.
func OpenLog() (io.WriteCloser, error) {
	return openLogAt(LogPath())
}

// WindowLogPath is the path of the window process's log file.
func WindowLogPath() string {
	return filepath.Join(localDir(), appName, "window.log")
}

// OpenWindowLog opens the log of the window process (commander.exe
// --window), %LOCALAPPDATA%\Commander\window.log, which works and rotates
// as OpenLog does. The window has a log of its own so that two processes
// never write and rotate the same file.
func OpenWindowLog() (io.WriteCloser, error) {
	return openLogAt(WindowLogPath())
}

// WebView2Dir is the folder WebView2 keeps the window's data in.
func WebView2Dir() string {
	return filepath.Join(localDir(), appName, "WebView2")
}

// openLogAt opens the log at path for appending; see OpenLog.
func openLogAt(path string) (io.WriteCloser, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("opening the log: %w", err)
	}
	l := &logFile{path: path, lineStart: true}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

// rotateRetry is how long a failed rotation waits before it is tried again.
const rotateRetry = time.Minute

type logFile struct {
	mu        sync.Mutex
	path      string
	f         *os.File // nil after a failed open; the next Write opens it
	size      int64
	lineStart bool      // the next byte written starts a line
	retryAt   time.Time // no rotation before this, after a failed rename
	closed    bool
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

// rotate moves the full log aside and starts a new one. The file is closed
// first, as Windows does not rename an open file. When the rename fails the
// same file is opened again and keeps growing until the next attempt.
func (l *logFile) rotate() error {
	l.f.Close()
	l.f = nil
	if err := os.Rename(l.path, l.path+".1"); err != nil {
		l.retryAt = now().Add(rotateRetry)
	}
	return l.open()
}

// Write writes p, putting a timestamp in front of every line. n counts the
// bytes of p written, not the timestamps.
func (l *logFile) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, os.ErrClosed
	}
	if l.f == nil {
		if err := l.open(); err != nil {
			return 0, err
		}
	}
	for len(p) > 0 {
		seg := p
		if i := bytes.IndexByte(p, '\n'); i >= 0 {
			seg = p[:i+1]
		}
		var chunk []byte
		if l.lineStart {
			t := now()
			chunk = append(t.AppendFormat(chunk, stampLayout), ' ')
			full := l.size > 0 && l.size+int64(len(chunk)+len(seg)) > maxLog
			if full && !t.Before(l.retryAt) {
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
	if l.closed {
		return os.ErrClosed
	}
	l.closed = true
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}
