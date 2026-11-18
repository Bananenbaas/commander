# Development notes

Working conventions for KB7 Unleashed (`commander`, until 2026-11-22 `kb7-commander`). For how captures are
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
| `site/` | The project site, plain HTML and CSS without a build step, plus `theme.js` for the light/dark switch; `.github/workflows/pages.yml` publishes it to GitHub Pages. The pictures in `site/img/` are output of `kb7screen preview` and `light-preview` |
| `screen/` | `kb7screen`, the Go program that puts own buttons on the touchscreen; usage in `screen/README.md` |
| `screen/cmd/kb7screen/` | Its command line: `check`, `read`, `preview`, `light-preview`, `apply`, `restore`, `run`, `keytest` |
| `screen/internal/` | `proto` (frames), `device` (HID on Windows and Linux), `keyboard` (one frame at a time, no retries), `picture`, `layout`, `ledger`, `apply`, `action` (keystrokes, programs), `procs`, `run` |
| `screen/internal/audio/` | The computer's sound for the volume mixer: the system volume and the programs with sound sessions on every active output device (and which of them play), and a signal when they change (Windows audio session API through direct COM calls; other systems report an error); `run` shows it on the keyboard's mixer page |
| `screen/internal/light/` | Lighting: the LED table (names, groups, positions), the effects (`fx_*.go`, each declaring its settings, listed by `light.Specs()`), the layer stack and the engine that `run` streams from |
| `screen/internal/keys/` | The KB7's key presses, for the ripple and the states: Raw Input on Windows, `/dev/input/event*` on Linux; what `kb7screen keytest` prints |
| `screen/internal/runlock/` | The lock file `.kb7screen-run.lock` that makes `apply` and `restore` refuse while `run` runs |
| `screen/internal/supervisor/` | Starts, stops and restarts `run` inside Commander, and the apply pause |
| `screen/internal/request/` | The `stop` and `reload` requests a running `run` answers (`kb7screen stop \| reload <folder>`) |
| `screen/internal/access/` | What the commands that use the keyboard share: the Swarm II check, opening the keyboard, the ledger of a folder |
| `screen/internal/tray/` | The notification-area icon, menu, tooltip and balloons (Windows) |
| `screen/internal/autostart/` | Start with Windows (the per-user Run key) |
| `screen/internal/update/` | Commander updates: release versions, picking the update from GitHub's release list, `SHA256SUMS.txt`, the checked download (no Windows calls; the swap and restart are in `cmd/commander`) |
| `screen/internal/appdata/` | Settings (`%APPDATA%\Commander\settings.toml`) and the log (`%LOCALAPPDATA%\Commander\commander.log`) |
| `screen/internal/icon/` | The helmet icon: the generator (`gen/`), the ICO and the Windows resource with the DPI manifest; `helmet-256.png` is the reference render |
| `screen/cmd/commander/` | Commander, the windowless tray program; design in `docs/design/commander-tray.md` |
| `docs/PROTOCOL.md` | Protocol knowledge base, with status tags |
| `docs/design/screen-app.md` | Design of `kb7screen` and what it still needs measured |
| `docs/CAPTURE-GUIDE.md` | How captures are made and named |
| `docs/test-images/` | PNGs with known pixel values, for upload captures |
| `captures/` | Local raw captures, git-ignored (they contain keystrokes) |

## Conventions

- **Language**: everything in the repository is English: code, comments,
  documentation, commit messages.
- **License**: GPL-2.0-or-later, for compatibility with OpenRGB. Every source
  file starts with `# SPDX-License-Identifier: GPL-2.0-or-later`; in Go files
  it is `// SPDX-License-Identifier: GPL-2.0-or-later`, followed by a blank
  line so it does not become the package comment.
- **Dependencies**: the Python toolkit `kb7tools` is **stdlib only**. No
  pyshark or tshark dependency; Python 3.10 is the minimum. `kb7screen` (Go
  1.27) uses `golang.org/x/sys`, `golang.org/x/image` and
  `github.com/BurntSushi/toml`, and nothing that needs cgo: it must build with
  `CGO_ENABLED=0`. It bundles the Barlow Condensed SemiBold font with its SIL
  Open Font License in `screen/internal/picture/fonts/`; a published
  executable must come with that `OFL.txt`.
- **No deployment pipeline**: CI runs the tests and nothing else.

## Invariants

- **Hardware safety**: only commands that have been observed in a capture are
  ever replayed. Firmware-update, bootloader and factory-reset commands are
  never implemented; they belong on the do-not-send list in `PROTOCOL.md`. No
  fuzzing of write commands.
- **Privacy**: raw captures are never committed. `PROTOCOL.md` contains only
  short, reviewed hex excerpts. Keystroke reports stay hidden by default in the
  tooling.
- **Clean room**: the protocol is derived solely from observing USB traffic of
  hardware we own (EU Directive 2009/24/EC art. 5(3)). Swarm II binaries,
  firmware images and assets are not analysed, decompiled or included.
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

For `kb7screen`, in `screen/`:

```sh
go vet ./...
go test ./...                               # no test opens the keyboard
go test -race ./...                         # needs cgo and a C compiler
go build ./cmd/kb7screen                    # kb7screen, or kb7screen.exe
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/kb7screen   # Linux build
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./cmd/kb7screen # Windows build
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags=-H=windowsgui ./cmd/commander # Commander, no console
go run internal/icon/gen/main.go            # regenerate the icon: writes cmd/commander/commander.ico and rsrc_windows_amd64.syso
```

### Releases

A release is made by pushing a tag `vX.Y.Z` (or `vX.Y.Z-beta.N` for a
pre-release) on a commit of `main` whose `CHANGELOG.md` has a section
`## [X.Y.Z…] - date`. `.github/workflows/release.yml` then tests, builds the
executables with the version stamped in (`-X main.version=<tag>`), writes
`SHA256SUMS.txt`, submits the files to VirusTotal when the repository secret
`VIRUSTOTAL` (a VirusTotal API key) is set, and publishes the GitHub release with the changelog
section, the checksums and the VirusTotal links as its notes. The executables
are not code-signed.

Compatibility contract: from v0.1.0-beta.2 on, released Commanders rely on
every later release keeping the `--updated-from <version>` argument, the asset
names `commander-windows-amd64.exe` and `SHA256SUMS.txt`, the `sha256sum` line
format with plain file names, the `vMAJOR.MINOR.PATCH[-pre]` tag scheme, and
deleting `<exe>.old` and `<exe>.new` at start. Changing any of them breaks
updates for installed Commanders.

The repository was renamed from `kb7-commander` to `commander` on 2026-11-22.
Commanders up to v0.1.0-beta.5 ask the old name for releases and reach the
new one through GitHub's redirect. That redirect ends if a repository named
`kb7-commander` is ever created again under the same account, so that name
must stay unused.

## Known gaps and pitfalls

- **USBPcap control-transfer layout: checked, and it bit us once.** USBPcap
  1.5.4 emits no DATA-stage packet for a control transfer: the payload is
  attached to the **COMPLETE** stage. The parser originally closed the transfer
  on COMPLETE without reading that payload, so every control transfer in a real
  capture came out empty and no device descriptor was ever found. Fixed, with
  `tests/synth.py` now able to build both layouts
  (`control_in(..., data_on_complete=True)`). When adding a stage, keep the
  "only if no earlier stage supplied data" guard, or a capture containing both
  layouts will count the same bytes twice.
- **Transfers are timestamped when they complete.** Interrupt IN requests are
  queued long before the device has data (Windows keeps several outstanding per
  endpoint and resubmits one the moment another completes), so a request's
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
  descriptor draws on the keyboard usage page (`0x07`) anywhere; the summariser
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
- **Injected descriptors can carry the wrong device address.** After a reboot
  of the PC, `--inject-descriptors` labelled the KB7 as device `3.5` while all
  of its traffic came from `3.4`. `summary` then shows `3.5` with descriptors
  and no traffic, and `3.4` with traffic but "(no device descriptor)", and,
  per the previous point, `3.4` is then not filtered for keystrokes at all.
  Check which address actually carries the `0x8c` reports before trusting a
  `dump` of such a capture.
- **Writing to the keyboard: respect its busy times.** After a `0x0e` write it
  redraws for about 2 s and does not answer on interface 1. An image upload must
  never be abandoned once opened: a sender that gave up after a 1 s timeout left
  the keyboard waiting inside the upload, and it stopped working as a keyboard
  until it was unplugged. Use timeouts of several seconds, and wait 200 ms
  between `0x29` and `0x25` as Swarm II does. See "Application buttons" in
  `PROTOCOL.md`.
- **The lighting stream: nothing for 2.5 s after `0x0e`, and never let a
  write stall.** The keyboard does not answer on interface 1 for about 2 s
  after `0x0e`, so `run` sends no stream frame within 2.5 s of enabling the
  notifications. A stream write that times out returns `ErrStreamStalled`;
  `run` then closes the keyboard and opens it again at the next poll, as it
  does on any stream error other than a missed acknowledgement. `apply` and
  `restore` must never run alongside a stream (the run lock). A lighting frame
  set must never be left half-written: one missing acknowledgement must not
  stop the remaining banks (`StreamLights` writes all six), or the keyboard
  apparently waits for the rest and the LEDs freeze until it is unplugged.
- **Unplug and replug after an interrupted upload.** When `kb7screen` reports
  `keyboard.ErrUploadLeftOpen` (with a banner on the command line), the upload
  stopped after its opening frame may have been accepted. The keyboard may be
  waiting for the rest of the picture and does not work as a keyboard until it
  is unplugged and plugged back in; nothing sent from the PC fixes that, and
  the same session sends no further upload. The ledger has stopped trusting
  that slot, so the next `apply` sends the picture again.
- **Ctrl+C during `apply` stops between pictures.** `apply` and `restore`
  catch Ctrl+C and the terminate request (on Windows also sent when the
  console is closed). An upload is never cut short: `apply` stops before the
  next picture or profile, after writing the tables for the pictures already
  sent, and the ledger is saved; `restore` stops only before it touches the
  keyboard. Windows ends a program a few seconds after its console is closed,
  which can still be too soon for an upload. New keyboard work in `apply`
  must check its context only at such points. `background` catches the same
  signals and lets its upload run to the end before it reports the stop.
- **Tests of the per-user lock skip when a Commander or run is live.** The
  lock is per user and machine (a named mutex on Windows, a `flock` file on
  Linux), so those tests skip with a message when a real Commander or `run`
  holds it. Quit it before running `go test ./...` if you want them to run.
- **A test that shows nothing needs a control in the same capture.** "No
  report on tap" means nothing unless the same capture also shows a known
  event getting through (for example a dial turn with `0x0e` written).
- The `license = "GPL-2.0-or-later"` SPDX string in `pyproject.toml` requires
  setuptools ≥ 77.
- Captures come from a Windows machine running Swarm II, with the KB7 attached
  there. The keyboard sits behind its own integrated hub (`10f5:503d`), so a
  USBPcap selection has to include that hub as well as the keyboard, or the
  enumeration is missed.
