# Development notes

Working conventions for KB7 Unleashed (`kb7-commander`). For how captures are
made, see [`CAPTURE-GUIDE.md`](CAPTURE-GUIDE.md); for what is known about the
protocol, [`PROTOCOL.md`](PROTOCOL.md). For how to contribute, see
[`../CONTRIBUTING.md`](../CONTRIBUTING.md).

## Repository layout

| Path | Purpose |
|---|---|
| `kb7tools/pcap.py` | pcap/pcapng reader → `RawPacket(ts, linktype, data)` |
| `kb7tools/usb.py` | USBPcap (249) and usbmon (189/220) → paired `Transfer`s |
| `kb7tools/descriptors.py` | Device/config/string descriptors, HID report descriptor summary |
| `kb7tools/analysis.py` | `Capture` (inventory, filtering), hexdump, summary, diff |
| `kb7tools/cli.py` | `kb7cap summary \| dump \| diff` |
| `tests/synth.py` | Builds synthetic captures (fake VID:PID 1234:5678) |
| `docs/PROTOCOL.md` | Protocol knowledge base, with status tags |
| `docs/CAPTURE-GUIDE.md` | How captures are made and named |
| `docs/test-images/` | PNGs with known pixel values, for upload captures |
| `captures/` | Local raw captures, git-ignored (they contain keystrokes) |

## Conventions

- **Language**: everything in the repository is English — code, comments,
  documentation, commit messages.
- **License**: GPL-2.0-or-later, for compatibility with OpenRGB. Every source
  file starts with `# SPDX-License-Identifier: GPL-2.0-or-later`.
- **Dependencies**: the toolkit is **stdlib only**. No pyshark or tshark
  dependency; Python 3.10 is the minimum.
- **No deployment pipeline**: CI runs the tests and nothing else.

## Invariants

- **Hardware safety**: only commands that have been observed in a capture are
  ever replayed. Firmware-update, bootloader and factory-reset commands are
  never implemented — they belong on the do-not-send list in `PROTOCOL.md`. No
  fuzzing of write commands.
- **Privacy**: raw captures are never committed. `PROTOCOL.md` contains only
  short, reviewed hex excerpts. Keystroke reports stay hidden by default in the
  tooling.
- **Clean room**: no Swarm II binaries, assets or decompiled code enter this
  repository.
- **Knowledge discipline**: every claim in `PROTOCOL.md` carries a status tag
  (`confirmed` / `observed` / `hypothesis` / `unknown`) and names the capture(s)
  it rests on.
- **Tests** use the fake VID:PID 1234:5678 and must not encode guesses about
  the real protocol.

## Commands

```sh
python -m unittest discover -s tests -t .   # tests
python -m compileall -q kb7tools tests      # syntax check
python -m kb7tools summary <capture>        # run without installing
pip install -e .                            # then: kb7cap summary <capture>
```

## Known gaps and pitfalls

- **The USBPcap control-transfer layout is unverified against a real capture.**
  The parser accepts OUT data both appended to the SETUP-stage packet and in a
  separate DATA-stage packet, and closes a control transfer on the STATUS or
  COMPLETE stage. The tests only cover our own synthetic layout. Check the
  first real KB7 capture with `kb7cap dump --standard` against Wireshark's view
  and record the outcome here.
- The `license = "GPL-2.0-or-later"` SPDX string in `pyproject.toml` requires
  setuptools ≥ 77.
- The KB7 is not attached to the machine the toolkit is developed on; all
  captures come from a Windows machine running Swarm II.
