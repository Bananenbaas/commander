// SPDX-License-Identifier: GPL-2.0-or-later

// Package control answers the calls the window makes over the bridge and
// turns what happens in Commander into the events it sends back. It holds no
// window code and no Windows code: Commander hands it the functions it needs.
package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Bananenbaas/commander/screen/internal/audio"
	"github.com/Bananenbaas/commander/screen/internal/bridge"
	"github.com/Bananenbaas/commander/screen/internal/layout"
	"github.com/Bananenbaas/commander/screen/internal/procs"
	"github.com/Bananenbaas/commander/screen/internal/run"
	"github.com/Bananenbaas/commander/screen/internal/supervisor"
)

// replugText is the status while the keyboard does not answer.
const replugText = "the keyboard does not answer, unplug and replug it"

// eventBuffer is the number of events that wait for the window.
const eventBuffer = 16

// Deps is what Commander supplies. All fields are called from the bridge's
// goroutines, also concurrently.
type Deps struct {
	Version         string
	Dir             string
	Load            func(dir string) (*layout.Config, error)
	Autostart       func() (bool, error) // is Start with Windows on
	SetAutostart    func(on bool) error
	CheckUpdates    func() (bool, error)
	SetCheckUpdates func(on bool) error // switching on also checks at once (as the tray does)
	Theme           func() (string, error)
	SetTheme        func(string) error
	CheckNow        func(ctx context.Context) error   // the manual check (shows its balloon); nil = it checked, an error = it failed or was skipped
	Offered         func() (tag, url string, ok bool) // the update on offer, if any
	Install         func(ctx context.Context)         // installs the offered update after the confirmation box
	Open            func(path string) error           // opens a folder or file with its default program
	Reload          func()                            // asks the supervisor to reload the configuration; nil does nothing
	Apply           func()                            // asks the supervisor to send the buttons again; nil does nothing
	LogPath         string
	Logf            func(format string, args ...any) // nil discards

	// Mixer reads the system volume and the programs with sound; ok is
	// false when there is no audio now. nil: there is no mixer.
	Mixer func() (system audio.Session, programs []audio.Session, ok bool, err error)
	// Running lists every running process; nil: none.
	Running func() ([]procs.Proc, error)
}

// Service answers the window's calls and queues its events.
type Service struct {
	d Deps

	mu     sync.Mutex // guards st, pending and closed; never held across Load or a send
	st     supervisor.State
	closed bool
	// pending is the newest State the worker has not turned into an event
	// yet, valid while hasPending.
	pending    supervisor.State
	hasPending bool

	wake    chan struct{} // capacity 1: SetState signals the worker
	done    chan struct{} // closed by Close
	stopped chan struct{} // closed when the worker has returned

	// qmu serializes pushes to events. It cannot block: under it, a send
	// is only tried without waiting, and a full queue is made room in
	// by receiving without waiting.
	qmu    sync.Mutex
	events chan bridge.Message
}

// New returns a Service over d and starts its worker; Close stops it.
func New(d Deps) *Service {
	s := &Service{
		d:       d,
		st:      supervisor.State{Run: run.Status{Profile: -1}},
		events:  make(chan bridge.Message, eventBuffer),
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go s.worker()
	return s
}

// Close stops the worker; later SetState and Show calls do nothing. It does
// not wait for a Load that is in progress. It may be called more than once.
func (s *Service) Close() {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		close(s.done)
	}
	s.mu.Unlock()
}

// Events is the channel Serve writes to the window.
func (s *Service) Events() chan bridge.Message { return s.events }

func (s *Service) logf(format string, args ...any) {
	if s.d.Logf != nil {
		s.d.Logf(format, args...)
	}
}

// SetState records the supervisor state and has the worker queue a "state"
// event for the window. It returns at once, whatever Load does: only the
// latest state is kept for the worker, so a burst of states becomes one event.
func (s *Service) SetState(st supervisor.State) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.st = st
	s.pending, s.hasPending = st, true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default: // the worker is already signalled
	}
}

func (s *Service) worker() {
	defer close(s.stopped)
	for {
		select {
		case <-s.done:
			return
		case <-s.wake:
		}
		s.mu.Lock()
		st, ok := s.pending, s.hasPending
		s.hasPending = false
		s.mu.Unlock()
		if !ok {
			continue
		}
		data, err := json.Marshal(s.state(st))
		if err != nil {
			s.logf("control: state event: %v", err)
			continue
		}
		select {
		case <-s.done:
			return
		default:
		}
		s.push(bridge.Message{Event: "state", Data: data})
	}
}

// Show queues a "show" event: bring the window to the front.
func (s *Service) Show() {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if !closed {
		s.push(bridge.Message{Event: "show"})
	}
}

// push queues m; when the queue is full the oldest event is dropped.
func (s *Service) push(m bridge.Message) {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	for {
		select {
		case s.events <- m:
			return
		default:
		}
		select {
		case <-s.events:
		default:
		}
	}
}

// DropEvents empties the event queue. Commander calls it when a new window
// connects: what was queued while no window was there (a "show" among it)
// is stale, and the new page asks for the state itself.
func (s *Service) DropEvents() {
	s.qmu.Lock()
	defer s.qmu.Unlock()
	for {
		select {
		case <-s.events:
		default:
			return
		}
	}
}

// StatusText is what Commander is doing, in words, for the tray tooltip and
// the window.
func StatusText(st supervisor.State) string {
	switch st.Phase {
	case supervisor.Starting:
		return "starting"
	case supervisor.Running:
		switch {
		case st.Run.Waiting == "keyboard":
			return "waiting for the keyboard"
		case st.Run.Waiting == "Swarm II":
			return "waiting for Swarm II to close"
		case st.Run.Waiting == run.WaitReplug:
			return replugText
		case st.Run.Waiting != "":
			return "waiting for " + st.Run.Waiting
		case st.Run.Name != "":
			return st.Run.Name
		case st.Run.Profile >= 0:
			return fmt.Sprintf("profile %d", st.Run.Profile)
		}
		return "starting"
	case supervisor.Applying:
		return "applying buttons…"
	case supervisor.Failed:
		what := "error"
		if st.Err != "" {
			what += ": " + st.Err
		}
		return what
	case supervisor.Stopped:
		return "stopped"
	}
	return st.Phase.String()
}

type keyboardInfo struct {
	Connected bool   `json:"connected"`
	Waiting   string `json:"waiting"`
	Profile   int    `json:"profile"`
	Name      string `json:"name"`
}

type profileInfo struct {
	Number  int    `json:"number"`
	Name    string `json:"name"`
	File    string `json:"file"`
	Buttons int    `json:"buttons"`
	Lit     bool   `json:"lit"`
}

type gameInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Program string `json:"program"`
	KB7     *int   `json:"kb7"`
	File    string `json:"file"`
}

type desktopInfo struct {
	Name string `json:"name"`
	KB7  int    `json:"kb7"`
}

type stateInfo struct {
	Version   string        `json:"version"`
	Phase     string        `json:"phase"`
	Status    string        `json:"status"`
	Failed    bool          `json:"failed"`
	Error     string        `json:"error"`
	Keyboard  keyboardInfo  `json:"keyboard"`
	Folder    string        `json:"folder"`
	LoadError string        `json:"loadError"`
	Profiles  []profileInfo `json:"profiles"`
	Games     []gameInfo    `json:"games"`
	Desktop   desktopInfo   `json:"desktop"`
}

func (s *Service) state(st supervisor.State) stateInfo {
	out := stateInfo{
		Version: s.d.Version,
		Phase:   st.Phase.String(),
		Status:  StatusText(st),
		Failed:  st.Phase == supervisor.Failed,
		Error:   st.Err,
		Keyboard: keyboardInfo{
			Connected: st.Phase == supervisor.Running && st.Run.Waiting == "",
			Waiting:   st.Run.Waiting,
			Profile:   st.Run.Profile,
			Name:      st.Run.Name,
		},
		Folder:   s.d.Dir,
		Profiles: []profileInfo{},
		Games:    []gameInfo{},
		Desktop:  desktopInfo{Name: "Desktop"},
	}
	cfg, err := s.d.Load(s.d.Dir)
	if err != nil {
		out.LoadError = err.Error()
		return out
	}
	for _, p := range cfg.Profiles {
		out.Profiles = append(out.Profiles, profileInfo{
			Number: int(p.Number), Name: p.Name, File: p.File,
			Buttons: len(p.Buttons), Lit: p.Light != nil,
		})
	}
	for _, g := range cfg.Games {
		gi := gameInfo{ID: gameID(cfg, g), Name: g.Name, Program: g.Program, File: g.File}
		if g.KB7 != nil {
			n := int(*g.KB7)
			gi.KB7 = &n
		}
		out.Games = append(out.Games, gi)
	}
	out.Desktop.KB7 = int(cfg.DesktopProfile())
	if cfg.Desktop != nil && cfg.Desktop.Name != "" {
		out.Desktop.Name = cfg.Desktop.Name
	}
	return out
}

type settingsInfo struct {
	StartWithWindows bool   `json:"startWithWindows"`
	CheckUpdates     bool   `json:"checkUpdates"`
	Theme            string `json:"theme"`
	Folder           string `json:"folder"`
	LogPath          string `json:"logPath"`
}

type updateInfo struct {
	Current   string `json:"current"`
	Available string `json:"available"`
	URL       string `json:"url"`
}

func validTheme(t string) bool { return t == "system" || t == "light" || t == "dark" }

func (s *Service) settings() (settingsInfo, error) {
	auto, err := s.d.Autostart()
	if err != nil {
		return settingsInfo{}, fmt.Errorf("settings: %w", err)
	}
	upd, err := s.d.CheckUpdates()
	if err != nil {
		return settingsInfo{}, fmt.Errorf("settings: %w", err)
	}
	theme, err := s.d.Theme()
	if err != nil {
		return settingsInfo{}, fmt.Errorf("settings: %w", err)
	}
	if !validTheme(theme) {
		theme = "system"
	}
	return settingsInfo{StartWithWindows: auto, CheckUpdates: upd, Theme: theme, Folder: s.d.Dir, LogPath: s.d.LogPath}, nil
}

func (s *Service) update() updateInfo {
	u := updateInfo{Current: s.d.Version}
	if tag, url, ok := s.d.Offered(); ok {
		u.Available, u.URL = tag, url
	}
	return u
}

// noArgs reports whether args hold nothing but, at most, an empty object.
func noArgs(args json.RawMessage) bool {
	a := bytes.TrimSpace(args)
	return len(a) == 0 || string(a) == "null" || string(a) == "{}"
}

// Handle answers one call; it is a bridge.Handler.
func (s *Service) Handle(ctx context.Context, call string, args json.RawMessage) (any, error) {
	switch call {
	case "state":
		s.mu.Lock()
		st := s.st
		s.mu.Unlock()
		return s.state(st), nil
	case "settings":
		return s.settings()
	case "saveSettings":
		var a struct {
			StartWithWindows *bool   `json:"startWithWindows"`
			CheckUpdates     *bool   `json:"checkUpdates"`
			Theme            *string `json:"theme"`
		}
		if !noArgs(args) {
			if err := json.Unmarshal(args, &a); err != nil {
				return nil, fmt.Errorf("saveSettings: bad arguments: %w", err)
			}
		}
		if a.Theme != nil && !validTheme(*a.Theme) {
			return nil, fmt.Errorf("saveSettings: unknown theme %q", *a.Theme)
		}
		if a.StartWithWindows != nil {
			if err := s.d.SetAutostart(*a.StartWithWindows); err != nil {
				return nil, fmt.Errorf("saveSettings: %w", err)
			}
		}
		if a.CheckUpdates != nil {
			if err := s.d.SetCheckUpdates(*a.CheckUpdates); err != nil {
				return nil, fmt.Errorf("saveSettings: %w", err)
			}
		}
		if a.Theme != nil {
			if err := s.d.SetTheme(*a.Theme); err != nil {
				return nil, fmt.Errorf("saveSettings: %w", err)
			}
		}
		return s.settings()
	case "update":
		return s.update(), nil
	case "checkUpdate":
		if err := s.d.CheckNow(ctx); err != nil {
			return nil, fmt.Errorf("checkUpdate: %w", err)
		}
		return s.update(), nil
	case "installUpdate":
		s.d.Install(ctx)
		return s.update(), nil
	case "deviceProfile":
		return s.deviceProfile(args)
	case "saveDeviceProfile":
		return s.saveDeviceProfile(args)
	case "renderButton":
		return s.renderButton(args)
	case "importPicture":
		return s.importPicture(args)
	case "lightingInfo":
		return lightingInfo(), nil
	case "deviceLight":
		return s.deviceLight(args)
	case "saveDeviceLight":
		return s.saveDeviceLight(args)
	case "lightFrames":
		return s.lightFrames(args)
	case "gameProfile":
		return s.gameProfile(args)
	case "saveGameProfile":
		return s.saveGameProfile(args)
	case "deleteGameProfile":
		return s.deleteGameProfile(args)
	case "runningPrograms":
		return s.runningPrograms()
	case "mixer":
		return s.mixer(), nil
	case "profileInfo":
		return s.profileInfo(args)
	case "saveProfileName":
		return s.saveProfileName(args)
	case "duplicateProfile":
		return s.duplicateProfile(args)
	case "deleteProfile":
		return s.deleteProfile(args)
	case "openFolder":
		if err := s.d.Open(s.d.Dir); err != nil {
			return nil, fmt.Errorf("openFolder: %w", err)
		}
		return nil, nil
	case "openLog":
		if err := s.d.Open(s.d.LogPath); err != nil {
			return nil, fmt.Errorf("openLog: %w", err)
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unknown call %q", call)
}
