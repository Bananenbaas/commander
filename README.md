# KB7 Unleashed: Beyond Swarm II

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

**Phase 0: collecting captures.** The protocol is not known yet; nothing in
this repository talks to the keyboard yet. Progress is tracked in
[`docs/PROTOCOL.md`](docs/PROTOCOL.md).

| Phase | Goal | State |
|---|---|---|
| 0 | USB captures of Swarm II ↔ KB7, one action per capture | in progress |
| 1 | Analysis toolkit (`kb7cap`) and protocol notes | toolkit ready |
| 2 | Safe replay: device info → RGB → button layout → image upload → touch input | not started |
| 3 | `kb7` library, daemon, dashboards, plugins, macros | not started |

## The `kb7cap` toolkit

Pure Python (3.10+), no dependencies. Reads Windows USBPcap and Linux usbmon
captures (`.pcap` / `.pcapng`).

```sh
python -m kb7tools summary capture.pcapng            # devices, interfaces, HID reports, traffic
python -m kb7tools dump capture.pcapng -d <vid>:<pid> # transfers with hex payload
python -m kb7tools diff a.pcapng b.pcapng -d <vid>:<pid>  # which bytes changed
```

Or install it as a command: `pip install -e .`, then `kb7cap …`.

Reports from boot-keyboard interfaces are hidden by default, because they show
what was typed during the capture (`--show-keystrokes` to override).

Tests: `python -m unittest discover -s tests -t .`

## Contributing captures

Follow [`docs/CAPTURE-GUIDE.md`](docs/CAPTURE-GUIDE.md). In short: one action
per capture, every scenario twice, no typing while recording, and never publish
raw captures. Share reviewed `kb7cap` output instead.

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
