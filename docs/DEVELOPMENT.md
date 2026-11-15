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

- **USBPcap control-transfer layout — checked, and it bit us once.** USBPcap
  1.5.4 emits no DATA-stage packet for a control transfer: the payload is
  attached to the **COMPLETE** stage. The parser originally closed the transfer
  on COMPLETE without reading that payload, so every control transfer in a real
  capture came out empty and no device descriptor was ever found. Fixed, with
  `tests/synth.py` now able to build both layouts
  (`control_in(..., data_on_complete=True)`). When adding a stage, keep the
  "only if no earlier stage supplied data" guard, or a capture containing both
  layouts will count the same bytes twice.
- **Transfers are timestamped when they complete.** Interrupt IN requests are
  queued long before the device has data — Windows keeps several outstanding per
  endpoint and resubmits one the moment another completes — so a request's
  submission time says nothing about when its data arrived. An earlier version
  timestamped and ordered by submission, which put each report next to the
  previous one: two screen taps three seconds apart came out as an adjacent
  duplicate pair, and the order of the dial's three reports was reversed.
  `Transfer.submitted` keeps the queueing time. Requests still outstanding when
  a capture stops never complete and are emitted last, without data.
- `--inject-descriptors` gives device and configuration descriptors only. HID
  report descriptors and string descriptors appear solely in a real enumeration,
  so `01-plug-in` cannot be skipped.
- **Keystroke hiding fails closed.** A report counts as typing when its
  descriptor draws on the keyboard usage page (`0x07`) anywhere — the summariser
  records every page a report's main items use, including pages carried in the
  high half of four-byte Usage / Usage Minimum / Usage Maximum items. Recording
  only the page in effect at a report's first item, as an earlier version did,
  let a report that starts on a vendor page and switches to the keyboard page
  through unfiltered.
- When a HID interface's report descriptor is **missing** from the capture, or
  **truncated** mid-item, `is_keystroke` returns `True` for every report on it.
  Report descriptors are only fetched during enumeration, so this hides all
  device-to-host HID traffic in captures started with the device already
  connected; `--show-keystrokes` reveals it, and `dump` labels the reason. A
  device with no configuration descriptor in the capture is not recognised as
  HID and is not filtered. Treat the filter as a safety net, not a guarantee:
  review output before sharing it.
- The `license = "GPL-2.0-or-later"` SPDX string in `pyproject.toml` requires
  setuptools ≥ 77.
- Captures come from a Windows machine running Swarm II, with the KB7 attached
  there. The keyboard sits behind its own integrated hub (`10f5:503d`), so a
  USBPcap selection has to include that hub as well as the keyboard, or the
  enumeration is missed.
