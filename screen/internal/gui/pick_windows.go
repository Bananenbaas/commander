// SPDX-License-Identifier: GPL-2.0-or-later

package gui

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	ofnFileMustExist = 0x00001000
	ofnPathMustExist = 0x00000800
	ofnNoChangeDir   = 0x00000008
	ofnExplorer      = 0x00080000

	// pickBufLen is the file buffer in UTF-16 units: room for long paths.
	pickBufLen = 32768
)

var (
	comdlg32                 = windows.NewLazySystemDLL("comdlg32.dll")
	procGetOpenFileName      = comdlg32.NewProc("GetOpenFileNameW")
	procCommDlgExtendedError = comdlg32.NewProc("CommDlgExtendedError")
)

// openFileName is OPENFILENAMEW (64-bit layout; Go's alignment rules match
// the C ones for these fields).
type openFileName struct {
	lStructSize       uint32
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

// pngFilter is the double-NUL terminated filter list.
func pngFilter() []uint16 {
	var f []uint16
	for _, s := range []string{"PNG pictures (*.png)", "*.png"} {
		f = append(f, windows.StringToUTF16(s)...) // keeps the NUL after each
	}
	return append(f, 0)
}

// pickPicture shows the open-file dialog owned by the window and returns the
// chosen path, or "" when the player cancelled. It runs on the window thread.
func (w *window) pickPicture() (string, error) {
	filter := pngFilter()
	file := make([]uint16, pickBufLen)
	title, err := windows.UTF16PtrFromString("Choose a picture")
	if err != nil {
		return "", err
	}
	ofn := openFileName{
		hwndOwner:    w.hwnd,
		lpstrFilter:  &filter[0],
		nFilterIndex: 1,
		lpstrFile:    &file[0],
		nMaxFile:     pickBufLen,
		lpstrTitle:   title,
		flags:        ofnFileMustExist | ofnPathMustExist | ofnNoChangeDir | ofnExplorer,
	}
	ofn.lStructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := procGetOpenFileName.Call(uintptr(unsafe.Pointer(&ofn)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(file)
	runtime.KeepAlive(title)
	if r == 0 {
		if code, _, _ := procCommDlgExtendedError.Call(); code != 0 {
			return "", fmt.Errorf("the file dialog failed (error %d)", code)
		}
		return "", nil
	}
	return windows.UTF16ToString(file), nil
}
