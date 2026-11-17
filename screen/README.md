# kb7screen

`kb7screen` puts your own buttons on the touchscreen of the Turtle Beach
Command Series KB7 and carries out what they do on your computer, without
Swarm II. Each of the keyboard's five profiles can have its own buttons, with
your own pictures or a built-in style (a coloured frame, a symbol and a title
in a condensed font). A button presses a key or a combination, plays a sequence
of keys with pauses, starts a program, or opens a website, file or folder. The
program can switch the keyboard to a profile while a given program runs, for
example a game.

The design and the measurements behind it are in
[`docs/design/screen-app.md`](../docs/design/screen-app.md) and
[`docs/PROTOCOL.md`](../docs/PROTOCOL.md).

## Build

You need Go 1.27 or newer. There are no other dependencies to install and no C
compiler is needed.

```sh
cd screen
go build ./cmd/kb7screen
```

This produces `kb7screen` (`kb7screen.exe` on Windows). To build the Linux
executable from another system:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./cmd/kb7screen
```

## Commands

```
kb7screen <command> [flags] [config-folder]
```

| Command | Does |
|---|---|
| `kb7screen check` | reports whether Swarm II runs; if not, finds the keyboard and prints its five profile names |
| `kb7screen read --profile N` | prints the button table of profile N (0 to 4): for each position 0 to 11 the `0x29` entry (modifier, key, kind, slot, label) and the `0x25` type and value |
| `kb7screen preview <config> [--out DIR]` | writes the rendered pictures as PNG files, exactly as `apply` would send them. Default folder: `<config>/previews`. Touches nothing else |
| `kb7screen apply <config> [--full]` | puts every layout of the folder on the keyboard. `--full` sends every picture again |
| `kb7screen restore <config> --profile N` | gives the positions kb7screen took over in profile N their original contents back |
| `kb7screen run <config>` | carries out the buttons' actions until you press Ctrl+C |

`-h` or `--help` prints the usage. The exit code is 0 on success and 1 on an
error; the message goes to standard error, prefixed with `kb7screen:`.

`preview` writes one PNG per button, named `<profile>-<page>-<position>.png`
(with transparency), and per profile a `panel-<profile>.png` that shows the 12
positions as a grid of 3 columns and 4 rows on a dark background, empty
positions left dark. It is the quickest way to check titles and symbols before
anything is sent to the keyboard.

`apply` and `restore` report which pictures were uploaded and which were
already in place, which positions were restored (as `profile P page G position
N`), and any notes. Pictures that did not change are never sent again, so a
second `apply` of an unchanged folder is fast.

## The configuration folder

```
config/
  desktop.toml            one file per managed profile
  game.toml
  pictures/               own pictures (PNG, any size)
  symbols/                symbols (PNG, white on transparent)
  .kb7screen-state.json   the slot ledger, written by kb7screen
```

Every `*.toml` file directly in the folder is the layout of one profile.
Paths inside a file are relative to the folder. The ledger records which
picture is in which of the keyboard's picture slots and what each position held
before kb7screen took it over, so `restore` can give it back. Keep it with the
configuration, and do not put it under version control. Without it `apply`
still works, but sends every picture again, and `restore` has no record of
which positions it may give back.

### A complete example

`game.toml`:

```toml
profile = 1                          # which of the five profiles (0 to 4) this file owns
switch_when_running = "game.exe"     # optional: switch to this profile while it runs

[[button]]
page = 1                             # only page 1 is supported
position = 0                         # 0 to 11, row by row from the top left
title = "Power"
color = "#00AAFF"                    # #RRGGBB, default #00AAFF
symbol = "symbols/power.png"         # white-on-transparent symbol, drawn in the colour
action = ["P", "wait 600ms", "I"]

[[button]]
page = 1
position = 5
picture = "pictures/boost.png"       # own picture instead of the built-in style
action = ["LShift"]
mode = "hold"                        # "tap" (default) or "hold"
```

Each file needs a `profile`. Each button needs `page`, `position` and at least
one step in `action`. Buttons without `symbol` or `picture` get the frame and
the title only.

| Key | Meaning |
|---|---|
| `title` | drawn into the picture in a condensed font, 23 px, set smaller (down to 17 px) when it is long. A title that still does not fit is refused, never cut |
| `color` | colour of the frame and the symbol |
| `symbol` | PNG drawn inside the frame in the button's colour |
| `picture` | own PNG of any size, fitted into 142 x 148 pixels keeping its transparency. Cannot be combined with `symbol` |
| `label` | optional firmware label, at most 11 printable ASCII characters. Empty by default |
| `action` | the steps, see below |
| `mode` | `tap` presses the steps once; `hold` keeps one key chord down for as long as the touch lasts |
| `confirm` | `"hold 1s"`: the button acts only once the finger has stayed on it that long (200 ms to 5 s), so an accidental touch does nothing. Not with `mode = "hold"`, and needs an `action` |

At most 9 different pictures can be on the keyboard at once; buttons with an
identical picture share one slot. `apply` refuses a folder that needs more.

### Action steps

An action is a list of steps, carried out in order.

- A **key chord**: keys joined with `+`, pressed together and released, for
  example `F5`, `LCtrl+C`, `AltGr+Y`, `Period`, `VolumeUp`.
- `wait <n>ms`: pause, 1 to 60000 ms.
- `run "<program>" [arguments]`: start a program. Double quotes group words.
- `open <target>`: open a website, file or folder with its default handler.

A tap presses each chord and releases it 40 ms later, with 20 ms between steps.
A `hold` button has exactly one step, a key chord: it is pressed when the
button is touched and released when the finger lifts.

Key names are not case sensitive:

- letters `A` to `Z`, digits `0` to `9`
- `F1` to `F24`
- `Enter`, `Escape`, `Backspace`, `Tab`, `Space`, `CapsLock`
- punctuation by name: `Minus`, `Equals`, `LeftBracket`, `RightBracket`,
  `Backslash`, `Semicolon`, `Quote`, `Grave`, `Comma`, `Period`, `Slash`,
  `NonUSBackslash`
- `PrintScreen`, `ScrollLock`, `Pause`, `Insert`, `Delete`, `Home`, `End`,
  `PageUp`, `PageDown`, `Up`, `Down`, `Left`, `Right`
- the keypad: `Num0` to `Num9`, `NumLock`, `NumDivide`, `NumMultiply`,
  `NumMinus`, `NumPlus`, `NumEnter`, `NumDecimal`
- media: `VolumeUp`, `VolumeDown`, `Mute`, `PlayPause`, `NextTrack`,
  `PrevTrack`, `StopMedia`
- modifiers: `LCtrl`, `RCtrl`, `LShift`, `RShift`, `LAlt`, `RAlt`, `LWin`,
  `RWin`
- `AltGr`, which means `LCtrl+RAlt`, as Windows produces it on keyboard
  layouts that have an AltGr key

## How it works

`apply` renders every picture before it touches the keyboard, so a bad file or
a title that does not fit stops it before anything is sent. It then reads each
profile's tables, records what the managed positions held, uploads the pictures
that are not yet in their slots and writes the button tables. Each profile's
tables are written whole, and `apply` waits until the keyboard has redrawn its
screen before it switches to another profile: the keyboard takes the tables
over some time after they are sent, and a switch before that could change the
wrong profile. If the redraw does not come within 5 s, `apply` stops with an
error; running it again writes the tables again. When it succeeds, it ends on the profile that `run` would choose: the one whose program
is running, otherwise the desktop profile.

The desktop profile is the lowest profile whose layout has no
`switch_when_running`. When every layout has one, as in a folder with only the
`game.toml` above, it is the lowest profile that no layout names (profile 0 in
that example); that profile has no buttons of kb7screen, and touches there are
left to the keyboard.

`run` waits for the keyboard, enables its notifications, switches to the
expected profile and then carries out the touches of buttons it manages.
The keyboard does not confirm a switch to the profile that is already active,
so where the active profile is not known (the first switch of `apply` and
`restore`, and when `run` connects) the switch goes through another profile
first. Every two seconds `run` checks the programs named in
`switch_when_running` and switches profile when one starts or ends, unless
that profile is already active. Only touches of application buttons
act; a position that the keyboard shows as something else is left to the
keyboard. If the keyboard is unplugged, `run` waits for it and sets it up
again.

`run` reads the configuration folder once, when it starts. After changing the
folder, stop `run` with Ctrl+C, run `apply`, and start `run` again so that it
acts on the new buttons.

`run` holds a lock file, `.kb7screen-run.lock`, in the configuration folder.
A second `run` for the same folder refuses to start, and `apply` and `restore`
refuse while `run` holds the lock, because its lighting stream and the picture
uploads share the keyboard's interface 1. The system frees the lock when `run`
ends, however it ends.

## Safety

- **Close Swarm II first.** `check`, `read`, `apply` and `restore` refuse to
  run while Swarm II or its device service is running. Swarm II rewrites the
  button tables and would receive the taps itself. `run` does not refuse: it
  pauses, releases every held key and closes the keyboard while Swarm II runs,
  and carries on when Swarm II is closed. If Swarm II changed the buttons in
  the meantime, `run` says so; run `apply` again.
- **Never unplug the keyboard during `apply`.** A picture upload takes about
  2.7 seconds and must run to the end.
- **Ctrl+C during `apply`** does not cut a picture short: `apply` finishes the
  picture it is sending, writes the buttons of that profile whose pictures are
  in place, saves the ledger and ends with `stopped after the current picture;
  the ledger is saved`. Run `apply` again to finish the rest. `restore` stops
  only if it has not changed anything yet. Closing the console window asks
  for the same stop, but Windows ends the program a few seconds later, which
  may be too soon; use Ctrl+C.
- **"Unplug and replug."** If an upload is interrupted, the keyboard may stay
  in upload mode and stops working as a keyboard. `apply` then prints a
  prominent message. Unplug the keyboard, plug it back in, and run `apply`
  again; the ledger remembers which pictures did not arrive.
- Only frames that are documented in `docs/PROTOCOL.md` are sent. Nothing that
  could touch firmware updates, the bootloader or a factory reset is
  implemented.
- `kb7screen` writes only to the buttons you configured and to its own ledger.
  `restore` gives positions back what they held before.
- `Ctrl+C` stops `run` and releases every key still held.

## Linux

The same code builds for Linux. It talks to the keyboard through
`/dev/hidraw*` and sends keystrokes through `/dev/uinput`. Your user needs
read and write access to both, which normally takes udev rules, for example:

```
# /etc/udev/rules.d/70-kb7screen.rules
KERNEL=="hidraw*", ATTRS{idVendor}=="10f5", ATTRS{idProduct}=="5038", MODE="0660", TAG+="uaccess"
KERNEL=="uinput", MODE="0660", TAG+="uaccess", OPTIONS+="static_node=uinput"
```

Reload them with `sudo udevadm control --reload && sudo udevadm trigger`, and
make sure the `uinput` module is loaded. The Linux side builds and passes its
tests but has not been measured on a keyboard yet; treat it as untested.

Lighting that reacts to key presses reads the KB7's own key events from
`/dev/input/event*` (read only, nothing is grabbed, and only 64-bit Linux is
supported). That needs one more, optional rule in its own file:

```
# /etc/udev/rules.d/71-kb7screen-keys.rules
SUBSYSTEM=="input", ATTRS{idVendor}=="10f5", ATTRS{idProduct}=="5038", TAG+="uaccess"
```

Warning: this rule lets every program running as the logged-in user read the
KB7's key presses, so a program that logs keys could read this keyboard. Only
add it if you want the reactive lighting effects.

## Tests

```sh
cd screen
go vet ./...
go test ./...
```

No test opens the keyboard.

## Licence

GPL-2.0-or-later, like the rest of the repository. The bundled title font is
Barlow Condensed SemiBold under the SIL Open Font License; its licence text is
in `internal/picture/fonts/OFL.txt`. The font is built into the executable, so
whoever publishes a `kb7screen` executable must ship that `OFL.txt` with it,
for example next to the executable in the same archive.
