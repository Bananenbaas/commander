// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package pipe

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// bufferSize is the in and out buffer size of each pipe instance.
const bufferSize = 64 * 1024

var procWaitNamedPipe = windows.NewLazySystemDLL("kernel32.dll").NewProc("WaitNamedPipeW")

// userSID is the SID of the user this process runs as.
func userSID() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("pipe: read the user of this process: %w", err)
	}
	return user.User.Sid.String(), nil
}

// Listener accepts connections on a named pipe. One instance of the pipe is
// always waiting for a client, so the name stays owned by this process from
// Listen until Close.
type Listener struct {
	name string
	sa   *windows.SecurityAttributes
	quit windows.Handle // set by Close, ends a pending Accept

	mu     sync.Mutex
	closed bool
	next   windows.Handle // the instance the next Accept connects, or 0
	active sync.WaitGroup // running Accept calls
	// pending holds the OVERLAPPED of each running ConnectNamedPipe. The
	// kernel writes to it until the call completes, so it must live on the
	// heap (stacks move) and stay reachable until then.
	pending map[*windows.Overlapped]struct{}
}

// Listen creates the first instance of the pipe; it fails when another
// process already listens on name (FILE_FLAG_FIRST_PIPE_INSTANCE).
func Listen(name string) (*Listener, error) {
	sid, err := userSID()
	if err != nil {
		return nil, err
	}
	// The owner is set to the user, not left to the token's default owner
	// (Administrators in an elevated token), so Dial's owner check holds.
	sd, err := windows.SecurityDescriptorFromString("O:" + sid + "D:P(A;;GA;;;" + sid + ")")
	if err != nil {
		return nil, fmt.Errorf("pipe: build the security descriptor: %w", err)
	}
	sa := &windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(*sa))
	l := &Listener{name: name, sa: sa, pending: make(map[*windows.Overlapped]struct{})}
	first, err := l.instance(true)
	if err != nil {
		return nil, err
	}
	quit, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		windows.CloseHandle(first)
		return nil, fmt.Errorf("pipe: create an event: %w", err)
	}
	l.next, l.quit = first, quit
	return l, nil
}

// instance creates one instance of the pipe.
func (l *Listener) instance(first bool) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(l.name)
	if err != nil {
		return 0, fmt.Errorf("pipe: %q: %w", l.name, err)
	}
	flags := uint32(windows.PIPE_ACCESS_DUPLEX | windows.FILE_FLAG_OVERLAPPED)
	if first {
		flags |= windows.FILE_FLAG_FIRST_PIPE_INSTANCE
	}
	mode := uint32(windows.PIPE_TYPE_BYTE | windows.PIPE_READMODE_BYTE |
		windows.PIPE_WAIT | windows.PIPE_REJECT_REMOTE_CLIENTS)
	h, err := windows.CreateNamedPipe(name, flags, mode,
		windows.PIPE_UNLIMITED_INSTANCES, bufferSize, bufferSize, 0, l.sa)
	if err != nil {
		return 0, fmt.Errorf("pipe: create %s: %w", l.name, err)
	}
	return h, nil
}

// Accept waits for the next client. After Close it returns ErrClosed.
func (l *Listener) Accept() (io.ReadWriteCloser, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrClosed
	}
	h := l.next
	l.next = 0
	if h == 0 {
		var err error
		if h, err = l.instance(false); err != nil {
			l.mu.Unlock()
			return nil, err
		}
	}
	l.active.Add(1)
	l.mu.Unlock()
	defer l.active.Done()

	if err := l.connect(h); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		windows.CloseHandle(h)
		return nil, ErrClosed
	}
	// Keep an instance waiting, so the name is never free for another
	// process. If this fails, the next Accept tries again.
	if next, err := l.instance(false); err == nil {
		l.next = next
	}
	return os.NewFile(uintptr(h), l.name), nil
}

// connect waits until a client opens instance h or the listener is closed.
func (l *Listener) connect(h windows.Handle) error {
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return fmt.Errorf("pipe: create an event: %w", err)
	}
	defer windows.CloseHandle(ev)
	// Every return below happens after the operation has completed or was
	// cancelled and waited for, so ov can then be dropped.
	ov := &windows.Overlapped{HEvent: ev}
	l.mu.Lock()
	l.pending[ov] = struct{}{}
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		delete(l.pending, ov)
		l.mu.Unlock()
	}()
	err = windows.ConnectNamedPipe(h, ov)
	switch {
	case err == nil, errors.Is(err, windows.ERROR_PIPE_CONNECTED):
		return nil
	case !errors.Is(err, windows.ERROR_IO_PENDING):
		return fmt.Errorf("pipe: wait for a client on %s: %w", l.name, err)
	}
	which, err := windows.WaitForMultipleObjects([]windows.Handle{ev, l.quit}, false, windows.INFINITE)
	if err != nil || which != windows.WAIT_OBJECT_0 {
		windows.CancelIoEx(h, ov)
		var n uint32
		windows.GetOverlappedResult(h, ov, &n, true)
		if err != nil {
			return fmt.Errorf("pipe: wait for a client on %s: %w", l.name, err)
		}
		return ErrClosed
	}
	var n uint32
	if err := windows.GetOverlappedResult(h, ov, &n, false); err != nil {
		return fmt.Errorf("pipe: wait for a client on %s: %w", l.name, err)
	}
	return nil
}

// checkHandleOwner fails with ErrNotOurs unless the owner of the pipe h is
// user.
func checkHandleOwner(h windows.Handle, user *windows.SID) error {
	sd, err := windows.GetSecurityInfo(h, windows.SE_KERNEL_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read the owner: %w", err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read the owner: %w", err)
	}
	return checkOwner(owner, user)
}

// checkOwner fails with ErrNotOurs unless owner is user.
func checkOwner(owner, user *windows.SID) error {
	if owner == nil || user == nil || !owner.Equals(user) {
		return fmt.Errorf("%w (owner %v)", ErrNotOurs, owner)
	}
	return nil
}

// Close stops the listener and ends a pending Accept with ErrClosed.
// Connections already accepted stay open.
func (l *Listener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	windows.SetEvent(l.quit)
	if l.next != 0 {
		windows.CloseHandle(l.next)
		l.next = 0
	}
	l.mu.Unlock()
	l.active.Wait()
	return windows.CloseHandle(l.quit)
}

// Dial connects to name. It fails at once when the pipe does not exist and
// waits up to timeout only while every instance is busy. It fails with
// ErrNotOurs when the pipe is owned by anyone but the signed-in user, so a
// pipe another account created under this name first is never used.
func Dial(name string, timeout time.Duration) (io.ReadWriteCloser, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, fmt.Errorf("pipe: %q: %w", name, err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("pipe: read the user of this process: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		// SECURITY_IDENTIFICATION: the server may learn who we are, but
		// cannot act as us.
		h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
			windows.OPEN_EXISTING,
			windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		if err == nil {
			if err := checkHandleOwner(h, user.User.Sid); err != nil {
				windows.CloseHandle(h)
				return nil, fmt.Errorf("pipe: open %s: %w", name, err)
			}
			return os.NewFile(uintptr(h), name), nil
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, fmt.Errorf("pipe: open %s: %w", name, err)
		}
		left := time.Until(deadline)
		if left <= 0 {
			return nil, fmt.Errorf("pipe: open %s: no free instance within %v", name, timeout)
		}
		ms := uint32(left.Milliseconds())
		if ms == 0 {
			ms = 1
		}
		// WaitNamedPipe returns when an instance is free or the time is up;
		// the loop then tries again and checks the deadline.
		procWaitNamedPipe.Call(uintptr(unsafe.Pointer(p)), uintptr(ms))
	}
}
