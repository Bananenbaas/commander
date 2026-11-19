// SPDX-License-Identifier: GPL-2.0-or-later

//go:build amd64

package audio

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// winEngine follows the active output devices through the Core Audio API.
// It is used by the audio goroutine only; the callbacks it registers touch
// its hub alone.
type winEngine struct {
	hub  *hub
	logf func(string, ...any)

	enum           *comPtr // IMMDeviceEnumerator
	client         *callback
	clientIsListed bool // client is registered with enum
	built          bool // the devices were listed at least once

	// The default device, for the system volume; nil until acquired or
	// after a failure.
	defID       string
	def         *comPtr // IMMDevice
	vol         *comPtr // IAudioEndpointVolume
	volCB       *callback
	volIsListed bool

	devs   deviceSet // every active output device, for the sessions
	sessCB *callback // shared by every device
	evCB   *callback // shared by every session
}

// winDevice is one output device whose sessions the engine follows.
type winDevice struct {
	dev          *comPtr // IMMDevice
	mgr          *comPtr // IAudioSessionManager2
	sessCB       *callback
	sessIsListed bool
	evCB         *callback
	followed     map[string]*winSession // by session instance identifier
	order        []*winSession          // scratch: the sessions of one read
}

// winSession is a session the engine follows.
type winSession struct {
	ctl    *comPtr // IAudioSessionControl2
	simple *comPtr // ISimpleAudioVolume
	pid    uint32
	exe    string // the program's name from its executable, read once when the session is followed
	listed bool   // evCB is registered with ctl
	seen   bool   // listed by the latest enumeration
}

// openEngine initialises COM (multithreaded) on the calling thread, which
// the caller keeps for the engine, and starts listening for changes of the
// output devices. A missing device is not an error here: it is ErrNoDevice
// from the calls until one appears.
func openEngine(signal func(), logf func(string, ...any)) (engine, error) {
	if err := windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED); err != nil && !errors.Is(err, syscall.Errno(1)) {
		return nil, fmt.Errorf("COM: %w", err)
	}
	enum, err := coCreateInstance(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator)
	if err != nil {
		windows.CoUninitialize()
		return nil, err
	}
	h := &hub{signal: signal}
	e := &winEngine{
		hub:    h,
		logf:   logf,
		enum:   enum,
		client: newCallback(kindNotificationClient, h),
		volCB:  newCallback(kindVolumeCallback, h),
		sessCB: newCallback(kindSessionNotification, h),
		evCB:   newCallback(kindSessionEvents, h),
	}
	if err := enum.call(mRegisterEndpointNotify, e.client.ptr()).err("RegisterEndpointNotificationCallback"); err != nil {
		e.close()
		return nil, err
	}
	e.clientIsListed = true
	if err := e.ready(); err != nil && !errors.Is(err, ErrNoDevice) {
		logf("audio: %v", err)
	}
	return e, nil
}

// ready makes sure the engine follows the current default device and every
// active output device. It lists them again after Windows reported a change
// of the devices, and while there is no default device.
func (e *winEngine) ready() error {
	stale := e.hub.deviceChanged.Swap(false)
	if !stale && e.built && e.vol != nil {
		return nil
	}
	if stale && e.built {
		e.logf("audio: the output devices changed")
	}
	if err := e.updateDevices(); err != nil {
		e.hub.deviceChanged.Store(true) // try again on the next call
		return err
	}
	e.built = true
	return e.acquireDefault()
}

// updateDevices lists the active output devices and follows exactly those,
// keeping the ones already followed. A device that cannot be followed is
// logged and left out.
func (e *winEngine) updateDevices() error {
	var coll *comPtr
	if err := e.enum.call(mEnumAudioEndpoints, eRender, deviceStateActive, uintptr(unsafe.Pointer(&coll))).err("EnumAudioEndpoints"); err != nil {
		return err
	}
	defer coll.release()
	var n uint32
	if err := coll.call(mCollectionGetCount, uintptr(unsafe.Pointer(&n))).err("IMMDeviceCollection.GetCount"); err != nil {
		return err
	}
	found := map[string]*comPtr{}
	var ids []string
	for i := range n {
		var dev *comPtr
		if coll.call(mCollectionItem, uintptr(i), uintptr(unsafe.Pointer(&dev))).failed() {
			continue
		}
		id := dev.text(mGetId)
		if id == "" || found[id] != nil {
			dev.release()
			continue
		}
		found[id] = dev
		ids = append(ids, id)
	}
	err := e.devs.update(ids, func(id string) (endpoint, error) {
		dev := found[id]
		found[id] = nil // openDevice owns it now
		return openDevice(dev, e.sessCB, e.evCB)
	})
	for _, dev := range found {
		dev.release() // devices already followed: their own reference stays
	}
	if err != nil {
		e.logf("audio: %v", err)
	}
	return nil
}

// acquireDefault takes the current default output device for the system
// volume, unless it is the one already held.
func (e *winEngine) acquireDefault() error {
	var dev *comPtr
	hr := e.enum.call(mGetDefaultAudioEndpoint, eRender, eConsole, uintptr(unsafe.Pointer(&dev)))
	if hr == eNotFound {
		e.dropDefault()
		return ErrNoDevice
	}
	if err := hr.err("GetDefaultAudioEndpoint"); err != nil {
		e.dropDefault()
		return err
	}
	id := dev.text(mGetId)
	if e.vol != nil && id != "" && id == e.defID {
		dev.release()
		return nil
	}
	e.dropDefault()
	e.def, e.defID = dev, id
	var vol *comPtr
	if err := dev.call(mActivate, uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)), clsctxAll, 0, uintptr(unsafe.Pointer(&vol))).err("activating the endpoint volume"); err != nil {
		e.dropDefault()
		return err
	}
	e.vol = vol
	if err := vol.call(mRegisterControlChangeNotify, e.volCB.ptr()).err("RegisterControlChangeNotify"); err != nil {
		e.dropDefault()
		return err
	}
	e.volIsListed = true
	return nil
}

// dropDefault unregisters from the default device's volume and releases it.
func (e *winEngine) dropDefault() {
	if e.volIsListed {
		e.vol.call(mUnregisterControlChangeNotify, e.volCB.ptr())
		e.volIsListed = false
	}
	e.vol.release()
	e.vol = nil
	e.def.release()
	e.def, e.defID = nil, ""
}

// openDevice follows the sessions of the device dev and takes over its
// reference: on an error dev is released.
func openDevice(dev *comPtr, sessCB, evCB *callback) (endpoint, error) {
	d := &winDevice{dev: dev, sessCB: sessCB, evCB: evCB, followed: map[string]*winSession{}}
	var mgr *comPtr
	if err := dev.call(mActivate, uintptr(unsafe.Pointer(&iidIAudioSessionManager2)), clsctxAll, 0, uintptr(unsafe.Pointer(&mgr))).err("activating the session manager"); err != nil {
		d.close()
		return nil, err
	}
	d.mgr = mgr
	// Windows delivers new-session notifications only after the session
	// enumerator was asked for once.
	var en *comPtr
	if err := mgr.call(mGetSessionEnumerator, uintptr(unsafe.Pointer(&en))).err("GetSessionEnumerator"); err != nil {
		d.close()
		return nil, err
	}
	en.release()
	if err := mgr.call(mRegisterSessionNotification, sessCB.ptr()).err("RegisterSessionNotification"); err != nil {
		d.close()
		return nil, err
	}
	d.sessIsListed = true
	return d, nil
}

// close unregisters from the device and releases it and its sessions.
func (d *winDevice) close() {
	for id, s := range d.followed {
		d.untrack(s)
		delete(d.followed, id)
	}
	if d.sessIsListed {
		d.mgr.call(mUnregisterSessionNotification, d.sessCB.ptr())
		d.sessIsListed = false
	}
	d.mgr.release()
	d.mgr = nil
	d.dev.release()
	d.dev = nil
}

func (e *winEngine) sessions() (Session, []Session, error) {
	if err := e.ready(); err != nil {
		return Session{}, nil, err
	}
	system, err := e.system()
	if err != nil {
		// The device may be gone (unplugged, disabled): take it again next time.
		e.dropDefault()
		return Session{}, nil, err
	}
	raw, lost := e.devs.read(e.logf)
	if lost {
		e.hub.deviceChanged.Store(true)
	}
	return system, merge(raw), nil
}

func (e *winEngine) devices() int { return e.devs.len() }

// system reads the default device's volume and mute.
func (e *winEngine) system() (Session, error) {
	var f float32
	if err := e.vol.call(mGetMasterVolumeLevelScalar, uintptr(unsafe.Pointer(&f))).err("GetMasterVolumeLevelScalar"); err != nil {
		return Session{}, err
	}
	var muted int32
	if err := e.vol.call(mGetMute, uintptr(unsafe.Pointer(&muted))).err("GetMute"); err != nil {
		return Session{}, err
	}
	return Session{ID: "system", Name: "SystemVolume", Level: levelOf(f), Muted: muted != 0}, nil
}

// read enumerates the device's sessions: it follows new ones, lets go of
// those gone or expired, and returns the programs' sessions in the
// enumerator's order. Inactive sessions stay, a paused player is listed, but
// only active ones are marked Active.
func (d *winDevice) read() ([]rawSession, error) {
	var en *comPtr
	if err := d.mgr.call(mGetSessionEnumerator, uintptr(unsafe.Pointer(&en))).err("GetSessionEnumerator"); err != nil {
		return nil, err
	}
	defer en.release()
	var n int32
	if err := en.call(mGetCount, uintptr(unsafe.Pointer(&n))).err("IAudioSessionEnumerator.GetCount"); err != nil {
		return nil, err
	}
	for _, s := range d.followed {
		s.seen = false
	}
	d.order = d.order[:0]
	for i := range n {
		var c *comPtr
		if !en.call(mGetSession, uintptr(i), uintptr(unsafe.Pointer(&c))).failed() {
			if s := d.track(c); s != nil && !s.seen {
				s.seen = true
				d.order = append(d.order, s)
			}
			c.release()
		}
	}
	for id, s := range d.followed {
		if !s.seen {
			d.untrack(s)
			delete(d.followed, id)
		}
	}
	raw := make([]rawSession, 0, len(d.order))
	for _, s := range d.order {
		var f float32
		if s.simple.call(mGetMasterVolume, uintptr(unsafe.Pointer(&f))).failed() {
			continue
		}
		var muted int32
		s.simple.call(mSimpleGetMute, uintptr(unsafe.Pointer(&muted)))
		var state int32
		active := !s.ctl.call(mGetState, uintptr(unsafe.Pointer(&state))).failed() && state == sessionActive
		raw = append(raw, rawSession{PID: s.pid, Display: s.ctl.text(mGetDisplayName), Exe: s.exe, Level: levelOf(f), Muted: muted != 0, Active: active})
	}
	return raw, nil
}

// track returns the followed session for the control c, following it from
// now on if it is new; nil for the system sounds, expired sessions and
// sessions that cannot be read.
func (d *winDevice) track(c *comPtr) *winSession {
	ctl, err := c.query(&iidIAudioSessionControl2)
	if err != nil {
		return nil
	}
	var state int32
	if ctl.call(mGetState, uintptr(unsafe.Pointer(&state))).failed() || state == sessionExpired {
		ctl.release()
		return nil
	}
	if ctl.call(mIsSystemSoundsSession) == sOK {
		ctl.release()
		return nil
	}
	id := ctl.text(mGetSessionInstanceIdentifier)
	if id == "" {
		ctl.release()
		return nil
	}
	if s, ok := d.followed[id]; ok {
		ctl.release()
		return s
	}
	simple, err := ctl.query(&iidISimpleAudioVolume)
	if err != nil {
		ctl.release()
		return nil
	}
	s := &winSession{ctl: ctl, simple: simple}
	// For a session of several processes Windows answers with a success
	// code other than S_OK and a process ID that still names one of them.
	if ctl.call(mGetProcessID, uintptr(unsafe.Pointer(&s.pid))).failed() {
		s.pid = 0
	}
	s.exe = processName(s.pid)
	s.listed = !ctl.call(mRegisterAudioSessionNotification, d.evCB.ptr()).failed()
	d.followed[id] = s
	return s
}

// untrack unregisters from a session and releases it.
func (d *winDevice) untrack(s *winSession) {
	if s.listed {
		s.ctl.call(mUnregisterAudioSessionNotification, d.evCB.ptr())
		s.listed = false
	}
	s.simple.release()
	s.ctl.release()
}

// setProgram sets every followed session of the process on the device.
func (d *winDevice) setProgram(pid uint32, level int) (bool, error) {
	found := false
	for _, s := range d.followed {
		if s.pid != pid {
			continue
		}
		found = true
		if err := s.simple.call(mSetMasterVolume, scalar(level), 0).err("ISimpleAudioVolume.SetMasterVolume"); err != nil {
			return true, err
		}
	}
	return found, nil
}

func (e *winEngine) setSystem(level int) error {
	if err := e.ready(); err != nil {
		return err
	}
	return e.vol.call(mSetMasterVolumeLevelScalar, scalar(level), 0).err("SetMasterVolumeLevelScalar")
}

func (e *winEngine) setProgram(id string, level int) error {
	pid, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return fmt.Errorf("no program with ID %q", id)
	}
	if err := e.ready(); err != nil {
		return err
	}
	found, err := e.devs.setProgram(uint32(pid), level)
	if !found && err == nil {
		// The devices were listed again since the last read: read once more.
		if _, _, err := e.sessions(); err != nil {
			return err
		}
		found, err = e.devs.setProgram(uint32(pid), level)
	}
	if err == nil && !found {
		err = fmt.Errorf("no program with ID %q", id)
	}
	return err
}

// scalar turns a level 0 to 100 into a float argument: its bits in the
// integer register, which Go's stdcall copies to the XMM register too.
func scalar(level int) uintptr {
	return uintptr(math.Float32bits(float32(level) / 100))
}

func (e *winEngine) close() {
	e.devs.closeAll()
	e.dropDefault()
	if e.clientIsListed {
		e.enum.call(mUnregisterEndpointNotify, e.client.ptr())
		e.clientIsListed = false
	}
	e.enum.release()
	e.enum = nil
	for _, c := range []*callback{e.client, e.volCB, e.sessCB, e.evCB} {
		c.release()
	}
	windows.CoUninitialize()
}

// processName names a process after its executable (see programName), or
// "" when its executable cannot be read.
func processName(pid uint32) string {
	path := processPath(pid)
	return programName(path, versionInfo(path))
}

// processPath returns the full path of a process's executable, or "" when it
// cannot be read.
func processPath(pid uint32) string {
	if pid == 0 {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

// versionInfo reads the version information of an executable once and
// returns a lookup of its strings, such as "FileDescription", in the first
// string block that has the string (see stringBlocks); "" when it has none.
func versionInfo(path string) func(key string) string {
	none := func(string) string { return "" }
	if path == "" {
		return none
	}
	size, err := windows.GetFileVersionInfoSize(path, nil)
	if err != nil || size == 0 {
		return none
	}
	buf := make([]byte, size)
	block := unsafe.Pointer(&buf[0])
	if err := windows.GetFileVersionInfo(path, 0, size, block); err != nil {
		return none
	}
	var trans []uint16
	var p unsafe.Pointer
	var n uint32 // bytes
	if windows.VerQueryValue(block, `\VarFileInfo\Translation`, unsafe.Pointer(&p), &n) == nil && p != nil && n >= 4 {
		trans = slices.Clone(unsafe.Slice((*uint16)(p), n/2))
	}
	blocks := stringBlocks(trans)
	return func(key string) string {
		for _, b := range blocks {
			var p unsafe.Pointer
			var n uint32 // characters, with the terminating NUL
			if windows.VerQueryValue(block, b+key, unsafe.Pointer(&p), &n) != nil || p == nil || n == 0 {
				continue
			}
			s := windows.UTF16ToString(unsafe.Slice((*uint16)(p), n))
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
		return ""
	}
}
