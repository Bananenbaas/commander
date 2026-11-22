// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package pipe

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// testName returns a pipe name of this test and process only.
func testName(t *testing.T) string {
	t.Helper()
	n := strings.NewReplacer("/", "-", " ", "-").Replace(t.Name())
	return fmt.Sprintf(`\\.\pipe\Commander-test-%s-%d`, n, os.Getpid())
}

func listen(t *testing.T) (*Listener, string) {
	t.Helper()
	name := testName(t)
	l, err := Listen(name)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l, name
}

// acceptAsync runs Accept in a goroutine and delivers its result.
func acceptAsync(l *Listener) <-chan acceptResult {
	ch := make(chan acceptResult, 1)
	go func() {
		c, err := l.Accept()
		ch <- acceptResult{c, err}
	}()
	return ch
}

type acceptResult struct {
	conn io.ReadWriteCloser
	err  error
}

func waitAccept(t *testing.T, ch <-chan acceptResult) io.ReadWriteCloser {
	t.Helper()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Accept: %v", r.err)
		}
		t.Cleanup(func() { r.conn.Close() })
		return r.conn
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return")
		return nil
	}
}

func dial(t *testing.T, name string) io.ReadWriteCloser {
	t.Helper()
	c, err := Dial(name, 5*time.Second)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestDialAndAccept(t *testing.T) {
	l, name := listen(t)
	ch := acceptAsync(l)
	client := dial(t, name)
	server := waitAccept(t, ch)

	if _, err := io.WriteString(client, "ping\n"); err != nil {
		t.Fatalf("client write: %v", err)
	}
	line, err := bufio.NewReader(server).ReadString('\n')
	if err != nil || line != "ping\n" {
		t.Fatalf("server read = %q, %v; want %q", line, err, "ping\n")
	}
	if _, err := io.WriteString(server, "pong\n"); err != nil {
		t.Fatalf("server write: %v", err)
	}
	line, err = bufio.NewReader(client).ReadString('\n')
	if err != nil || line != "pong\n" {
		t.Fatalf("client read = %q, %v; want %q", line, err, "pong\n")
	}
}

func TestSecondListenFails(t *testing.T) {
	_, name := listen(t)
	l2, err := Listen(name)
	if err == nil {
		l2.Close()
		t.Fatal("second Listen on the same name succeeded")
	}
}

func TestCloseUnblocksAccept(t *testing.T) {
	l, _ := listen(t)
	ch := acceptAsync(l)
	time.Sleep(50 * time.Millisecond)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case r := <-ch:
		if !errors.Is(r.err, ErrClosed) {
			t.Fatalf("Accept after Close = %v, %v; want ErrClosed", r.conn, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Accept still blocked 2 s after Close")
	}
	if _, err := l.Accept(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Accept on a closed listener = %v; want ErrClosed", err)
	}
}

func TestTwoClientsInTurn(t *testing.T) {
	l, name := listen(t)
	for i := 1; i <= 2; i++ {
		ch := acceptAsync(l)
		client, err := Dial(name, 5*time.Second)
		if err != nil {
			t.Fatalf("Dial client %d: %v", i, err)
		}
		server := waitAccept(t, ch)
		msg := fmt.Sprintf("client %d\n", i)
		if _, err := io.WriteString(client, msg); err != nil {
			t.Fatalf("client %d write: %v", i, err)
		}
		line, err := bufio.NewReader(server).ReadString('\n')
		if err != nil || line != msg {
			t.Fatalf("server read = %q, %v; want %q", line, err, msg)
		}
		client.Close()
	}
}

func TestDialWithoutListenerFails(t *testing.T) {
	const timeout = 500 * time.Millisecond
	start := time.Now()
	c, err := Dial(testName(t), timeout)
	if err == nil {
		c.Close()
		t.Fatal("Dial without a listener succeeded")
	}
	if !errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		t.Fatalf("Dial without a listener = %v; want ERROR_FILE_NOT_FOUND", err)
	}
	if d := time.Since(start); d > timeout+2*time.Second {
		t.Fatalf("Dial took %v, timeout was %v", d, timeout)
	}
}

func currentUser(t *testing.T) *windows.SID {
	t.Helper()
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("GetTokenUser: %v", err)
	}
	return u.User.Sid
}

func systemSID(t *testing.T) *windows.SID {
	t.Helper()
	sid, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatalf("CreateWellKnownSid: %v", err)
	}
	return sid
}

func TestCheckOwner(t *testing.T) {
	user, system := currentUser(t), systemSID(t)
	if err := checkOwner(user, user); err != nil {
		t.Fatalf("checkOwner(user, user) = %v; want nil", err)
	}
	if err := checkOwner(system, user); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("checkOwner(system, user) = %v; want ErrNotOurs", err)
	}
	if err := checkOwner(nil, user); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("checkOwner(nil, user) = %v; want ErrNotOurs", err)
	}
}

// TestPipeOwnerIsTheUser reads the owner of a real instance: it is the user,
// and the same handle is rejected when another user is expected.
func TestPipeOwnerIsTheUser(t *testing.T) {
	l, name := listen(t)
	ch := acceptAsync(l)
	_ = dial(t, name) // Dial itself passed the owner check
	server := waitAccept(t, ch)
	h := windows.Handle(server.(*os.File).Fd())
	if err := checkHandleOwner(h, currentUser(t)); err != nil {
		t.Fatalf("owner check against the user = %v; want nil", err)
	}
	if err := checkHandleOwner(h, systemSID(t)); !errors.Is(err, ErrNotOurs) {
		t.Fatalf("owner check against SYSTEM = %v; want ErrNotOurs", err)
	}
}

func TestNameHasTheUserSID(t *testing.T) {
	name, err := Name()
	if err != nil {
		t.Fatalf("Name: %v", err)
	}
	if want := `\\.\pipe\Commander-S-1-5-`; !strings.HasPrefix(name, want) {
		t.Fatalf("Name() = %q; want prefix %q", name, want)
	}
}

func TestCloseUnblocksRead(t *testing.T) {
	l, name := listen(t)
	ch := acceptAsync(l)
	_ = dial(t, name)
	// Not waitAccept: this test closes server itself, exactly once.
	var server io.ReadWriteCloser
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("Accept: %v", r.err)
		}
		server = r.conn
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not return")
	}

	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := server.Read(make([]byte, 16))
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		t.Fatalf("Read returned before Close: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	go server.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Read after Close returned no error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read still blocked 2 s after Close")
	}
}
