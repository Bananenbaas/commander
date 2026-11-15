# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed
- Keyboard reports on a vendor-defined interface were printed in full. Hiding
  only looked at boot-keyboard interfaces, but the KB7 also exposes a keyboard
  report on interface 2 alongside unrelated reports, so typed text could appear
  in `dump` output without `--show-keystrokes`. Hiding is now decided per report
  ID from the HID report descriptor, which keeps the other reports on that
  endpoint visible.
- USBPcap control transfers lost their payload. USBPcap 1.5.4 attaches the data
  of a control transfer to the COMPLETE stage and emits no DATA stage at all, so
  every control transfer in a real capture was parsed as empty and no device or
  configuration descriptor was ever recognised. `tests/synth.py` can now build
  both layouts, and the device-inventory test runs against each.

### Added
- `kb7cap` capture toolkit (stdlib only): pcap/pcapng reader, USBPcap and
  Linux usbmon transfer pairing, device/configuration/string/HID report
  descriptor parsing, and `summary`, `dump` and `diff` commands.
- Boot-keyboard reports are hidden by default in `dump` and `diff`.
- Capture guide for Phase 0, protocol notes skeleton, PNG test images with
  known pixel values.
- CI: unit tests on Python 3.10 and 3.12; Dependabot for GitHub Actions.
- Project documentation: `CONTRIBUTING.md`, `SECURITY.md`,
  `CODE_OF_CONDUCT.md` (Contributor Covenant 2.1) and
  `docs/DEVELOPMENT.md`.
- Issue forms for bug reports, protocol findings, capture contributions and
  open questions, plus a pull request template. All of them refuse raw
  captures.
- First real protocol notes: KB7 device identity (`10f5:5038`, bcdDevice
  `0x0139`, integrated hub `10f5:503d`) and the full interface/endpoint map —
  four HID interfaces, three of them with an OUT endpoint.
- Capture guide: the `00-descriptors` shortcut for pulling device and
  configuration descriptors without a full plug-in capture.
- The Swarm II startup exchange: a selector register (report `0x05`) that
  selects one of nineteen configuration blocks, whose report IDs and lengths
  match the report map exactly. Plus the `a1` state stream and `a3` frames on
  interface 1, and the device's own notification report `0x03`.
- The system-volume frame on interface 1, verified by predicting where the
  level would appear and then capturing a slider drag. Live PC values reach
  the screen as numbers, not as rendered images.
- The volume dial's full round trip: the device reports a relative step as a
  Consumer bitmap plus a notification, and the host pushes the resulting
  absolute level back about 9 ms later. The screen is a slave display for
  host-side state.
