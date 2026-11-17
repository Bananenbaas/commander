// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

const usageText = `kb7screen puts your own buttons on the touchscreen of the Turtle Beach
Command Series KB7 and carries out their actions on this computer.

Usage:
  kb7screen <command> [flags] [config-folder]

Commands:
  check                          report whether Swarm II runs, find the keyboard
                                 and print its five profile names
  read --profile N               print the button table of profile N (0 to 4)
  preview <config> [--out DIR]   write the rendered pictures as PNG files
                                 (default DIR: <config>/previews); touches
                                 nothing else
  apply <config> [--full]        put every layout of the folder on the keyboard;
                                 --full sends every picture again
  restore <config> --profile N   give the positions kb7screen took over in
                                 profile N their original contents back
  keytest [--seconds N]          prints the keys pressed on the KB7 for N
                                 seconds (default 20, 1 to 120); nothing is
                                 saved
  run <config>                   carry out the buttons' actions until stopped
                                 with Ctrl+C

check, read, apply and restore refuse to start while Swarm II or its device
service runs. run waits for them to close.

Flags:
  -h, --help   print this text
`

// command is a parsed command line.
type command struct {
	name    string
	config  string // config folder, empty for check and read
	profile int    // read and restore
	full    bool   // apply
	out     string // preview
	seconds int    // keytest
}

// errHelp is returned by parseArgs when the usage text was asked for.
var errHelp = errors.New("help requested")

// parseArgs parses the command line without the program name. Flags and the
// config folder may come in any order.
func parseArgs(args []string) (command, error) {
	var c command
	if len(args) == 0 {
		return c, errors.New("no command given; run kb7screen --help")
	}
	switch args[0] {
	case "-h", "--help", "-help", "help":
		return c, errHelp
	}
	c.name = args[0]
	needsConfig, needsProfile := false, false
	switch c.name {
	case "check", "keytest":
	case "read":
		needsProfile = true
	case "preview", "apply", "run":
		needsConfig = true
	case "restore":
		needsConfig, needsProfile = true, true
	default:
		return c, fmt.Errorf("unknown command %q; run kb7screen --help", c.name)
	}

	fs := flag.NewFlagSet(c.name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	profile := fs.Int("profile", -1, "")
	full := fs.Bool("full", false, "")
	out := fs.String("out", "", "")
	seconds := fs.Int("seconds", 20, "")
	var rest []string
	args = args[1:]
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return c, errHelp
			}
			return c, fmt.Errorf("%s: %w", c.name, err)
		}
		if fs.NArg() == 0 {
			break
		}
		rest = append(rest, fs.Arg(0))
		args = fs.Args()[1:]
	}

	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["profile"] && !needsProfile {
		return c, fmt.Errorf("%s does not take --profile", c.name)
	}
	if set["full"] && c.name != "apply" {
		return c, fmt.Errorf("%s does not take --full", c.name)
	}
	if set["out"] && c.name != "preview" {
		return c, fmt.Errorf("%s does not take --out", c.name)
	}
	if set["seconds"] && c.name != "keytest" {
		return c, fmt.Errorf("%s does not take --seconds", c.name)
	}
	if c.name == "keytest" {
		if *seconds < 1 || *seconds > 120 {
			return c, fmt.Errorf("--seconds must be 1 to 120, got %d", *seconds)
		}
		c.seconds = *seconds
	}
	if needsProfile {
		if !set["profile"] {
			return c, fmt.Errorf("%s needs --profile N", c.name)
		}
		if *profile < 0 || *profile > 4 {
			return c, fmt.Errorf("--profile must be 0 to 4, got %d", *profile)
		}
		c.profile = *profile
	}
	if needsConfig {
		switch len(rest) {
		case 0:
			return c, fmt.Errorf("%s needs a config folder", c.name)
		case 1:
			c.config = rest[0]
		default:
			return c, fmt.Errorf("%s takes one config folder, got %s", c.name, strings.Join(rest, " "))
		}
	} else if len(rest) > 0 {
		return c, fmt.Errorf("%s takes no arguments, got %s", c.name, strings.Join(rest, " "))
	}
	c.full = *full
	c.out = *out
	return c, nil
}
