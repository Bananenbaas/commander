// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package procs

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// list returns the executable name of every process.
func list() ([]string, error) {
	procs, err := listProcs()
	if err != nil {
		return nil, err
	}
	out := make([]string, len(procs))
	for i, p := range procs {
		out[i] = p.Name
	}
	return out, nil
}

// listProcs returns every process of a Toolhelp snapshot; the path is empty
// for processes that may not be opened.
func listProcs() ([]Proc, error) {
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
	var out []Proc
	for {
		out = append(out, Proc{
			Name: windows.UTF16ToString(entry.ExeFile[:]),
			Path: imagePath(entry.ProcessID),
		})
		if err := windows.Process32Next(snap, &entry); err != nil {
			if err == windows.ERROR_NO_MORE_FILES {
				return out, nil
			}
			return nil, err
		}
	}
}

// imagePath returns the full path of the process's program, or "" when the
// process cannot be opened (protected or gone).
func imagePath(pid uint32) string {
	if pid == 0 {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:n])
}
