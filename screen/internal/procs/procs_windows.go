//go:build windows

package procs

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// list returns the executable name of every process from a Toolhelp snapshot.
func list() ([]string, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil, err
	}
	var out []string
	for {
		out = append(out, windows.UTF16ToString(entry.ExeFile[:]))
		if err := windows.Process32Next(snap, &entry); err != nil {
			if err == windows.ERROR_NO_MORE_FILES {
				return out, nil
			}
			return nil, err
		}
	}
}
