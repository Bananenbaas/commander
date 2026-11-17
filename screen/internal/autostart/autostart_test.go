// SPDX-License-Identifier: GPL-2.0-or-later

package autostart

import "testing"

func TestCommandQuotesBothPaths(t *testing.T) {
	got := Command(`C:\Program Files\Commander\commander.exe`, `D:\My Config`)
	want := `"C:\Program Files\Commander\commander.exe" "D:\My Config"`
	if got != want {
		t.Fatalf("Command = %s, want %s", got, want)
	}
}

func TestCommandParseRoundTrip(t *testing.T) {
	cases := []struct{ exe, dir string }{
		{`C:\Program Files\Commander\commander.exe`, `D:\My Config`},
		{`C:\c.exe`, `D:\`},                 // trailing backslash before the closing quote
		{`C:\c.exe`, `D:\a "quoted" name`},  // quotes in the path
		{`C:\c.exe`, `D:\back\\"slash`},     // backslashes before a quote
		{`C:\with space\c.exe`, `D:\x\\`},   // two trailing backslashes
		{`C:\c.exe`, `D:\tab	and  spaces `}, // tab and runs of spaces
		{`C:\c.exe`, ``},
	}
	for _, c := range cases {
		cmd := Command(c.exe, c.dir)
		exe, dir, ok := Parse(cmd)
		if !ok || exe != c.exe || dir != c.dir {
			t.Errorf("Parse(Command(%q, %q)) = %q, %q, %v (command %s)", c.exe, c.dir, exe, dir, ok, cmd)
		}
	}
}

func TestParseUnquoted(t *testing.T) {
	exe, dir, ok := Parse(`C:\Tools\commander.exe D:\cfg`)
	if !ok || exe != `C:\Tools\commander.exe` || dir != `D:\cfg` {
		t.Fatalf("Parse = %q, %q, %v", exe, dir, ok)
	}
	exe, dir, ok = Parse(`  C:\Tools\commander.exe  `)
	if !ok || exe != `C:\Tools\commander.exe` || dir != "" {
		t.Fatalf("Parse exe only = %q, %q, %v", exe, dir, ok)
	}
	exe, dir, ok = Parse(`"C:\Program Files\c.exe" D:\cfg`)
	if !ok || exe != `C:\Program Files\c.exe` || dir != `D:\cfg` {
		t.Fatalf("Parse mixed = %q, %q, %v", exe, dir, ok)
	}
}

func TestParseGarbage(t *testing.T) {
	for _, s := range []string{
		"",
		"   ",
		`"C:\c.exe`,           // unterminated quote
		`"C:\c.exe" "D:\cfg`,  // unterminated quote
		`a b c`,               // too many arguments
		`"C:\c.exe" "D:\x" y`, // too many arguments
		`""`,                  // empty program
		"\"C:\\c.exe\"x\x00",  // NUL
	} {
		if exe, dir, ok := Parse(s); ok {
			t.Errorf("Parse(%q) = %q, %q, true; want ok false", s, exe, dir)
		}
	}
}
