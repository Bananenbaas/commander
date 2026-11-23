# Commander: your keyboard, your way

[![CI](https://github.com/Bananenbaas/commander/actions/workflows/ci.yml/badge.svg)](https://github.com/Bananenbaas/commander/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/Bananenbaas/commander?include_prereleases&sort=semver)](https://github.com/Bananenbaas/commander/releases)
[![License: GPL v2+](https://img.shields.io/badge/license-GPL--2.0--or--later-blue.svg)](LICENSE)
[![Python 3.10+](https://img.shields.io/badge/python-3.10%2B-blue.svg)](https://www.python.org/downloads/)
[![Go 1.27+](https://img.shields.io/github/go-mod/go-version/Bananenbaas/commander?filename=screen%2Fgo.mod)](https://go.dev/dl/)
[![VirusTotal: scanned per release](https://img.shields.io/badge/VirusTotal-scanned%20per%20release-394EFF?logo=virustotal&logoColor=white)](https://github.com/Bananenbaas/commander/releases)

Commander is an open-source project for using gaming hardware without its
maker's software. It starts with the **Turtle Beach
Command Series KB7**.

The goal is to reverse-engineer the KB7's USB protocol and build an independent
software ecosystem that goes well beyond what Turtle Beach Swarm II offers.

> Swarm II should be optional, not essential. The hardware belongs to the user,
> so the software should too.

## Vision

- A fully customisable touchscreen: custom graphics, icons, animations and GIFs.
- Live dashboards: CPU, GPU, RAM, network and homelab statistics.
- Custom touchscreen pages, widgets and interactive controls.
- Advanced macro management and application integrations.
- RGB lighting control without Swarm II.
- Direct communication with the keyboard, with no proprietary software in between.

The end goal is to turn the KB7 into a fully programmable hardware platform.

## Status

**Pre-releases are out** (the newest is on the
[releases page](https://github.com/Bananenbaas/commander/releases)).
`kb7screen` and Commander drive the keyboard on Windows without Swarm II.
Swarm II is still needed for firmware updates. The Linux build passes its
tests but has not been run on a keyboard yet. What is known about the
protocol, and how it was measured, is in
[`docs/PROTOCOL.md`](docs/PROTOCOL.md).

What works in the released builds:

- Own buttons on all three touchscreen pages, with pictures, titles and
  actions (key combinations, sequences with pauses, programs, websites).
- Layered RGB lighting that reacts to key presses, and stored lighting per
  profile for when nothing streams.
- The keyboard's volume mixer page shows and controls the programs that play
  sound in Windows, on every output device.
- Profile names, and switching to a profile while a program runs.
- Commander, the tray program: keeps it all running, starts with Windows and
  updates itself from the releases, after asking.

On `main`, for the next release:

- Game profiles: one file per game with its program and the profile to show
  while it runs.
- Commander's window. Everything is set up there: a KB7 profile's buttons and
  lighting, its name and copies, the volume mixer it shows, and game profiles
  that switch the KB7 when a game starts. It also holds the settings and updates.
  Saving checks the files first and keeps a backup of hand-written ones. The
  design is in
  [`docs/design/commander-gui.md`](docs/design/commander-gui.md).

Not there yet: dashboards and widgets on the touchscreen, animations, the
media controls of the volume page, and devices other than the KB7.

| Phase | Goal | State |
|---|---|---|
| 0 | USB captures of Swarm II ↔ KB7, one action per capture | ongoing, per new feature |
| 1 | Analysis toolkit (`kb7cap`) and protocol notes | ready |
| 2 | Safe replay: device info → RGB → button layout → image upload → touch input | done on Windows: buttons, pictures, touches, profile switching, lighting, mixer, profile names |
| 3 | Programs built on it: `kb7screen`, Commander and its window; later dashboards, plugins, more devices | pre-releases; the window is being built in parts |

## Download

Ready-built executables are on the
[releases page](https://github.com/Bananenbaas/commander/releases):
`commander-windows-amd64.exe` (the tray program),
`kb7screen-windows-amd64.exe` and `kb7screen-linux-amd64` (the command line).
They are built from this repository by GitHub Actions. Each release lists the
SHA-256 of every file and links to its VirusTotal scan. The executables are
not code-signed, so Windows SmartScreen may warn before the first start. A
few of the scanners on VirusTotal flagged `v0.1.0-beta.1` (1 of 71 for
`kb7screen`, 2 of 71 for Commander, all generic or machine-learning detections);
the scan links show the current results. How to set them up is in
[`screen/README.md`](screen/README.md).

## The `kb7cap` toolkit

Pure Python (3.10+), no dependencies. Reads Windows USBPcap and Linux usbmon
captures (`.pcap` / `.pcapng`).

```sh
git clone https://github.com/Bananenbaas/commander.git
cd commander

python -m kb7tools summary capture.pcapng            # devices, interfaces, HID reports, traffic
python -m kb7tools dump capture.pcapng -d <vid>:<pid> # transfers with hex payload
python -m kb7tools diff a.pcapng b.pcapng -d <vid>:<pid>  # which bytes changed
```

Or install it as a command: `pip install -e .`, then `kb7cap …`.

Keyboard reports are hidden by default, because they show what was typed during
the capture (`--show-keystrokes` to override). That covers boot-keyboard
interfaces and, per report ID, any report whose descriptor draws on the
keyboard usage page, including ones on an otherwise vendor-defined interface,
which the KB7 has.

When a capture lacks a HID interface's report descriptor (because it started
with the device already plugged in), nothing rules out typing on that
interface, so **all** of its reports are hidden and the output says why. A
device whose configuration descriptor is not in the capture at all is not
recognised as HID and is not filtered. Review any output before you share it.

Tests: `python -m unittest discover -s tests -t .`

## `kb7screen` and Commander: your own buttons and lighting

`kb7screen` is a Go program (Windows, with a Linux build) that puts your own
pictures and titles on the KB7's touchscreen buttons and carries out their
actions on the PC, without Swarm II: key combinations, sequences with pauses,
programs, websites. It also streams layered RGB lighting: waves, breathing,
ripples from key presses, and keys that light up with an on/off state.
It drives the volume mixer page from Windows and switches profiles per game.
Commander is its tray program for Windows: it keeps it running, reloads the
configuration, starts with Windows, updates itself and has a window for the
settings. See
[`screen/README.md`](screen/README.md).

## Contributing

Captures are the bottleneck in Phase 0, so a good capture is the most useful
thing you can contribute. Read [`CONTRIBUTING.md`](CONTRIBUTING.md) and
[`docs/CAPTURE-GUIDE.md`](docs/CAPTURE-GUIDE.md) first. In short: one action per
capture, every scenario twice, no typing while recording, and **never publish
raw captures**, share reviewed `kb7cap` output instead.

Working on the code? [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) has the
layout, conventions and known pitfalls. Privacy or hardware-safety problems go
to [`SECURITY.md`](SECURITY.md) rather than a public issue. Participation is
covered by the [Code of Conduct](CODE_OF_CONDUCT.md).

## Safety rules

- Only commands that have been observed in captures are ever sent to the device.
  No guessing and no fuzzing of write commands.
- Firmware update, bootloader and factory-reset commands are never implemented.
- The protocol is derived solely from observing USB traffic of hardware we own
  (EU Directive 2009/24/EC art. 5(3)). Swarm II binaries, firmware images and
  assets are not analysed, decompiled or included.

Using this software is at your own risk. It may void your warranty.

## Legal

This project is not affiliated with or endorsed by Turtle Beach. "Turtle Beach",
"Swarm II" and "KB7" are trademarks of their respective owner. Protocol
analysis is done for interoperability, on hardware we own.

## License

[GPL-2.0-or-later](LICENSE).
