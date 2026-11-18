# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Changed
- A reload, or Apply buttons, no longer switches the keyboard back to the
  expected profile: the loop keeps the profile the keyboard was on, also one
  chosen by hand, unless a running program's profile differs. After a reload
  nothing is switched at all, so the screen no longer flashes another
  profile. After an unplug, and at the first start, the profile is chosen as
  before. Not yet checked on the keyboard.

### Added
- When the keyboard stops answering (three failed attempts in a row to set it
  up or switch its profile, while it is still connected), `run` logs that it
  should be unplugged and plugged back in, and Commander's tooltip and a
  balloon say so; the message ends as soon as the keyboard answers again. Not
  yet checked on the keyboard.

## [0.1.0-beta.3] - 2026-11-18

### Added
- `kb7screen background <image>` uploads a screen background (and the
  thumbnail the keyboard's background picker shows), framed as Swarm II does.
- `kb7screen clear-launchers <config> --profile N` empties the launcher
  buttons Swarm II left on a profile, so their picture slots are free. The
  positions the folder manages are left as they are.
- Commander's tooltip and the log of `run` name the active profile as the
  keyboard stores it ("SC", "DESKTOP") instead of only its number.
- `docs/CAPTURES-WANTED.md`: the Swarm II captures the next session should
  record (renaming a profile, the volume mixer page, saving lighting, ...).

### Changed
- Up to 15 pictures instead of 9: slots 10 to 15 are used too. A ledger
  that holds a slot above 9 is refused by older versions of kb7screen and
  Commander, which report the slot as out of range; do not go back to one
  after applying with this version.
- `apply` reads the button tables of all five profiles and leaves the picture
  slots of launchers it does not manage alone, on every page. When too few
  slots are left it refuses before uploading any picture and names those launchers.
- A slot freed by a removed button keeps its picture in the ledger, so a
  button moved to another position is not uploaded again. New pictures go to
  slots of unknown content first.

## [0.1.0-beta.2] - 2026-11-18

Commander learns to update itself. Updating from `0.1.0-beta.1` is by hand,
once: that version does not have the update feature yet.

### Added
- Commander can update itself from the GitHub releases. **Check for updates**
  in the menu (off by default) checks at start and once a day; **Check now**
  checks at once. A newer release is offered in a balloon and as
  **Update to v...**; after a Yes in the confirmation, Commander downloads it,
  checks its SHA-256, replaces itself (keeping `.old`) and restarts on the same
  folder. Not yet checked on a real desktop.

### Changed
- Release builds keep their symbol tables (no `-s -w`), to see whether fewer
  virus scanners flag them.
- Releases ship `LICENSE.txt` and the title font's licence,
  `OFL-BarlowCondensed.txt`, next to the executables.
- The READMEs say where to download the executables, how to check them, and
  that `--version` prints the version.

## [0.1.0-beta.1] - 2026-11-18

The first release: `kb7screen` and Commander as ready-built executables.

### Added
- Release builds: a version tag builds `kb7screen` for Windows and Linux and
  Commander for Windows, publishes them with their SHA-256 checksums and
  submits them to VirusTotal. `kb7screen --version` and Commander's log say
  which version runs.
- Commander, a program for the Windows notification area that keeps
  `kb7screen run` going. Its menu reloads the configuration, applies the
  buttons, switches Start with Windows and opens the configuration folder and
  the log. A balloon shows when a start or reload failed; a failed reload keeps
  the old configuration running, and a red dot on the icon means no
  configuration is running. It is built from `internal/supervisor`,
  `internal/tray`, `internal/autostart`, `internal/appdata` and
  `internal/icon`. It has been checked on the Windows 11 desktop (see
  `docs/design/commander-tray.md`, "Desktop check").
- `kb7screen stop <config>` and `kb7screen reload <config>` ask the running
  `run` for that folder to end cleanly or read its configuration again.
- One program owns the keyboard per user: a second Commander or `run` refuses
  to start, and `apply` and `restore` refuse while one of them runs.
- Lighting layers in `kb7screen`: a profile's layout file can have a `[light]`
  block with a stack of effects (`static`, `fixed`, `breathing`, `wave`,
  `ripple`, `state`), each declaring its own settings, which `run` streams to
  the LEDs. `run` reads the brightness at start and after a profile switch,
  scales the colours by the brightness notifications, keeps streaming while
  the RGB button has the LEDs off, and when the stream stops leaves the active
  profile's stored lighting (report `0x11`, read with the brightness) on the
  LEDs, or the last lit profile's layers at rest when that cannot be read.
  Buttons can flip a lighting state with `state = "name"`, with or without an
  action. The start-up brightness read has run on the keyboard without an
  error; the rest is pending hardware acceptance: whether that level is right,
  the brightness handling and the RGB button have not been tried yet.
- Reading the KB7's own key presses for the ripple and the states: Raw Input on
  Windows (measured with Star Citizen focused), `/dev/input/event*` on Linux,
  with an optional udev rule; the Linux reader has not been run on hardware.
  `kb7screen keytest` prints the keys pressed for a
  few seconds and saves nothing.
- `kb7screen light-preview`, an animated GIF of a profile's lighting with
  simulated key presses.
- `confirm = "hold <duration>"` on a button: it acts only after the finger has
  stayed on it that long.
- A run lock, `.kb7screen-run.lock`: a second `run` for a folder, and `apply`
  and `restore` while `run` holds it, refuse to start.
- Image slots 9 to 15 took uploads from Swarm II (`40-swarm-slots-pages-1`);
  15 as the highest slot is a hypothesis from Swarm II's own limit of 15
  application widgets.
- Pages 2 and 3 of the touchscreen: byte 2 of report `0x35` is the page,
  report `0x29` holds the pages one after the other, twelve entries each, and
  report `0x25` puts a marker before the twelve types of each page
  (`40-swarm-slots-pages-1`). Observed for page 2; page 3's `0x29` position
  comes from the table size only.
- `kb7screen`, a Go program in `screen/` that puts own buttons (built-in style
  or own PNG pictures) on the KB7's touchscreen per profile and carries out
  their actions on the PC: key chords, sequences with waits, programs, URLs.
  Commands: `check`, `read`, `preview`, `apply`, `restore` and `run`, which
  also switches profile when a named program starts. Builds for Windows and
  Linux.
- Application buttons: a screen button whose picture is uploaded by the host.
  The upload (`a5 41`) is 142 x 148 RGB565 followed by a one-byte-per-pixel
  alpha mask. A picture uploaded from our own code was drawn with its
  transparency, straight on the background and without any firmware tile. Its
  `0x29` label can be set (eleven characters were drawn in full) or left
  empty, which draws no text. A tap is reported as `0x33` plus a new `f1`
  notification once `0x0e` is written; the keyboard starts nothing itself.
- A key position turned into an application button from our own code, with
  its own picture: upload to an image slot, the slot in byte 3 of the `0x29`
  entry, and `0x35` type `05` / `06`. Positions can share a slot. Report `0x25`
  decoded as the page's whole type table, the same pairs as `0x35`.
- Image slots 1–9 seen, numbered across profiles, with slot 4 written from our
  own code; the top row's buttons can be application buttons too; each profile
  has its own button tables; the host switches the active profile with report
  `0x06`. Swarm II and our own code conflict over slots and labels, so only
  one of them should manage the buttons.
- The CPU temperature widget: `0x35` type `39`, drawn by the firmware, with the
  value pushed by the host as a number in an `a3` frame. The top row's buttons
  change type like any other position.
- Macro buttons: binding kind `07` in `0x29` and widget type `05` in `0x35`,
  drawn as a fixed "M" icon with the `0x29` label; Swarm II cuts the label to
  ten characters before sending it.
- Three upload pitfalls: the keyboard does not answer on interface 1 for about
  2 s after a `0x0e` write; an upload left open stops it working as a keyboard
  until it is unplugged; and `0x25` has to wait 200 ms after `0x29`.
- Second command replayed to the hardware: writing feature report `0x0e` as
  `0e 05 01 00 00` switches on the notifications that the keyboard otherwise
  only sends with Swarm II running; Fn and dial events were checked.
  `0e 05 00 00 00` switches them off again, and the setting is lost when the
  keyboard is unplugged.
- Fourth command replayed: report `0x35` on its own empties and restores a
  screen button; the keyboard redraws just that widget and the restored button
  works as before.
- Third command replayed: a background image uploaded from our own code, with
  framing verified byte for byte against Swarm II's before sending. The device
  sets the pace, so a full screen takes 32 seconds whoever sends it.
- Report `0x35` sets the widget type of one screen position; type `00` leaves
  the position empty, so the background image shows through.
- Hardware hints from the names and sizes of files in the Swarm II
  installation, marked as hypotheses: a Sonix main controller and a second
  microcontroller with its own firmware. The do-not-send list now names both
  firmware-update paths. `firmware/` is ignored so vendor files stay local.
- The clean-room rule stated precisely in the README, `CONTRIBUTING.md` and
  `docs/DEVELOPMENT.md`: the protocol is derived solely from observing USB
  traffic of hardware we own (EU Directive 2009/24/EC art. 5(3)), and Swarm II
  binaries, firmware images and assets are not analysed, decompiled or
  included.
- Screen pixel order: row by row from the top left. The second, smaller image
  upload is the keyboard's thumbnail of the background: the image squashed to
  142 x 148, followed by what is most likely a fully opaque alpha mask.
- The keyboard without Swarm II: a plain keyboard with media keys. The dial,
  the buttons beside it and the screen's default page send ordinary Consumer
  reports, the brightness and profile keys work on the device alone, and the
  extra notifications (Fn, brightness level, dial, active profile, touch
  positions) stay off until Swarm II is running. The full Consumer bitmap is
  listed from the report descriptor.
- Development notes: injected descriptors can name the wrong device address
  after a reboot, which leaves that device's traffic unfiltered.
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
- Who handles which key: the keyboard reports Fn, its own keys and the new LED
  brightness through notification `0x03`; Swarm II applies brightness by scaling
  the colours it streams, the keyboard switches its LEDs off by itself, and
  Microphone Off is carried out on the host alone. Header byte 6 of report
  `0x11` is the brightness in percent.
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
  the current brightness. Report `0x11` is stored configuration, which is why writing it only
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
- `PROTOCOL.md` gave the selector write as `05 00 <selector> 00`. Byte 1 is the
  profile: Swarm II writes every selector with `00` to `04` at startup
  (`02-swarm-start-1`, `02-swarm-start-2`), and `05 01 b9 00` / `05 01 b8 00`
  read profile 1's tables (`34-read-profile-1`).
- `kb7screen apply` could change the wrong profile. It set each position's
  type with report `0x35`, which names no profile and which the keyboard
  queues: a profile switch sent right after it made the queued changes land
  on the profile switched to (`37-kb7screen-apply-sc-1`). `apply` and
  `restore` now write each profile's whole `0x25` table and wait for the
  keyboard's full redraw before anything else is sent.
- A profile switch to the profile that is already active is not confirmed by
  the keyboard, so `apply`, `restore` and `run` timed out when the keyboard
  was already on the profile (`38-copy-factory-to-desktop-1`). Where the
  active profile is not known they now switch to another profile first; `run`
  sends no switch when a program starts or stops and its profile is already
  active.
- `PROTOCOL.md` gave `0x29` plus `0x35` as the recipe for an application
  button. It now records that `0x35` is queued and names no profile, that a
  switch to the active profile is not confirmed, and that a whole `0x25` table
  from our own code is taken over with a full redraw.
