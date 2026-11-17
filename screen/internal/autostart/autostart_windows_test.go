// SPDX-License-Identifier: GPL-2.0-or-later

package autostart

import (
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// useTestKey points the package at a scratch key under HKCU instead of the
// real Run key, and removes it afterwards.
func useTestKey(t *testing.T) {
	t.Helper()
	path := fmt.Sprintf(`Software\Commander-autostart-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
	old := keyPath
	keyPath = path
	t.Cleanup(func() {
		keyPath = old
		registry.DeleteKey(registry.CURRENT_USER, path)
	})
}

func splitByWindows(t *testing.T, cmd string) []string {
	t.Helper()
	p, err := windows.UTF16PtrFromString(cmd)
	if err != nil {
		t.Fatal(err)
	}
	var argc int32
	argv, err := windows.CommandLineToArgv(p, &argc)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(argv)))
	args := make([]string, argc)
	for i := range args {
		args[i] = windows.UTF16PtrToString(&argv[i][0])
	}
	return args
}

// The folder must reach the started program unchanged: Windows splits the
// command line with CommandLineToArgvW.
func TestCommandMatchesWindowsSplitting(t *testing.T) {
	for _, dir := range []string{`D:\My Config`, `D:\`, `D:\x\\`, `D:\a "quoted" name`, `D:\back\\"slash`} {
		args := splitByWindows(t, Command(`C:\Program Files\c.exe`, dir))
		if len(args) != 2 || args[0] != `C:\Program Files\c.exe` || args[1] != dir {
			t.Errorf("Windows splits Command(..., %q) into %q", dir, args)
		}
	}
}

func TestEnableDisable(t *testing.T) {
	useTestKey(t)
	exe := `C:\Program Files\Commander\commander.exe`
	if on, err := Enabled(exe); err != nil || on {
		t.Fatalf("Enabled before Enable = %v, %v", on, err)
	}
	if err := Disable(); err != nil {
		t.Fatalf("Disable without a key: %v", err)
	}
	if err := Enable(exe, `D:\cfg\`); err != nil {
		t.Fatal(err)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	v, _, err := k.GetStringValue(valueName)
	k.Close()
	if err != nil || v != Command(exe, `D:\cfg\`) {
		t.Fatalf("value = %q, %v", v, err)
	}
	if on, err := Enabled(`c:\program files\COMMANDER\commander.exe`); err != nil || !on {
		t.Fatalf("Enabled (other case) = %v, %v", on, err)
	}
	if on, err := Enabled(`C:\elsewhere\commander.exe`); err != nil || on {
		t.Fatalf("Enabled for another exe = %v, %v", on, err)
	}
	if err := Disable(); err != nil {
		t.Fatal(err)
	}
	if on, err := Enabled(exe); err != nil || on {
		t.Fatalf("Enabled after Disable = %v, %v", on, err)
	}
	if err := Disable(); err != nil {
		t.Fatalf("second Disable: %v", err)
	}
}
