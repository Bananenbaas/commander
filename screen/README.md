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
| `kb7screen run <config>` | carries out the buttons' actions and streams the lighting until you press Ctrl+C |
| `kb7screen keytest [--seconds N]` | prints the keys pressed on the KB7 for N seconds (default 20, 1 to 120), to check that the key reader sees the keyboard. Nothing is saved |
| `kb7screen light-preview <config> --profile N [--out FILE]` | writes a 4 second animated GIF of the lighting of profile N, with simulated key presses. Default file: `<config>/previews/light-N.gif`. Touches no keyboard |

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
one step in `action`, unless it has a `state` (see Lighting). Buttons without `symbol` or `picture` get the frame and
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
| `state` | name of a lighting state the button flips; see Lighting. Without an `action` the button flips only the state |
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
ends, however it ends. The lock covers one configuration folder: `apply` with a
different folder while `run` streams is not refused.

## Lighting

`run` can also light the keyboard, so that Swarm II is not needed for that
either. A layout file with a `[light]` block gets a stack of lighting layers;
`run` streams the result to the keyboard's 112 LEDs while that profile is
active. A profile without `[light]` gets no stream and shows what is stored on
the keyboard. A configuration without any `[light]` makes `run` behave exactly
as it does without lighting.

### Layers

Layers are listed bottom first and drawn on top of each other. An effect that
only touches a few keys (the ripple, a state, fixed keys) leaves the layers
below visible everywhere else. A typical stack is a base (`wave`, `breathing`
or `static`), a `ripple` for key presses, one `state` layer per state, and
`fixed` keys on top, so that for example WASD stay lit whatever the effect
does.

```toml
[light]
fps = 30                         # optional, 10 to 30, default 30

[[light.layer]]                  # bottom first
effect = "wave"
colors = ["#0040FF", "#00C0FF", "#8000FF"]
speed = 0.5
direction = "right"

[[light.layer]]
effect = "ripple"
color = "#FFFFFF"

[[light.layer]]
effect = "state"
name = "gear"
toggle = "N"
start = "on"
keys = ["N"]
color = "#30E070"

[[light.layer]]
effect = "fixed"
keys = ["W", "A", "S", "D"]
color = "#FFFFFF"
```

Every layer needs `effect`. The other settings depend on the effect and are
listed below. Any layer may also take `keys = [...]` to limit where it shows;
without it a layer covers every LED. `state` and `fixed` need `keys`. An
unknown effect, an unknown setting, a value of the wrong type or outside its
range, and a missing required setting are refused when the file is loaded, with
the layer's number and the setting's name.

Key and LED names are the key names of the action steps (`W`, `LShift`, `F1`,
`Space`, ...) plus the lights that are not keys: `PrevTrack`, `NextTrack`,
`GameMode`, `ProfileCycle`, `MicOff`, `RGBButton`, `Wrist1` to `Wrist10`, `Fn`
and `Menu`. Three groups name many LEDs at once: `all` (every LED), `keys`
(every LED of index 0 to 84: the main block and the arrows, including Fn, Menu
and RAlt) and `wrist` (the ten wrist rest segments). Names are
not case sensitive.

Setting kinds: a *colour* is `"#RRGGBB"`; a *colour list* is a non-empty list
of those; a *number* is an integer or decimal number; a *duration* is text such
as `"4s"` or `"600ms"`; a *chord* is a key chord as in the action steps, such as
`"N"` or `"LAlt+N"`; a *choice* is one of a fixed list of words.

### Effects

#### `static`

Every LED in one colour.

| Setting | Kind | Default | Range | Meaning |
|---|---|---|---|---|
| `color` | colour | required | | The colour |

#### `fixed`

The listed keys in one colour. Needs `keys`. Put it last, so the keys stay in
this colour whatever the layers below do.

| Setting | Kind | Default | Range | Meaning |
|---|---|---|---|---|
| `color` | colour | required | | The colour |

#### `breathing`

All LEDs fade between a dim level and full brightness; the colour changes at
each minimum.

| Setting | Kind | Default | Range | Meaning |
|---|---|---|---|---|
| `colors` | colour list | `["#FFFFFF"]` | | Colours used one per breath |
| `period` | duration | `"4s"` | 0.5 s to 60 s | Length of one breath |
| `low` | number | 0.1 | 0 to 1 | Brightness at the minimum |

#### `wave`

A colour gradient that travels across the keyboard, following the real
positions of the keys.

| Setting | Kind | Default | Range | Meaning |
|---|---|---|---|---|
| `colors` | colour list | required | at least two | Colours, blended in a cycle |
| `speed` | number | 0.5 | 0.05 to 5 | Cycles per second |
| `direction` | choice | `"right"` | `right`, `left`, `up`, `down` | Direction of travel |
| `length` | number | 14 | 2 to 40 | Key units per cycle |

#### `ripple`

A ring that spreads from every key you press and fades. It needs the key
reader (see Key presses below). Only keys of the KB7 count. Give `color` or
`colors`, not both; with neither the rings are white.

| Setting | Kind | Default | Range | Meaning |
|---|---|---|---|---|
| `color` | colour | white | | Colour of the rings |
| `colors` | colour list | | not together with `color` | Colours of the rings in turn, by press number |
| `speed` | number | 12 | 2 to 60 | Keys per second the ring spreads |
| `width` | number | 1.5 | 0.5 to 6 | Width of the ring in keys |
| `life` | duration | `"1.2s"` | 0.2 s to 5 s | How long a ring lasts |

#### `state`

Lights its keys in a colour while a named on/off state is on. Needs `keys`.

| Setting | Kind | Default | Range | Meaning |
|---|---|---|---|---|
| `name` | name | required | | Name of the state, unique within the profile |
| `toggle` | chord | none | | Key chord that flips the state; without it only a screen button flips it |
| `hold` | duration | none | 100ms to 3s | How long the toggle key must be held; the state flips when it is released. Without it the state flips as the key goes down |
| `start` | choice | `"off"` | `on`, `off` | Value when the profile starts |
| `color` | colour | required | | Colour while the state is on |
| `blink` | number | 0 | 0 to 10 | Blinks per second while on; 0 is steady |

### States

A state is an on/off switch that lights keys, for example the landing gear.
It is flipped by the key chord in `toggle`, which matches when its last key
goes down while exactly its modifiers are held (`N` does not match LAlt+N). The
keys before the last must be modifiers, so `toggle = "A+B"` is refused. By
default the state flips as the key goes down and how long it is held plays no
part.

With `hold` the state flips only when the toggle key is released after being
held at least that long; a shorter press does nothing. The modifiers are
checked when the key goes down. This suits a mode the game switches by holding
a key, such as quantum travel on B (a short tap of B does something else). A
key-up the keyboard never reported does not flip the state. A screen button
still flips such a state at once, as it sends a tap, and switching profiles
drops a press in progress. `hold` without `toggle` is refused.

```toml
[[light.layer]]
effect = "state"
name = "quantum"
toggle = "B"
hold = "400ms"
keys = ["B"]
color = "#40A0FF"
```

A button flips a state with `state = "gear"`; the name must be a `state` layer
of the same profile's `[light]`, or the file is refused when it is loaded. With an `action` it sends the
action and flips the state, unless the action sends the state's own `toggle`
chord, which flips it already. **Without an action** it flips only the state
and sends nothing to the game: the way to bring the light back in line when it
has drifted from the game.

```toml
[[button]]
page = 1
position = 3
title = "Gear"
state = "gear"                   # no action: only the light flips
```

States belong to their profile: two profiles may use the same name without
sharing it, and toggle presses count only while that profile is active. A state
takes its `start` value when its profile is switched to because its program
started, and when `run` starts and finds that program already running;
otherwise it keeps its value across switches back and forth. States can drift
from the game, because a key pressed on another keyboard, a binding that does
more than one thing or a death in the game change the ship without kb7screen
seeing it. There is no game data to correct that; use a button with only a
`state`.

### Key presses

The ripple and key-toggled states need to see the KB7's key presses. On
Windows `run` reads them through Raw Input, which delivers them whatever
program has the focus and only listens: the key still goes to the program
unchanged. Other keyboards, and the keystrokes `run` sends itself, are not
counted. A held key's repeats count as one press. On Linux the keyboard's
`/dev/input/event*` node is read (see the optional udev rule above); that
reader has not been run on a keyboard yet. Without
key input the ripple and the key-toggled states do nothing. Key presses are used
in memory for the lighting and never written to disk or to the log.

`kb7screen keytest` checks the key reader: it prints each key pressed on the
KB7 for the given time (20 seconds by default) and saves nothing.

### Brightness, RGB button and when `run` stops

`run` scales the streamed colours by the keyboard's brightness level. It reads
the level at start and after each profile switch, and follows the notifications
that Fn+Up and Fn+Down cause (steps of ten from 20 to 100 percent). When the
RGB button reports the lights off, `run` keeps streaming. Hardware acceptance
of these three is pending: the brightness read has run on the keyboard without
an error, but whether the level is right has not been checked, and the
brightness following and the RGB button have not been tried yet.

No frame is sent for 2.5 seconds after `run` has enabled the notifications, as
the keyboard does not answer on interface 1 for about 2 seconds then. A slow
or missed frame is skipped, never queued. After a missed acknowledgement the
stream pauses for 2.5 seconds, and a stalled write closes and reopens the
keyboard. A frame streamed after a profile switch stays on the LEDs (seen on
the keyboard), and the same is expected when `run` ends. So when the stream
stops, because the active profile has no
`[light]`, Swarm II started, or `run` ends, `run` sends one last frame after
the held keys are released: the lighting stored on the keyboard for the active
profile (what it shows without Swarm II), at the current brightness. `run`
reads that table together with the brightness after each profile switch. If
it cannot be read, the last frame is the previous lit profile's layers at
their starting point (no ripple, states off) instead. Nothing is sent when the
keyboard is already gone, and lighting continues when the keyboard is plugged
in again.

### Preview

`kb7screen light-preview <config> --profile N` writes a 4 second GIF of the
profile's lighting with simulated key presses (W at 0.5 s, Space at 1.5 s, and
every state flipped at 2.5 s), so effects and colours can be checked without
the keyboard. The GIF does not follow the keyboard's brightness.

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

On Linux the key reader stops after the keyboard is unplugged, until `run`
restarts. Linux key reading has not been run on hardware.

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
