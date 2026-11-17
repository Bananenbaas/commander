# kb7screen — own buttons on the KB7 touchscreen

Design for a program that puts my own buttons on the KB7's screen, per
profile, and carries out what they do without Swarm II. Status: draft for
approval.

## Intent

The KB7's screen can show buttons, but Swarm II draws them in Turtle Beach's
style, cuts names to ten characters and offers no way to change the icons. The
measurements in [`PROTOCOL.md`](../PROTOCOL.md) ("Application buttons") show
that an application button can carry a picture sent by the host, with its own
transparency, an optional label, no firmware graphics, and a touch report the
host can act on.

`kb7screen` uses that to give each button my own picture and title, and to run
an action on the PC when it is tapped. Each of the keyboard's five profiles can
get its own set of buttons: desktop functions on the desktop profile, and a
game profile that is switched to automatically when the game starts. The first
game is Star Citizen: buttons for multi-key commands that are awkward to type
in flight — a key combination, a sequence of keys, or a sequence with a pause
in it (power on, wait for it to boot, then engines on).

Swarm II stays installed for what only it does, such as firmware updates. It
just no longer manages the screen buttons.

## Goals

- A configuration folder holds one layout file per profile plus the pictures.
  One command puts all of it on the keyboard.
- A program running on the PC catches the taps on every managed profile and
  carries out each button's action: keys, combinations, sequences with waits,
  starting a program, opening a website, file or folder; as a tap, or a key
  held for as long as the finger stays on the button.
- It switches the keyboard to a profile when a given program starts (the game)
  and back when it ends.
- One executable for Windows, and the same code built for Linux. Nothing to
  install besides the executable.
- A built-in simple button style (coloured frame with a cut corner, a symbol,
  the title in a condensed font), and own PNG pictures as an alternative.

## Non-goals for now

- A graphical editor. It comes later, on top of the layout files.
- Actions named after game functions (e.g. "landing gear") that follow the
  game's key bindings. Actions are keys for now.
- Lighting, live values (CPU temperature, volume), the media page, and choosing
  the background image (a one-time step in the keyboard's own menu).
- Running at the same time as Swarm II.

## Anti-goals

These would make it a failure even if it works:

- Sending any command that is not in `PROTOCOL.md`, and anything on the
  do-not-send list (firmware update, bootloader, factory reset).
- Leaving an image upload open. The keyboard stops working as a keyboard until
  it is unplugged.
- Re-uploading pictures that have not changed. Pictures are stored on the
  device; uploads cost time and probably wear its memory.
- Cutting titles short.
- Shipping Turtle Beach or game files, or a non-redistributable font.
- Putting anyone's personal layouts, macros or pictures in this public
  repository. Those live in a separate, private repository.

## Constraints (all measured unless marked)

From `PROTOCOL.md`, "Application buttons" and the sections it links to:

| Step | What is sent |
|---|---|
| Picture | upload opener `a5 (0x40 + slot)`, 142 x 148 RGB565 little-endian followed by 142 x 148 one-byte alpha, framed in 4096-byte blocks as documented; every frame acknowledged |
| Bind | report `0x29` of the profile: the position's entry `00 01 0b <slot> <label or 12 x 00>`, checksum recomputed |
| Type | 200 ms later, report `0x35`: `35 0c 01 <position> 01 00 05 00 06 00 <sum16>`; the keyboard redraws that position |
| Type table | report `0x25` of the profile, the whole page's types; Swarm II writes it 200 ms after `0x29` |
| Notifications | report `0x0e` `0e 05 01 00 00`, once after every plug-in; it is lost on unplugging |
| Tap | report `0x33` `33 01 <position> 05 00 00 01 <down>` plus notification `03 00 f1 01 <id16> <down> 00`, touch and release |
| Profile | report `0x06` `06 04 <profile> 05` switches the active profile; notification `03 00 01 <profile>` reports any switch |
| Read | `05 <profile> <selector> 00`, then read report `0x05` (ready), then the block: `b9` for `0x29`, `b8` for `0x25`, `ba` for the profile names |

Further constraints:

- **Timing.** After `0x0e` the keyboard does not answer on interface 1 for
  about 2 s. Wait at least 5 s for each acknowledgement. Never abandon an
  upload once its opener has been echoed. Leave 200 ms between `0x29` and the
  next report.
- **An upload takes about 2.7 s** and its picture survives unplugging.
- **Image slots** are numbered across all profiles. Slots 1–9 have been seen;
  the highest slot is **not measured**, and with several profiles of up to 36
  buttons it decides how many distinct pictures there can be.
- **Pages.** Each profile has three pages of buttons and a media page, browsed
  on the keyboard itself. How pages 2 and 3 are addressed is **not measured**;
  the `0x29` table has room for 35 entries and `0x25` for 38, and byte 2 of
  `0x35` / byte 1 of `0x33` have only ever been `01`.
- **Writing to a profile that is not active is not measured.** Swarm II
  switches to a profile with `0x06` before writing its tables.
- **The active profile cannot be read**; it is only reported when it changes.
- **Taps carry no profile**, only position and kind.
- **The top row** (positions 0–2) can be application buttons. Its original
  types (`3b`, `3e`, `48`) have only been seen in `0x25`, never sent as `0x35`.
- **Labels** are at most eleven ASCII characters, drawn in a thin firmware font
  inside the picture area near its bottom edge. The built-in style draws the
  title into the picture and leaves the firmware label empty.
- **Swarm II.** Starting it left uploaded pictures and an empty label in
  place. Changing buttons in it rewrites the tables: it hands out slots from
  its own count and turns positions it does not know into slot `00` with an
  empty label. It also receives the taps when running.
- **The picture area is 142 pixels wide**, a little narrower than a grid cell
  (about 160), so these buttons stand slightly apart.
- **Fonts.** The title font is bundled with the program and must be under a
  licence that allows that: Barlow Condensed (SIL Open Font License).
  Bahnschrift, used in the trial, is part of Windows and cannot be bundled.
- **Linux is not measured.** The plan there is `/dev/hidraw*` for the keyboard
  and `/dev/uinput` for keystrokes, which needs udev rules.
- **Keystrokes reaching the game** through the operating system's input
  injection is not measured.

## Approaches considered

| Approach | For | Against |
|---|---|---|
| **Go** (chosen) | One executable without a runtime, built for Windows and Linux from one machine. The keyboard can be driven without C libraries: `hid.dll` / SetupAPI on Windows, hidraw ioctls on Linux; keystrokes through `SendInput` / uinput. PNG decoding is in the standard library | A second language next to the Python research tools; a font renderer is an extra module |
| Rust | Same single binary, stricter | Slower to build; cross-building a Windows executable is more work |
| Python, standard library only | Everything measured so far already runs this way | A Windows executable needs a packager such as PyInstaller, which virus scanners often flag; image scaling and drawing by hand |
| Python with packages (Pillow etc.) | Fast to write | Installs; breaks the standard-library rule of `kb7tools` |
| C# / .NET | The Stream Deck plugins' MIT-licensed binding code could be reused | A new language, and a rewrite of everything measured so far |

## Decided

- Layout files and a folder of pictures first; a graphical editor later.
- Go, for a Windows executable and a Linux build.
- The simple style for now; a fancier one later.
- The program lives in this repository; personal layouts, pictures and macros
  live in a private repository of their own.
- Paging is the keyboard's own; the program does not add page buttons.
- kb7screen manages all picture slots. Swarm II is still used for what only it
  does, but not for screen buttons, and not at the same time.
- The whole page may be used, top row included: 12 buttons per page.
- One layout per profile; desktop functions on the desktop profile, the game
  profile switched to automatically while the game runs.

## Design

### Where

A Go module in `screen/` of this repository, producing the executable
`kb7screen`. The Python `kb7tools` is untouched. Personal configuration lives
in a separate folder (in my case the private repository), passed on the
command line.

### Commands

| Command | Does |
|---|---|
| `kb7screen check` | finds the keyboard, reports whether Swarm II is running, reads the profile names |
| `kb7screen read --profile N` | prints a profile's button table (`0x29` and `0x25`) |
| `kb7screen preview <config> [--out DIR]` | writes the rendered pictures as PNG files (`<profile>-<page>-<position>.png`, and one `panel-<profile>.png` per profile), touching nothing else |
| `kb7screen apply <config> [--full]` | puts every layout of the folder on the keyboard; `--full` re-uploads every picture |
| `kb7screen restore <config> --profile N` | gives the profile's positions back their recorded original contents |
| `kb7screen run <config>` | writes `0x0e`, switches profiles with programs, carries out taps until stopped |

`apply` and `restore` refuse to start while Swarm II or its device service is
running. `run` instead waits for them to close, see below.

### The configuration folder

```
config/
  desktop.toml            one file per managed profile
  sc.toml
  pictures/  symbols/  fonts/
  .kb7screen-state.json   the slot ledger; not under version control
```

A layout file:

```toml
profile = 1                      # which of the five profiles this file owns
switch_when_running = "StarCitizen.exe"   # optional

[[button]]
page = 1
position = 3                     # 0-11, row by row from the top left
title = "Flight Ready"
color = "#00AAFF"
symbol = "symbols/power.png"     # white-on-transparent symbol, drawn in `color`
action = ["U", "wait 600ms", "I"]

[[button]]
page = 1
position = 5
picture = "pictures/eject.png"   # own picture instead of the built-in style
action = ["AltGr+Y"]
mode = "hold"                    # "tap" (default) or "hold"

[[button]]
page = 1
position = 0
title = "Browser"
color = "#FFAA00"
action = ["open https://example.org"]
```

- There is no `font` key in this version: the title font is the bundled one.
- A button has either `symbol` (built-in style) or `picture` (own PNG of any
  size, fitted into 142 x 148 keeping its alpha). Without either, the built-in
  style draws only the frame and the title.
- `label` (optional, at most eleven ASCII characters) sets the firmware label;
  it is empty by default.
- **Action steps**:
  - key chords (`LAlt+N`, `AltGr+Y`, `Period`, `VolumeUp`). Key names cover
    letters, digits, F1–F24, punctuation by name, arrows, the numeric keypad,
    media keys and the modifiers `LCtrl RCtrl LShift RShift LAlt RAlt LWin
    RWin`. `AltGr` means `LCtrl+RAlt`, which is what Windows produces for it on
    keyboard layouts that have AltGr.
  - `wait <n>ms`
  - `run "<program>" [arguments]` starts a program
  - `open <url, file or folder>` opens it with its default handler
- **Tap** presses each chord and releases it 40 ms later, 20 ms between steps.
  **Hold** presses the chord when the button is touched and releases it when
  the touch ends; a hold button has exactly one chord.
- Page 1 only until pages 2 and 3 are measured; layouts with other pages are
  refused until then.

### The slot ledger

One per configuration folder, i.e. per keyboard. It records per slot the hash
of what was last uploaded and which profile, page and position use it, and per
managed position its **original** `0x29` entry and `0x25` type and value, saved
the first time kb7screen takes the position over.

- Slots are allocated from the lowest free one, across all profiles, and kept
  stable, so unchanged pictures are never sent again. Buttons with identical
  pictures share a slot.
- Until the highest slot is measured, allocation stops at 9 and refuses beyond
  it. Apply counts the different pictures of all layouts before it touches the
  keyboard and refuses a configuration that needs more pictures than slots.
- **A missing or stale ledger is safe**: pictures cannot be read back, so
  without a matching ledger entry a picture is simply uploaded again. Apply also
  reads each managed profile's `0x29` and re-sends whatever differs from the
  layout, which repairs the tables after Swarm II has changed them.

### Apply

`Apply` returns a `Report` (pictures uploaded and skipped, positions restored,
notes) and an error. It first renders every picture, so a bad file or a title
that does not fit stops it before anything is sent. It then works in two
passes over the managed profiles (those of the configuration and those the
ledger still holds positions in), in ascending order, and saves the ledger
during the run: after the first pass and after each profile of the second. The
caller saves it once more when `Apply` returns, whether or not it failed.

First pass, for each profile:

1. Switch to it with `0x06` and wait for `03 00 01 <profile>` (as Swarm II
   does before writing a profile).
2. Read its `0x29` and `0x25`.
3. A position that was managed but is no longer in the layout gets its
   original contents back (as in `restore`), which frees its slot.
4. Record originals for positions taken over for the first time. Stop
   trusting the slot of a position that no longer shows it.

Second pass, for each profile with buttons:

5. Assign slots; upload the pictures whose slot does not hold the same hash.
6. Write `0x29` once with all changed entries; wait 200 ms; write `0x35` for
   each changed position (that also makes the keyboard redraw it).

Finish on the profile that `run` would choose: the game's profile if its
program is running, otherwise the desktop profile (the lowest managed profile
without `switch_when_running`; when every layout has one, the lowest profile
that no layout gives a program, which then has no layout).

Within one apply or restore, a switch to the profile the keyboard last
confirmed is not sent again, unless the input that has arrived since reports
another profile (someone may have switched on the keyboard itself) or cannot
be read.
The first switch is always sent, as the active profile cannot be read.

### Restore

Writes the recorded original `0x29` entries, then — 200 ms later — the
profile's `0x25` with the recorded original types and values, the way Swarm II
writes it. Positions without a recorded original are emptied (type `00`). This
path has not been replayed yet and must be tested before it is relied on.

### Run

- Open the keyboard, write `0x0e`, wait for the `31` redraw.
- Switch to the profile apply would finish on, so the active profile is known;
  from then on track it from `03 00 01 <profile>`.
- Every two seconds, check the programs named in `switch_when_running`: switch
  to that profile when one starts, back to the desktop profile when it ends.
- On a `0x33` touch: look up the button by active profile and position. Start
  its action (tap) or press its chord (hold). On release: release the hold
  chord. Taps on positions no layout manages are ignored, and so are touches
  of any kind but `0x05`: a position the keyboard shows as a key or another
  widget is left to the keyboard. Kind `0x05` is the widget type of
  application buttons, but modifier and macro widgets have it too (values
  `01` and `02` against `06` in `0x25`), and a touch does not carry the value,
  so it cannot tell them apart.
- A profile switch the keyboard does not confirm in time is assumed to have
  happened. Actions run one at a time in order, on a worker, so reading the
  keyboard does not wait for an action, except when held keys must be
  released (before a profile switch or `0x0e`, on a profile change, when the
  keyboard is closed and when `run` stops): the release comes after the
  actions queued before it, and reading waits until they have finished.
- **Swarm II is polled** every two seconds, like the programs above. While
  Swarm II or its device service runs, `run` releases every held key, closes
  the keyboard and does nothing else; when they have closed, it reopens the
  keyboard, writes `0x0e` and switches to the expected profile. On resuming it
  reads the managed profiles' `0x29` (reading does not need a profile switch)
  and, if Swarm II changed them, says so and advises `apply`; it does not
  rewrite them by itself.
- If the keyboard is unplugged, wait for it, then write `0x0e` again and switch
  to the expected profile.
- Ctrl+C stops cleanly and releases every key still held.

### Pictures

- RGB565 little-endian plus straight (not premultiplied) alpha, as in Swarm
  II's uploads and the confirmed test pictures.
- Built-in style, as in the trial on the keyboard (rendered 4x and box-filtered
  down; coordinates in the 142 x 148 picture):
  - frame: box from (12, 3) to (130, 111), 4-px stroke in the button's colour,
    lower-right corner cut by 20 px, with two accent bands outside the cut at
    5–9 and 13–17 px from it
  - inside of the frame: black at 82% opacity
  - symbol: inside the frame, in the button's colour
  - title: white, Barlow Condensed SemiBold, 23 px, centred in the band from
    y = 114 to 146, with a black outline of radius 2 px at 90% opacity
  - a title that does not fit is set smaller, down to 17 px; if it still does
    not fit, apply refuses with a message. Titles are never cut.

### Keystrokes

Windows: `SendInput` with scan codes (extended flag where needed, e.g. Right
Alt). Linux: a uinput virtual keyboard.

### Safety

- Only the frames listed under Constraints, built by one tested module.
- Upload: an opener that is not echoed exactly ends the upload before any
  data; once it is echoed, the upload runs to `a5 ff` or the program reports
  that the keyboard must be unplugged.
- Ctrl+C, or a request to terminate (on Windows also closing the console),
  during `apply` or `restore` never cuts an upload or a profile's table writes
  short: `apply` stops before the next picture or profile, after writing the
  tables of the pictures already sent, and the ledger is saved.
- No retries of writes that failed. The only writes repeated are the
  reconnect writes, `0x0e` and `0x06`, which are sent again on every connect
  and reconnect; they are idempotent.

## Testing

- Unit tests for every frame builder and the upload framing, against test
  vectors taken from the frames recorded in `PROTOCOL.md` (raw captures are
  never committed): `0x35` and `0x29` checksums, the `0x06`/`0x0e`/`0x05`
  frames, and the framing of a full 63,048-byte picture (1065 frames, block and
  frame sizes as documented).
- Unit tests for picture encoding, layout parsing, action parsing and slot
  allocation.
- CI: `go vet` and `go test` on Windows and Linux; build both executables.
- A manual hardware checklist for each release: apply, tap, hold, profile
  switch by program, unplug and replug during `run`, restore.

## Must be measured before relying on it

In this order:

1. The highest image slot.
2. Writing `0x29` and `0x35` right after switching profile with `0x06`.
3. Restoring a position with `0x25`, top row included.
4. How pages 2 and 3 are addressed — one capture of Swarm II putting a button
   on each.
5. Reading the touch and notification reports from our own code. Every tap so
   far was seen in a capture, not read by a program of ours.
6. Whether keystrokes from `SendInput` reach Star Citizen, and how AltGr
   chords arrive.
7. The keyboard on Linux (hidraw nodes, feature reports, the interface 1
   stream).
8. Whether the keyboard confirms a switch to the profile that is already
   active. Until that is measured, `run` assumes an unconfirmed switch has
   happened, and `apply` and `restore` do not send a switch to the profile
   the keyboard last confirmed.

## Left to the builder

Package structure, logging, error wording, the exact default timings beyond
those above, the TOML library and the font renderer.
