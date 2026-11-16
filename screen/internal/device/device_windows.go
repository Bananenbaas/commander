package device

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modHID      = windows.NewLazySystemDLL("hid.dll")
	modSetupAPI = windows.NewLazySystemDLL("setupapi.dll")

	procHidDGetHidGuid        = modHID.NewProc("HidD_GetHidGuid")
	procHidDGetAttributes     = modHID.NewProc("HidD_GetAttributes")
	procHidDGetPreparsedData  = modHID.NewProc("HidD_GetPreparsedData")
	procHidDFreePreparsedData = modHID.NewProc("HidD_FreePreparsedData")
	procHidPGetCaps           = modHID.NewProc("HidP_GetCaps")
	procHidDSetFeature        = modHID.NewProc("HidD_SetFeature")
	procHidDGetFeature        = modHID.NewProc("HidD_GetFeature")

	procSetupDiGetClassDevsW             = modSetupAPI.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces      = modSetupAPI.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetailW = modSetupAPI.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList     = modSetupAPI.NewProc("SetupDiDestroyDeviceInfoList")
)

const (
	digcfPresent         = 0x02
	digcfDeviceInterface = 0x10
	hidpStatusSuccess    = 0x00110000

	// streamWriteTimeout bounds a stream write the keyboard does not take.
	streamWriteTimeout = 5 * time.Second
	// maxWaitMillis is the longest finite wait; INFINITE is one more.
	maxWaitMillis = windows.INFINITE - 1
)

// SP_DEVICE_INTERFACE_DATA.
type spDeviceInterfaceData struct {
	size     uint32
	class    windows.GUID
	flags    uint32
	reserved uintptr
}

// HIDD_ATTRIBUTES.
type hiddAttributes struct {
	size      uint32
	vendorID  uint16
	productID uint16
	version   uint16
}

// HIDP_CAPS.
type hidpCaps struct {
	usage, usagePage                uint16
	inputLen, outputLen, featureLen uint16
	reserved                        [17]uint16
	counts                          [10]uint16
}

// Open finds the KB7 and opens its stream, feature, notification and touch
// collections.
func Open() (Device, error) {
	// A missing function would make LazyProc.Call panic.
	for _, p := range []*windows.LazyProc{
		procHidDGetHidGuid, procHidDGetAttributes, procHidDGetPreparsedData, procHidDFreePreparsedData,
		procHidPGetCaps, procHidDSetFeature, procHidDGetFeature,
		procSetupDiGetClassDevsW, procSetupDiEnumDeviceInterfaces, procSetupDiGetDeviceInterfaceDetailW, procSetupDiDestroyDeviceInfoList,
	} {
		if err := p.Find(); err != nil {
			return nil, fmt.Errorf("device: %w", err)
		}
	}
	paths, err := hidPaths()
	if err != nil {
		return nil, fmt.Errorf("device: list HID devices: %w", err)
	}
	var found []string
	var cs []collection
	for _, p := range paths {
		if c, ok := describe(p); ok {
			found = append(found, p)
			cs = append(cs, c)
		}
	}
	idx, err := pickCollections(cs)
	if err != nil {
		return nil, err
	}
	var hs [numRoles]windows.Handle
	for r, i := range idx {
		h, err := openRW(found[i])
		if err != nil {
			for _, o := range hs[:r] {
				windows.CloseHandle(o)
			}
			return nil, fmt.Errorf("device: open %s collection: %w", roleNames[r], err)
		}
		hs[r] = h
	}
	return newWinDevice(hs[roleStream], hs[roleFeature], cs[idx[roleFeature]].FeatureLen,
		inputCollection{hs[roleNotify], cs[idx[roleNotify]].InputLen},
		inputCollection{hs[roleTouch], cs[idx[roleTouch]].InputLen})
}

// hidPaths lists the device paths of every present HID collection.
func hidPaths() ([]string, error) {
	var guid windows.GUID
	procHidDGetHidGuid.Call(uintptr(unsafe.Pointer(&guid)))
	set, _, e := procSetupDiGetClassDevsW.Call(uintptr(unsafe.Pointer(&guid)), 0, 0, digcfPresent|digcfDeviceInterface)
	if windows.Handle(set) == windows.InvalidHandle {
		return nil, callErr(false, e, "SetupDiGetClassDevsW")
	}
	defer procSetupDiDestroyDeviceInfoList.Call(set)

	// SP_DEVICE_INTERFACE_DETAIL_DATA_W is packed to 1 byte on 32-bit
	// Windows and to 8 bytes on 64-bit Windows; DevicePath starts at 4.
	detailSize := uint32(8)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		detailSize = 6
	}
	var paths []string
	for i := 0; ; i++ {
		data := spDeviceInterfaceData{size: uint32(unsafe.Sizeof(spDeviceInterfaceData{}))}
		r, _, e := procSetupDiEnumDeviceInterfaces.Call(set, 0, uintptr(unsafe.Pointer(&guid)), uintptr(i), uintptr(unsafe.Pointer(&data)))
		if uint32(r) == 0 {
			if errors.Is(e, windows.ERROR_NO_MORE_ITEMS) {
				return paths, nil
			}
			return nil, callErr(false, e, "SetupDiEnumDeviceInterfaces")
		}
		var need uint32
		procSetupDiGetDeviceInterfaceDetailW.Call(set, uintptr(unsafe.Pointer(&data)), 0, 0, uintptr(unsafe.Pointer(&need)), 0)
		if need <= 4 {
			continue
		}
		buf := make([]uint32, (need+3)/4) // 4-byte aligned
		buf[0] = detailSize
		r, _, _ = procSetupDiGetDeviceInterfaceDetailW.Call(set, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*4), 0, 0)
		if uint32(r) == 0 {
			continue
		}
		paths = append(paths, windows.UTF16PtrToString((*uint16)(unsafe.Pointer(&buf[1]))))
	}
}

// describe reports whether path is a collection of a KB7, and how it is
// laid out. It opens the path without read or write access, which is enough
// for HidD_GetAttributes and HidP_GetCaps and leaves every collection
// unread, the keyboard ones included.
func describe(path string) (collection, bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return collection{}, false
	}
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return collection{}, false
	}
	defer windows.CloseHandle(h)

	attrs := hiddAttributes{size: uint32(unsafe.Sizeof(hiddAttributes{}))}
	r, _, _ := procHidDGetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attrs)))
	if byte(r) == 0 || attrs.vendorID != VendorID || attrs.productID != ProductID {
		return collection{}, false
	}
	var pp uintptr
	r, _, _ = procHidDGetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&pp)))
	if byte(r) == 0 {
		return collection{}, false
	}
	defer procHidDFreePreparsedData.Call(pp)
	var caps hidpCaps
	r, _, _ = procHidPGetCaps.Call(pp, uintptr(unsafe.Pointer(&caps)))
	if uint32(r) != hidpStatusSuccess {
		return collection{}, false
	}
	return collection{
		UsagePage:  caps.usagePage,
		Usage:      caps.usage,
		InputLen:   int(caps.inputLen),
		OutputLen:  int(caps.outputLen),
		FeatureLen: int(caps.featureLen),
	}, true
}

// openRW opens a collection for reading and writing with overlapped I/O.
func openRW(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
}

// callErr turns the result of a Windows call into an error: nil when ok,
// else the call's last error, or a generic one if Windows set none.
func callErr(ok bool, e error, name string) error {
	if ok {
		return nil
	}
	var errno syscall.Errno
	if errors.As(e, &errno) && errno != 0 {
		return errno
	}
	return errors.New(name + " failed")
}

func hidSetFeature(h windows.Handle, buf []byte) error {
	r, _, e := procHidDSetFeature.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return callErr(byte(r) != 0, e, "HidD_SetFeature")
}

func hidGetFeature(h windows.Handle, buf []byte) error {
	r, _, e := procHidDGetFeature.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return callErr(byte(r) != 0, e, "HidD_GetFeature")
}

// errStopped means Close interrupted an operation.
var errStopped = errors.New("device: stopped")

// gone reports whether a Windows error means the keyboard went away:
//
//   - ERROR_DEVICE_NOT_CONNECTED and ERROR_DEVICE_REMOVED say so;
//   - ERROR_BAD_COMMAND is STATUS_INVALID_DEVICE_STATE, what HID requests
//     on a surprise-removed device fail with;
//   - ERROR_FILE_NOT_FOUND is STATUS_NO_SUCH_DEVICE;
//   - ERROR_GEN_FAILURE counts only for reads and writes (transfer): a
//     feature request can also get it from a live keyboard that refuses
//     the report.
//
// ERROR_INVALID_HANDLE is left out: the handles stay valid until Close, so
// it would mean a fault here, not an unplugged keyboard.
func gone(err error, transfer bool) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case windows.ERROR_DEVICE_NOT_CONNECTED, windows.ERROR_DEVICE_REMOVED,
		windows.ERROR_BAD_COMMAND, windows.ERROR_FILE_NOT_FOUND:
		return true
	case windows.ERROR_GEN_FAILURE:
		return transfer
	}
	return false
}

// ioErr names the operation that failed and sorts the error into
// ErrTimeout, ErrClosed, ErrDisconnected or anything else.
func ioErr(op string, err error, transfer bool) error {
	switch {
	case errors.Is(err, ErrTimeout):
		return ErrTimeout
	case errors.Is(err, errStopped):
		return ErrClosed
	case gone(err, transfer):
		return disconnected(op, err)
	}
	return fmt.Errorf("device: %s: %w", op, err)
}

// overlapped is one overlapped operation's state. It lives on the heap and
// stays put while Windows holds a pointer to it.
type overlapped struct {
	ov  windows.Overlapped
	buf []byte
	n   uint32
}

func newOverlapped(size int) (*overlapped, error) {
	ev, err := windows.CreateEvent(nil, 1, 0, nil) // manual reset, not signalled
	if err != nil {
		return nil, err
	}
	return &overlapped{ov: windows.Overlapped{HEvent: ev}, buf: make([]byte, size)}, nil
}

func (o *overlapped) free() {
	windows.CloseHandle(o.ov.HEvent)
}

// do reads into o.buf, or writes it, on h, waiting at most ms milliseconds
// (windows.INFINITE: no limit) or until stop is signalled. An operation that
// does not finish in time is cancelled, and do waits for the cancellation,
// so o can be used again; data that arrived before the cancellation took
// effect is returned rather than lost. It returns the number of bytes
// moved, or ErrTimeout, or errStopped.
func (o *overlapped) do(h windows.Handle, write bool, ms uint32, stop windows.Handle) (int, error) {
	// CancelIo, the fallback for CancelIoEx, cancels only the I/O started
	// by the calling thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	o.ov = windows.Overlapped{HEvent: o.ov.HEvent}
	var err error
	if write {
		err = windows.WriteFile(h, o.buf, &o.n, &o.ov)
	} else {
		err = windows.ReadFile(h, o.buf, &o.n, &o.ov)
	}
	if err != nil && err != windows.ERROR_IO_PENDING {
		return 0, err
	}
	ev, werr := windows.WaitForMultipleObjects([]windows.Handle{o.ov.HEvent, stop}, false, ms)
	var n uint32
	if werr == nil && ev == windows.WAIT_OBJECT_0 {
		if err := windows.GetOverlappedResult(h, &o.ov, &n, false); err != nil {
			return 0, err
		}
		return int(n), nil
	}
	if err := windows.CancelIoEx(h, &o.ov); err != nil && err != windows.ERROR_NOT_FOUND {
		windows.CancelIo(h)
	}
	err = windows.GetOverlappedResult(h, &o.ov, &n, true)
	switch {
	case err == nil:
		return int(n), nil // finished before the cancellation
	case werr != nil:
		return 0, werr
	case err != windows.ERROR_OPERATION_ABORTED:
		return 0, err
	case ev == windows.WAIT_OBJECT_0+1:
		return 0, errStopped
	default:
		return 0, ErrTimeout
	}
}

// inputCollection is an open input collection and its input report length.
type inputCollection struct {
	h    windows.Handle
	size int
}

type winDevice struct {
	stream, feature windows.Handle
	featureLen      int
	inputs          []inputCollection

	streamIn, streamOut *overlapped
	inputOps            []*overlapped

	stop   windows.Handle // manual-reset event, set by Close
	ioMu   sync.Mutex     // one stream or feature operation at a time
	closed atomic.Bool
	in     *inputQueue
	wg     sync.WaitGroup // input readers

	// HidD_SetFeature and HidD_GetFeature; replaced in tests.
	setFeature, getFeature func(h windows.Handle, buf []byte) error
}

// newWinDevice takes over the open collections and starts one reader per
// input collection, each keeping a read outstanding. On error it closes
// them.
func newWinDevice(stream, feature windows.Handle, featureLen int, inputs ...inputCollection) (*winDevice, error) {
	d := &winDevice{
		stream:     stream,
		feature:    feature,
		featureLen: featureLen,
		inputs:     inputs,
		in:         newInputQueue(),
		setFeature: hidSetFeature,
		getFeature: hidGetFeature,
	}
	if err := d.prepare(); err != nil {
		d.release()
		return nil, fmt.Errorf("device: %w", err)
	}
	for i, c := range d.inputs {
		d.wg.Add(1)
		go d.readInput(c.h, d.inputOps[i])
	}
	return d, nil
}

func (d *winDevice) prepare() error {
	var err error
	if d.stop, err = windows.CreateEvent(nil, 1, 0, nil); err != nil {
		return err
	}
	if d.streamIn, err = newOverlapped(streamReportLen); err != nil {
		return err
	}
	if d.streamOut, err = newOverlapped(streamReportLen); err != nil {
		return err
	}
	for _, c := range d.inputs {
		o, err := newOverlapped(c.size)
		if err != nil {
			return err
		}
		d.inputOps = append(d.inputOps, o)
	}
	return nil
}

// release closes every handle and event. No operation may be in flight.
func (d *winDevice) release() error {
	var first error
	closeHandle := func(h windows.Handle) {
		if h == 0 || h == windows.InvalidHandle {
			return
		}
		if err := windows.CloseHandle(h); err != nil && first == nil {
			first = err
		}
	}
	closeHandle(d.stream)
	closeHandle(d.feature)
	for _, c := range d.inputs {
		closeHandle(c.h)
	}
	for _, o := range append([]*overlapped{d.streamIn, d.streamOut}, d.inputOps...) {
		if o != nil {
			o.free()
		}
	}
	closeHandle(d.stop)
	return first
}

// readInput keeps one read outstanding on an input collection and queues
// the notification and touch reports it receives.
func (d *winDevice) readInput(h windows.Handle, o *overlapped) {
	defer d.wg.Done()
	for {
		n, err := o.do(h, false, windows.INFINITE, d.stop)
		if errors.Is(err, errStopped) {
			return
		}
		if err != nil {
			d.in.fail(ioErr("read input", err, true))
			return
		}
		r := append([]byte(nil), o.buf[:n]...)
		clear(o.buf)
		if isInputReport(r) && !d.in.put(r) {
			return
		}
	}
}

// lock takes the I/O lock, or returns ErrClosed.
func (d *winDevice) lock() error {
	if d.closed.Load() {
		return ErrClosed
	}
	d.ioMu.Lock()
	if d.closed.Load() {
		d.ioMu.Unlock()
		return ErrClosed
	}
	return nil
}

func (d *winDevice) SetFeature(b []byte) error {
	if err := checkFeatureLen(len(b), d.featureLen); err != nil {
		return err
	}
	if err := d.lock(); err != nil {
		return err
	}
	defer d.ioMu.Unlock()
	err := d.setFeature(d.feature, append([]byte(nil), b...))
	if err != nil && len(b) < d.featureLen && !gone(err, false) {
		// A buffer of exactly the report's length is what worked on the
		// keyboard; the collection's full length is the fallback. A keyboard
		// that has gone away is not asked again.
		full := make([]byte, d.featureLen)
		copy(full, b)
		if d.setFeature(d.feature, full) == nil {
			return nil
		}
	}
	if err != nil {
		return ioErr(fmt.Sprintf("set feature report 0x%02x", b[0]), err, false)
	}
	return nil
}

func (d *winDevice) GetFeature(id byte, n int) ([]byte, error) {
	if err := checkFeatureLen(n, d.featureLen); err != nil {
		return nil, err
	}
	if err := d.lock(); err != nil {
		return nil, err
	}
	defer d.ioMu.Unlock()
	buf := make([]byte, n)
	buf[0] = id
	err := d.getFeature(d.feature, buf)
	if err != nil && n < d.featureLen && !gone(err, false) {
		full := make([]byte, d.featureLen)
		full[0] = id
		if d.getFeature(d.feature, full) == nil {
			copy(buf, full)
			return buf, nil
		}
	}
	if err != nil {
		return nil, ioErr(fmt.Sprintf("get feature report 0x%02x", id), err, false)
	}
	return buf, nil
}

func (d *winDevice) WriteStream(f [64]byte) error {
	if err := d.lock(); err != nil {
		return err
	}
	defer d.ioMu.Unlock()
	d.streamOut.buf[0] = 0
	copy(d.streamOut.buf[1:], f[:])
	n, err := d.streamOut.do(d.stream, true, uint32(millis(streamWriteTimeout, maxWaitMillis)), d.stop)
	if err != nil {
		return ioErr("write stream", err, true)
	}
	if n != streamReportLen {
		return fmt.Errorf("device: wrote %d of %d stream bytes", n, streamReportLen)
	}
	return nil
}

func (d *winDevice) ReadStream(timeout time.Duration) ([64]byte, error) {
	var f [64]byte
	if err := d.lock(); err != nil {
		return f, err
	}
	defer d.ioMu.Unlock()
	n, err := d.streamIn.do(d.stream, false, uint32(millis(timeout, maxWaitMillis)), d.stop)
	if err != nil {
		return f, ioErr("read stream", err, true)
	}
	if n != streamReportLen {
		return f, fmt.Errorf("device: stream report of %d bytes, want %d", n, streamReportLen)
	}
	copy(f[:], d.streamIn.buf[1:])
	return f, nil
}

func (d *winDevice) ReadInput(timeout time.Duration) ([]byte, error) {
	if d.closed.Load() {
		return nil, ErrClosed
	}
	return d.in.get(timeout)
}

func (d *winDevice) Close() error {
	if !d.closed.CompareAndSwap(false, true) {
		return ErrClosed
	}
	windows.SetEvent(d.stop) // interrupts every waiting operation
	d.in.close()
	d.wg.Wait()
	d.ioMu.Lock()
	defer d.ioMu.Unlock()
	if err := d.release(); err != nil {
		return fmt.Errorf("device: close: %w", err)
	}
	return nil
}
