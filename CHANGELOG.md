# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Fixed
- Transfers were timestamped and ordered by when the request was queued rather
  than when it completed. For interrupt IN that is wrong by up to the gap
  between two reports, so touch events appeared as duplicates and the order of
  reports within a dial click was reversed. Transfers now carry their
  completion time, with the queueing time kept in `Transfer.submitted`.
- The keystroke filter could still leak. A report was classified by the usage
  page in effect at its first main item only, so one that starts on a vendor
  page and switches to the keyboard page passed as vendor data, and usage pages
  carried in four-byte usages were ignored. Every page a report uses is now
  recorded, and when a HID interface's report descriptor is missing from the
  capture or truncated, its reports are hidden rather than shown.
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
- The RGB lighting format: report `0x11` over the control endpoint, a 112-LED
  table stored as seven blocks of sixteen reds, greens and blues, and a
  trailing 16-bit sum as its checksum.
- Touch events: report `0x33` reports a screen button being touched and
  released, with the button's key in it, and the keyboard sends that key
  itself. The dial's volume-down bit, and the boot-complete notification
  about 4.9 s after plug-in. Report `0x33` carries the button's position on
  the page, confirmed by prediction on two more buttons, and the mute loop
  identifies the mute byte of the volume frame and the mute-state frame.
- Default key and button functions from Turtle Beach's public support
  article, cited as such: the action bar's buttons now have names in the LED
  map, and the empty LED slots line up with the extra keys of the ISO and
  Japanese layouts the KB7 is sold in.
- Background image upload: `a5` frames on interface 1 carry a 480 x 800 RGB565
  image in 4096-byte blocks, every frame acknowledged by the device, at about
  23 KiB/s. The screen resolution and pixel format follow from it.
- Profile switching: the keyboard steps through its five slots itself when the
  profile name on the screen is tapped, announces each switch with notification
  `03 00 01 <slot>`, and Swarm II only follows by resuming its streams.
- Screen buttons and profiles: report `0x29` is the page's button table —
  binding and label per position, with the same checksum as the lighting
  block — and configuration blocks carry one of five profile slots in byte
  3, with all five profile names in report `0x16`. A button bound to a
  modifier on its own reports through notification `0x03`, not as a key.
- The complete LED map: 94 of the 112 slots identified, including the arrow
  keys, the top buttons and the ten-segment wrist rest strip, with the matrix
  shown to be wired for an ISO layout. With Swarm II closed, a written report
  `0x11` is what the keyboard displays.
- Lighting driven from our own code: streaming `a1` frames at 30 Hz rendered
  on the keyboard, contending with Swarm II's 20 Hz for a 56% share of the
  frames. Whoever streams last owns the LEDs.
- What actually drives the lighting: the `a1` stream on interface 1 carries the
  live 112-LED colour table, in the same layout as report `0x11` and scaled by
  255/256. Report `0x11` is stored configuration, which is why writing it only
  makes the keyboard flicker before the stream wins.
- The LED addressing scheme, proved by lighting a single key: an LED's red,
  green and blue bytes sit sixteen apart inside its 48-byte block, and
  Escape is LED index 0.
- First command replayed to the hardware: an `a3` volume frame written to
  interface 1 set the level shown on the keyboard's screen, promoting that
  frame from `observed` to `confirmed`. Documented with the Windows HID
  details needed to repeat it.
- The volume dial's full round trip: the device reports a relative step as a
  Consumer bitmap plus a notification, and the host pushes the resulting
  absolute level back about 9 ms later. The screen is a slave display for
  host-side state.
