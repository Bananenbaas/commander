// SPDX-License-Identifier: GPL-2.0-or-later

package action

import (
	"strings"

	"golang.org/x/sys/windows"
)

type winLauncher struct{}

// NewLauncher returns a launcher backed by ShellExecuteW.
func NewLauncher() Launcher { return winLauncher{} }

func (winLauncher) Run(program string, args []string) error {
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = windows.EscapeArg(a)
	}
	return shellOpen(program, strings.Join(quoted, " "))
}

func (winLauncher) Open(target string) error { return shellOpen(target, "") }

func shellOpen(file, params string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	f, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	var p *uint16
	if params != "" {
		if p, err = windows.UTF16PtrFromString(params); err != nil {
			return err
		}
	}
	return windows.ShellExecute(0, verb, f, p, nil, windows.SW_SHOWNORMAL)
}
