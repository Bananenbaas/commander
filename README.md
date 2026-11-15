# KB7 Unleashed: Beyond Swarm II

[![CI](https://github.com/Bananenbaas/kb7-commander/actions/workflows/ci.yml/badge.svg)](https://github.com/Bananenbaas/kb7-commander/actions/workflows/ci.yml)
[![License: GPL v2+](https://img.shields.io/badge/license-GPL--2.0--or--later-blue.svg)](LICENSE)
[![Python 3.10+](https://img.shields.io/badge/python-3.10%2B-blue.svg)](https://www.python.org/downloads/)

An open-source project to unlock the full potential of the **Turtle Beach
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

**Phase 0: collecting captures**, with the first results in. The device is
mapped, Swarm II's startup exchange and its live state stream are documented,
and one command has been replayed successfully: a volume level written to the
keyboard's screen. Progress is tracked in
[`docs/PROTOCOL.md`](docs/PROTOCOL.md).

| Phase | Goal | State |
|---|---|---|
| 0 | USB captures of Swarm II ↔ KB7, one action per capture | in progress |
| 1 | Analysis toolkit (`kb7cap`) and protocol notes | toolkit ready |
| 2 | Safe replay: device info → RGB → button layout → image upload → touch input | first command replayed |
| 3 | `kb7` library, daemon, dashboards, plugins, macros | not started |

## The `kb7cap` toolkit

Pure Python (3.10+), no dependencies. Reads Windows USBPcap and Linux usbmon
captures (`.pcap` / `.pcapng`).

```sh
git clone https://github.com/Bananenbaas/kb7-commander.git
cd kb7-commander

python -m kb7tools summary capture.pcapng            # devices, interfaces, HID reports, traffic
python -m kb7tools dump capture.pcapng -d <vid>:<pid> # transfers with hex payload
python -m kb7tools diff a.pcapng b.pcapng -d <vid>:<pid>  # which bytes changed
```

Or install it as a command: `pip install -e .`, then `kb7cap …`.

Keyboard reports are hidden by default, because they show what was typed during
the capture (`--show-keystrokes` to override). That covers boot-keyboard
interfaces and, per report ID, keyboard reports that sit on an otherwise
vendor-defined interface — the KB7 has one of those. It relies on the HID report
descriptor being present in the capture, which means a capture that does not
include an enumeration cannot be filtered this way. Review any output before
you share it.

Tests: `python -m unittest discover -s tests -t .`

## Contributing

Captures are the bottleneck in Phase 0, so a good capture is the most useful
thing you can contribute. Read [`CONTRIBUTING.md`](CONTRIBUTING.md) and
[`docs/CAPTURE-GUIDE.md`](docs/CAPTURE-GUIDE.md) first. In short: one action per
capture, every scenario twice, no typing while recording, and **never publish
raw captures** — share reviewed `kb7cap` output instead.

Working on the code? [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) has the
layout, conventions and known pitfalls. Privacy or hardware-safety problems go
to [`SECURITY.md`](SECURITY.md) rather than a public issue. Participation is
covered by the [Code of Conduct](CODE_OF_CONDUCT.md).

## Safety rules

- Only commands that have been observed in captures are ever sent to the device.
  No guessing and no fuzzing of write commands.
- Firmware update, bootloader and factory-reset commands are never implemented.
- No Swarm II binaries, assets or code are copied into this project.

Using this software is at your own risk. It may void your warranty.

## Legal

This project is not affiliated with or endorsed by Turtle Beach. "Turtle Beach",
"Swarm II" and "KB7" are trademarks of their respective owner. Protocol
analysis is done for interoperability, on hardware we own.

## License

[GPL-2.0-or-later](LICENSE).
