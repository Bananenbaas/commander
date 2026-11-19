// SPDX-License-Identifier: GPL-2.0-or-later

//go:build amd64

package audio

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// COM plumbing without a COM library: interfaces are called through their
// vtables with syscall.SyscallN, and the callback objects Windows calls are
// Go structs whose first field points to a vtable of syscall.NewCallback
// functions.
//
// Only 64-bit x86 is built: Go's stdcall on amd64 copies the first four
// arguments into XMM0-3 as well, so a float32 passed as its bits in a
// uintptr reaches a method that takes a float (SetMasterVolume). Go's arm64
// stdcall does not do that.

// Interface and class IDs, as published in the Windows SDK headers
// (mmdeviceapi.h, endpointvolume.h, audiopolicy.h, objidl.h).
var (
	clsidMMDeviceEnumerator         = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIUnknown                     = windows.GUID{Data1: 0x00000000, Data2: 0x0000, Data3: 0x0000, Data4: [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIAgileObject                 = windows.GUID{Data1: 0x94EA2B94, Data2: 0xE9CC, Data3: 0x49E0, Data4: [8]byte{0xC0, 0xFF, 0xEE, 0x64, 0xCA, 0x8F, 0x5B, 0x90}}
	iidIMMDeviceEnumerator          = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIMMNotificationClient        = windows.GUID{Data1: 0x7991EEC9, Data2: 0x7E89, Data3: 0x4D85, Data4: [8]byte{0x83, 0x90, 0x6C, 0x70, 0x3C, 0xEC, 0x60, 0xC0}}
	iidIAudioEndpointVolume         = windows.GUID{Data1: 0x5CDF2C82, Data2: 0x841E, Data3: 0x4546, Data4: [8]byte{0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A}}
	iidIAudioEndpointVolumeCallback = windows.GUID{Data1: 0x657804FA, Data2: 0xD6AD, Data3: 0x4496, Data4: [8]byte{0x8A, 0x60, 0x35, 0x27, 0x52, 0xAF, 0x4F, 0x89}}
	iidIAudioSessionManager2        = windows.GUID{Data1: 0x77AA99A0, Data2: 0x1BD6, Data3: 0x484F, Data4: [8]byte{0x8B, 0xC7, 0x2C, 0x65, 0x4C, 0x9A, 0x9B, 0x6F}}
	iidIAudioSessionNotification    = windows.GUID{Data1: 0x641DD20B, Data2: 0x4D41, Data3: 0x49CC, Data4: [8]byte{0xAB, 0xA3, 0x17, 0x4B, 0x94, 0x77, 0xBB, 0x08}}
	iidIAudioSessionControl2        = windows.GUID{Data1: 0xBFB7FF88, Data2: 0x7239, Data3: 0x4FC9, Data4: [8]byte{0x8F, 0xA2, 0x07, 0xC9, 0x50, 0xBE, 0x9C, 0x6D}}
	iidIAudioSessionEvents          = windows.GUID{Data1: 0x24918ACC, Data2: 0x64B3, Data3: 0x37C1, Data4: [8]byte{0x8C, 0xA9, 0x74, 0xA6, 0x6E, 0x99, 0x57, 0xA8}}
	iidISimpleAudioVolume           = windows.GUID{Data1: 0x87CE5498, Data2: 0x68D6, Data3: 0x44E5, Data4: [8]byte{0x92, 0x15, 0x6D, 0xA4, 0x7E, 0xF8, 0x83, 0xD8}}
)

// HRESULT values and constants of the audio API.
const (
	sOK          hresult = 0
	eNoInterface hresult = 0x80004002
	eNotFound    hresult = 0x80070490 // HRESULT_FROM_WIN32(ERROR_NOT_FOUND): no default device

	clsctxAll = 0x17

	eRender  = 0 // EDataFlow
	eConsole = 0 // ERole

	sessionActive  = 1 // AudioSessionState
	sessionExpired = 2
)

// Vtable slots (IUnknown's three come first in every interface).
const (
	mQueryInterface = 0
	mRelease        = 2

	// IMMDeviceEnumerator
	mGetDefaultAudioEndpoint  = 4
	mRegisterEndpointNotify   = 6
	mUnregisterEndpointNotify = 7
	// IMMDevice
	mActivate = 3
	// IAudioEndpointVolume
	mRegisterControlChangeNotify   = 3
	mUnregisterControlChangeNotify = 4
	mSetMasterVolumeLevelScalar    = 7
	mGetMasterVolumeLevelScalar    = 9
	mGetMute                       = 15
	// IAudioSessionManager2
	mGetSessionEnumerator          = 5
	mRegisterSessionNotification   = 6
	mUnregisterSessionNotification = 7
	// IAudioSessionEnumerator
	mGetCount   = 3
	mGetSession = 4
	// IAudioSessionControl / IAudioSessionControl2
	mGetState                           = 3
	mGetDisplayName                     = 4
	mRegisterAudioSessionNotification   = 10
	mUnregisterAudioSessionNotification = 11
	mGetSessionInstanceIdentifier       = 13
	mGetProcessID                       = 14
	mIsSystemSoundsSession              = 15
	// ISimpleAudioVolume
	mSetMasterVolume = 3
	mGetMasterVolume = 4
	mSimpleGetMute   = 6
)

var procCoCreateInstance = windows.NewLazySystemDLL("ole32.dll").NewProc("CoCreateInstance")

// hresult is a COM result code.
type hresult uint32

func (hr hresult) failed() bool { return int32(hr) < 0 }

// err returns nil for a success code, else an error naming the call.
func (hr hresult) err(call string) error {
	if !hr.failed() {
		return nil
	}
	return fmt.Errorf("%s: HRESULT 0x%08X", call, uint32(hr))
}

// comPtr is a COM interface pointer owned by Windows: its first word points
// to the vtable. The array length only bounds the slots that can be named.
type comPtr struct {
	vtbl *[32]uintptr
}

// call calls vtable slot m with the object as the first argument.
//
//go:uintptrescapes
func (p *comPtr) call(m int, args ...uintptr) hresult {
	all := make([]uintptr, 0, 1+len(args))
	all = append(all, uintptr(unsafe.Pointer(p)))
	all = append(all, args...)
	r, _, _ := syscall.SyscallN(p.vtbl[m], all...)
	return hresult(r)
}

// release drops one reference; nil is ignored.
func (p *comPtr) release() {
	if p != nil {
		p.call(mRelease)
	}
}

// query returns the object's interface iid, or an error.
func (p *comPtr) query(iid *windows.GUID) (*comPtr, error) {
	var q *comPtr
	if err := p.call(mQueryInterface, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&q))).err("QueryInterface"); err != nil {
		return nil, err
	}
	return q, nil
}

// text calls a method that returns a CoTaskMem string and frees it; a
// failure gives "".
func (p *comPtr) text(m int) string {
	var s *uint16
	if p.call(m, uintptr(unsafe.Pointer(&s))).failed() || s == nil {
		return ""
	}
	t := windows.UTF16PtrToString(s)
	windows.CoTaskMemFree(unsafe.Pointer(s))
	return t
}

// coCreateInstance creates an in-process object and returns interface iid.
//
//go:uintptrescapes
func coCreateInstance(clsid, iid *windows.GUID) (*comPtr, error) {
	var p *comPtr
	r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxAll, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&p)))
	if err := hresult(r).err("CoCreateInstance"); err != nil {
		return nil, err
	}
	return p, nil
}

// hub is what the callbacks of one engine share. Windows calls them on its
// own threads, so they touch nothing else.
type hub struct {
	signal        func()      // non-blocking
	deviceChanged atomic.Bool // the default output device changed
}

// callback is a COM object implemented in Go. Windows holds its address
// while it is registered; live keeps it reachable until the last Release.
// Go does not move heap objects.
type callback struct {
	vtbl *uintptr // the first slot of a vtable in vtables
	refs atomic.Int32
	iid  *windows.GUID
	hub  *hub
}

// live holds every callback with references, so the collector leaves it
// alone while Windows may call it.
var live sync.Map // *callback → struct{}

// Callback kinds, each with its interface and vtable.
const (
	kindNotificationClient  = iota // IMMNotificationClient
	kindVolumeCallback             // IAudioEndpointVolumeCallback
	kindSessionNotification        // IAudioSessionNotification
	kindSessionEvents              // IAudioSessionEvents
	callbackKinds
)

var (
	vtablesOnce sync.Once
	vtables     [callbackKinds][]uintptr
	kindIIDs    = [callbackKinds]*windows.GUID{
		&iidIMMNotificationClient,
		&iidIAudioEndpointVolumeCallback,
		&iidIAudioSessionNotification,
		&iidIAudioSessionEvents,
	}
)

// Vtable lengths: IUnknown's three and the interface's own methods.
const (
	notificationClientSlots  = 3 + 5
	volumeCallbackSlots      = 3 + 1
	sessionNotificationSlots = 3 + 1
	sessionEventsSlots       = 3 + 7
)

// makeVtables builds the vtables once per process: callbacks made with
// syscall.NewCallback are never freed and their number is limited.
func makeVtables() {
	unknown := []uintptr{
		syscall.NewCallback(cbQueryInterface),
		syscall.NewCallback(cbAddRef),
		syscall.NewCallback(cbRelease),
	}
	ignore2 := syscall.NewCallback(func(this *callback, a uintptr) uintptr { return 0 })
	ignore3 := syscall.NewCallback(func(this *callback, a, b uintptr) uintptr { return 0 })
	ignore5 := syscall.NewCallback(func(this *callback, a, b, c, d uintptr) uintptr { return 0 })
	signal2 := syscall.NewCallback(func(this *callback, a uintptr) uintptr { this.hub.signal(); return 0 })
	signal3 := syscall.NewCallback(func(this *callback, a, b uintptr) uintptr { this.hub.signal(); return 0 })
	signal4 := syscall.NewCallback(func(this *callback, a, b, c uintptr) uintptr { this.hub.signal(); return 0 })

	vtables[kindNotificationClient] = append(slices.Clone(unknown),
		ignore3, // OnDeviceStateChanged(id, state)
		ignore2, // OnDeviceAdded(id)
		ignore2, // OnDeviceRemoved(id)
		syscall.NewCallback(cbDefaultDeviceChanged), // OnDefaultDeviceChanged(flow, role, id)
		ignore3, // OnPropertyValueChanged(id, key): PROPERTYKEY is passed by reference on x64
	)
	vtables[kindVolumeCallback] = append(slices.Clone(unknown),
		signal2, // OnNotify(data)
	)
	vtables[kindSessionNotification] = append(slices.Clone(unknown),
		signal2, // OnSessionCreated(session)
	)
	vtables[kindSessionEvents] = append(slices.Clone(unknown),
		signal3, // OnDisplayNameChanged(name, context)
		ignore3, // OnIconPathChanged(path, context)
		signal4, // OnSimpleVolumeChanged(volume, mute, context); the float is not read
		ignore5, // OnChannelVolumeChanged(count, volumes, channel, context)
		ignore3, // OnGroupingParamChanged(param, context)
		signal2, // OnStateChanged(state)
		signal2, // OnSessionDisconnected(reason)
	)
}

// newCallback returns a callback object of the kind with one reference, the
// caller's.
func newCallback(kind int, h *hub) *callback {
	vtablesOnce.Do(makeVtables)
	c := &callback{vtbl: &vtables[kind][0], iid: kindIIDs[kind], hub: h}
	c.refs.Store(1)
	live.Store(c, struct{}{})
	return c
}

// ptr is the object's address for a COM call.
func (c *callback) ptr() uintptr { return uintptr(unsafe.Pointer(c)) }

// release drops the caller's reference.
func (c *callback) release() {
	if c.refs.Add(-1) == 0 {
		live.Delete(c)
	}
}

func cbQueryInterface(this *callback, riid *windows.GUID, ppv *uintptr) uintptr {
	if ppv == nil {
		return uintptr(0x80004003) // E_POINTER
	}
	if riid != nil && (*riid == iidIUnknown || *riid == iidIAgileObject || *riid == *this.iid) {
		this.refs.Add(1)
		*ppv = this.ptr()
		return uintptr(sOK)
	}
	*ppv = 0
	return uintptr(eNoInterface)
}

func cbAddRef(this *callback) uintptr {
	return uintptr(this.refs.Add(1))
}

func cbRelease(this *callback) uintptr {
	n := this.refs.Add(-1)
	if n == 0 {
		live.Delete(this)
	}
	return uintptr(n)
}

// cbDefaultDeviceChanged notes a new default output device for the engine
// to pick up on its own goroutine.
func cbDefaultDeviceChanged(this *callback, flow, role, id uintptr) uintptr {
	if uint32(flow) == eRender && uint32(role) == eConsole {
		this.hub.deviceChanged.Store(true)
		this.hub.signal()
	}
	return 0
}
