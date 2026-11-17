// SPDX-License-Identifier: GPL-2.0-or-later

// Package autostart switches starting Commander with Windows on and off: the
// value Commander under HKCU\Software\Microsoft\Windows\CurrentVersion\Run.
package autostart

import "strings"

const (
	// runKey is the user's own Run key, relative to HKEY_CURRENT_USER.
	runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
	// valueName is the name of Commander's value under runKey.
	valueName = "Commander"
)

// Command is the command line that starts exe with the configuration folder
// dir: both in double quotes. Without a folder only the program is given.
func Command(exe, dir string) string {
	if dir == "" {
		return quote(exe)
	}
	return quote(exe) + " " + quote(dir)
}

// quote puts s in double quotes the way Windows programs split their command
// line: a quote inside becomes \", and backslashes in front of a quote
// (including the closing one) are doubled.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes+1))
			slashes = 0
		default:
			slashes = 0
		}
		b.WriteByte(c)
	}
	b.WriteString(strings.Repeat(`\`, slashes))
	b.WriteByte('"')
	return b.String()
}

// Parse splits a command line made by Command, or an unquoted one, into the
// program and the folder. ok is false when it is not one program with at most
// one argument.
func Parse(cmd string) (exe, dir string, ok bool) {
	if strings.IndexByte(cmd, 0) >= 0 {
		return "", "", false
	}
	args, ok := split(cmd)
	if !ok || len(args) == 0 || len(args) > 2 || args[0] == "" {
		return "", "", false
	}
	if len(args) == 2 {
		dir = args[1]
	}
	return args[0], dir, true
}

// split splits a command line into arguments by the rules of
// CommandLineToArgvW: 2n backslashes and a quote give n backslashes and
// toggle quoting, 2n+1 backslashes and a quote give n backslashes and a
// literal quote, other backslashes are literal. An unclosed quote is refused.
func split(cmd string) ([]string, bool) {
	var args []string
	var cur strings.Builder
	inArg, quoted := false, false
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case c == '\\':
			n := 0
			for i < len(cmd) && cmd[i] == '\\' {
				n++
				i++
			}
			if i < len(cmd) && cmd[i] == '"' {
				cur.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					cur.WriteByte('"')
				} else {
					quoted = !quoted
				}
			} else {
				cur.WriteString(strings.Repeat(`\`, n))
				i--
			}
			inArg = true
		case c == '"':
			quoted = !quoted
			inArg = true
		case (c == ' ' || c == '\t') && !quoted:
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteByte(c)
			inArg = true
		}
	}
	if quoted {
		return nil, false
	}
	if inArg {
		args = append(args, cur.String())
	}
	return args, true
}
