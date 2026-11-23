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

## Download

Ready-built executables are on the
[releases page](https://github.com/Bananenbaas/commander/releases):

| File | What |
|---|---|
| `commander-windows-amd64.exe` | Commander, the tray program (see below) |
| `kb7screen-windows-amd64.exe` | the command line for Windows |
| `kb7screen-linux-amd64` | the command line for Linux (not yet run on a keyboard) |
| `SHA256SUMS.txt` | the SHA-256 of each file |
| `LICENSE.txt`, `OFL-BarlowCondensed.txt` | the licences of the program and of its built-in font |
| `THIRD-PARTY-NOTICES.txt` | the licences of the libraries built into the executables |

Rename them as you like, for example to `commander.exe` and `kb7screen.exe`;
the examples below use those names. Check a download against its SHA-256 with
`Get-FileHash <file> -Algorithm SHA256` in PowerShell (or `sha256sum` on
Linux), and see the release notes for the VirusTotal scans. The files are not
code-signed, so Windows SmartScreen may ask before the first start ("More
info", then "Run anyway"). `kb7screen --version` prints the version; Commander
writes it to its log when it starts.

To build them yourself, see Build below.

## Commander

Commander is a small program for the Windows notification area that keeps
`kb7screen run` going for you. It shows a helmet icon, replaces the hidden
`run`, and lets you reload the configuration, apply the buttons, and quit
cleanly from a menu, without a terminal or Task Manager.

![The Commander helmet icon](internal/icon/helmet-256.png)

Download `commander-windows-amd64.exe`, or build it without a console window:

```sh
cd screen
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags=-H=windowsgui ./cmd/commander
```

Start it the first time with your configuration folder:

```sh
commander.exe D:\path\to\config-folder
```

Commander remembers the folder in its settings, so after that it can be started
without an argument. Right-click the icon for the menu:

- **Open Commander**: opens Commander's window (see below). A left click on
  the icon does the same.
- **Reload configuration**: the running program reads the folder again.
- **Apply buttons**: puts the layouts on the keyboard for you. Commander pauses
  its own run for the moment, applies, and starts it again.
- **Start with Windows**: ticked, Commander starts when you sign in, on the
  folder it uses now.
- **Check for updates**: ticked, Commander looks for a newer release about a
  minute after it starts and once a day after that. Off by default; a check
  that fails is only written to the log.
- **Check now**: looks at once, ticked or not, and says the result in a
  balloon. A build without a version (`dev`) says it is a development build.
- **Update to v...**: shown only when a newer release was found. A balloon
  says so too, once per version; clicking it does the same as this item.
- **Open configuration folder** and **Open log**.
- **Quit**: stops cleanly, releases held keys and puts the stored lighting
  back.

The tooltip reads "Commander - KB7:" followed by the state. A red dot on the
icon means no configuration is running: the start failed, for example after a
typing mistake in a file, or the loop ended with an error. The tooltip and a
balloon say what is wrong; fix the file and choose Reload. A reload that fails
while a configuration is running shows only a balloon, and the old
configuration keeps running.

Updates come from the project's GitHub releases; pre-releases count. Commander
never updates without asking: a message box names both versions and the
release page, and only Yes installs. It then downloads
`commander-windows-amd64.exe`, checks it against the release's
`SHA256SUMS.txt`, puts it in place of the running file (keeping the old one as
`<name>.exe.old` next to it), stops cleanly and starts the new version on the
same folder, which shows a balloon "Commander updated to v..." and removes the
`.old` file. If anything fails before the stop, the running Commander carries
on and a balloon says why. Commander must be in a folder you can write to; in
`Program Files` the update stops and asks you to move it. If a new version
does not start properly, rename `<name>.exe.old` back to `<name>.exe`. The
requests to GitHub carry no more than a `User-Agent` of `Commander/<version>`.

### The window

**Open Commander**, or a left click on the icon, opens Commander's window. Only
one window exists at a time: opening it again brings the open one to the
front. Closing the window changes nothing for Commander or the keyboard, and
quitting Commander closes it.

The sidebar lists the desktop profile and your game profiles (the one
that is active is marked), the KB7 (connected or not) and Commander's
own pages. The top of the window shows the profile the keyboard is on. Today the window shows and does these:

- **KB7**: the profiles on the keyboard as chips; the one it is on is
  highlighted. The Buttons tab shows the profile's buttons and edits them:
  title, look (colour, symbol or your own picture), steps (press keys, wait,
  start a program, open a website, file or folder) and options. The Lighting
  tab edits the profile's lighting (below). The Mixer and Profile tabs are
  described below.
- **Desktop and each game**: a game profile page, edited and saved like the
  others (below).
- **Settings**: Start with Windows, Check for updates automatically, the theme
  (Follow Windows, Light or Dark) and buttons that open the configuration
  folder and the log.
- **Updates**: the version, Check now, and Update when a newer one is found.

**Saving.** Edits stay a draft (the "Unsaved changes" bar) until you press
Save. Commander checks that the whole folder still loads and that the pictures
fit the 15 different pictures the KB7 holds before it writes anything, and a file that does not load is
never written. Files are written to a temporary file and renamed, so a crash
does not leave half a file. Commander writes the files it saves in its own
form: they start with a line saying so, and comments, formatting and key
order are not kept. When a file holds any of that (comments, for example), the old file is first
copied to `backup/` in the configuration folder (for example
`backup/kb7-sc-20261122-221530.toml`, or `backup/games-sc-....toml` for
`games/sc.toml`). Pictures and symbols you pick (symbols are PNG files) are
copied into `pictures/` and `symbols/` in the folder. If the file was changed
outside Commander while the window was open, Save says so and keeps your edits;
a second Save overwrites it. After a save Commander reloads the configuration
(steps, modes, confirmation, states) or applies it to the keyboard (buttons
added, removed or moved, or a title, label, colour, symbol or picture
changed).

**The Lighting tab.** It shows the profile's lighting on a picture of the
keyboard, drawn by the same lighting engine that lights the keyboard, with
the same simulated key presses as `kb7screen light-preview` (W, then Space,
then every state flipped), looped. A profile without `[light]` says "Not set:
the keyboard shows its own lighting"; **Set lighting** starts one and **Remove
lighting** goes back to that.

The simple view covers a base effect (One colour, Wave or Breathing, or Off), an
optional ripple when a key is pressed, keys in one colour (Highlight keys,
picked on the picture) and Key states: a name, the key or keys that flip it
(recorded like a button's keys), the keys it lights and a colour. Wave and
Breathing have a speed slider (Breathing from slow, 10 s a breath, to fast,
1 s). Lighting the simple view cannot show (another order, other effects, an
effect twice, settings it has no control for, a layer switched off) opens in
**Advanced**, with the reason. Advanced edits the whole layer stack: add,
remove, reorder (drag, or Up and Down), switch a layer off and set every
setting of every effect; its fields come from the lighting engine, so a new
effect needs no change in the window. Back to the simple view is offered when the
stack fits it.

**When Commander is not running** sets what the keyboard shows then: "A still of
this lighting" (the lighting frozen as it is, which Commander writes to the
keyboard's stored lighting when it applies the profile; `[stored_light]` then
holds at most a brightness) or "One colour" (the `[stored_light]` colour),
with a brightness from 0 to 100 %, or "Keep the keyboard's brightness".

A lighting save is checked and written like a buttons save, and it only
looks at the lighting: buttons and lighting of one profile are saved
separately, and an unsaved draft of the one never makes the other "changed".
After it, Commander applies the keyboard when the profile has no stored
colour (the stored still must follow the new lighting) or when `[stored_light]`
changed, and otherwise reloads the configuration.

A layer can be switched off with `enabled = false` in the layer: Commander keeps it
in the file but leaves it out of the lighting. A `kb7screen` older than this
version refuses that key ("unknown setting"). A stack whose layers are all off
is an error ("every lighting layer is off; turn one on or remove the
lighting").

**Game profile pages.** Each game has a page with **Switch to this when** (the
program: pick one of the running programs, with Refresh, or choose its file;
Windows' own programs are left out of the list) and **What each device does**
(the KB7 profile to show, or "(don't switch)"). **New game profile** in the
sidebar makes one, and **Delete game profile** removes it. Desktop is the
profile shown when no game runs ("When the program ends: back to Desktop"): it
always has a KB7 profile, has no program and cannot be deleted. When
`games/desktop.toml` does not exist yet, the Desktop page shows the profile
Commander uses now, and the first game save, delete or duplicate writes that
file so that Desktop stays where it is. A new game's file is
`games/<name>.toml` (lower case; `-2`, `-3` on a clash); renaming keeps the
file. A game profile is saved like the buttons (checked first, backup of a
hand-written file), and Commander then reloads the configuration.

**The Mixer tab** shows the system volume and the programs with sound that the
keyboard's mixer page shows, and asks Commander every second while it is open.
Nothing is set from the window. Without Windows audio, or when Commander is not
running, it says "The volume mixer is not available right now."

**The Profile tab** sets the name shown on the keyboard (empty leaves the
keyboard's name as it is), **Duplicate** copies the profile into the lowest free
slot as `kb7-<slot>.toml` named "... copy" (refused when all five slots are in use:
"The KB7 has five profiles and all five are in use."), and **Delete profile**
removes it. A profile a game profile uses (Desktop included) cannot be deleted:
"Desktop and Star Citizen use this profile; choose another profile there
first." Neither can the last one: "The last profile cannot be deleted." A
deleted profile's layout is moved to `backup/`, so it can be restored. A name
change, a duplicate and a delete apply the keyboard; a duplicate and a delete
pin Desktop in `games/desktop.toml` first when it is not there yet.

Windows keeps some keys for itself (Windows key combinations, Print Screen),
so they cannot be recorded in a key step; add them with Add step.

The first Save also moves `switch_when_running` out of your layouts into game
profiles: `switch_when_running = "StarCitizen.exe"` becomes the game
`StarCitizen` in `games/starcitizen.toml`, and the desktop profile goes to
`games/desktop.toml` (only when none exists). Each file is written in turn, and
the original layout is kept in `backup/` when it holds something Commander
would not keep, such as comments. If a save is interrupted during this, a program's link to
its profile can be missing until you restore the layout from `backup/`.

The window needs the Microsoft Edge WebView2 Runtime. Windows 11 has it; if
it is missing, Commander says so and where to get it
(https://developer.microsoft.com/microsoft-edge/webview2/). The window is a
separate process (`commander.exe --window`, started for you) so a problem in
it never stops the keyboard. It loads nothing from the internet, opens no
other windows and refuses permission requests; only you, signed in on this
Windows account, can reach Commander from it. Its own data is in
`%LOCALAPPDATA%\Commander\WebView2` and its log is
`%LOCALAPPDATA%\Commander\window.log`.

Settings are in `%APPDATA%\Commander\settings.toml`. The log is
`%LOCALAPPDATA%\Commander\commander.log`; it is moved to `commander.log.1`
once it passes 1 MB, as is `window.log`.

Only one program owns the keyboard per user: a second Commander, or a `run`
while Commander runs, refuses to start. `apply` and `restore` refuse while
Commander or `run` runs; use **Apply buttons** in the menu, which does it for
you.

Linux has no tray program. There, start `kb7screen run <config>` yourself and
use `kb7screen stop <config>` and `kb7screen reload <config>` from another
terminal.

The check on the real desktop (icon, menu, Start with Windows) has passed;
see "Desktop check" in `docs/design/commander-tray.md`. Updates are covered by
unit tests only so far; their desktop check is still to be done (see
`docs/design/commander-update.md`). The same holds for the window (see
`docs/design/commander-gui.md`).

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
| `kb7screen apply <config> [--full]` | puts every layout of the folder on the keyboard. `--full` sends every picture again. Refuses while Commander or `run` runs |
| `kb7screen restore <config> --profile N` | gives the positions kb7screen took over in profile N their original contents back |
| `kb7screen run <config>` | carries out the buttons' actions and streams the lighting until you press Ctrl+C |
| `kb7screen stop <config>` | asks the run for that folder to stop and waits up to 10 s for the request to be taken |
| `kb7screen reload <config>` | asks the run for that folder to read its configuration again and waits up to 10 s for the request to be taken; the run's output or Commander's log says whether the new configuration loaded |
| `kb7screen background <image>` | uploads a PNG or JPEG as the screen background (scaled to cover 480 x 800 and cropped in the middle; about 30 s). Choose it afterwards in the keyboard's own background settings. Refuses while Commander or `run` runs |
| `kb7screen clear-launchers <config> --profile N` | empties the application, website and third-party application buttons on all pages of profile N, freeing their picture slots. The positions the folder manages are left as they are; use `restore` for those |
| `kb7screen keytest [--seconds N]` | prints the keys pressed on the KB7 for N seconds (default 20, 1 to 120), to check that the key reader sees the keyboard. Nothing is saved |
| `kb7screen light-preview <config> --profile N [--out FILE]` | writes a 4 second animated GIF of the lighting of profile N, with simulated key presses. Default file: `<config>/previews/light-N.gif`. Touches no keyboard |

`-h` or `--help` prints the usage, `--version` the version. The exit code is 0 on success and 1 on an
error; the message goes to standard error, prefixed with `kb7screen:`.

`preview` writes one PNG per button, named `<profile>-<page>-<position>.png`
(with transparency), and per profile a `panel-<profile>.png` for page 1 that
shows the 12 positions as a grid of 3 columns and 4 rows on a dark background,
empty positions left dark; page 2 or 3 with buttons gets its own
`panel-<profile>-<page>.png`. It is the quickest way to check titles and
symbols before anything is sent to the keyboard.

`apply` and `restore` report which pictures were uploaded and which were
already in place, which positions were restored (as `profile P page G position
N`), and any notes. Pictures that did not change are never sent again, so a
second `apply` of an unchanged folder is fast.

## The configuration folder

```
config/
  desktop.toml            one file per managed profile
  game.toml
  games/                  optional: one file per game profile
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
name = "SC"                          # optional: the profile's name on the keyboard
switch_when_running = "game.exe"     # optional: switch to this profile while it runs

[[button]]
page = 1                             # 1 to 3, the pages of the button screen
position = 0                         # 0 to 11, row by row from the top left (page 3: 0 to 10)
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

[[button]]
page = 2                             # the second page of the button screen
position = 0
title = "Map"
action = ["M"]
```

Each file needs a `profile`. `name` is optional: `apply` gives the profile that
name (1 to 19 printable ASCII characters, the length the keyboard stores) when
it differs from the one on the keyboard, and leaves the name alone without it.
Each button needs `page`, `position` and at least
one step in `action`, unless it has a `state` (see Lighting). Buttons without `symbol` or `picture` get the frame and
the title only.

| Key | Meaning |
|---|---|
| `page` | the page of the button screen, 1 to 3 |
| `position` | 0 to 11, row by row from the top left; page 3 has positions 0 to 10 only. Two buttons of one file cannot share a page and position |
| `title` | drawn into the picture in a condensed font, 23 px, set smaller (down to 17 px) when it is long. A title that still does not fit is refused, never cut |
| `color` | colour of the frame and the symbol |
| `symbol` | PNG drawn inside the frame in the button's colour |
| `picture` | own PNG of any size, fitted into 142 x 148 pixels keeping its transparency. Cannot be combined with `symbol` |
| `label` | optional firmware label, at most 11 printable ASCII characters. Empty by default |
| `action` | the steps, see below |
| `mode` | `tap` presses the steps once; `hold` keeps one key chord down for as long as the touch lasts |
| `state` | name of a lighting state the button flips; see Lighting. Without an `action` the button flips only the state |
| `confirm` | `"hold 1s"`: the button acts only once the finger has stayed on it that long (200 ms to 5 s), so an accidental touch does nothing. Not with `mode = "hold"`, and needs an `action` |

`kb7screen` uses up to 15 picture slots, so at most 15 different pictures can
be on the keyboard at once, over all profiles; buttons with an identical
picture share one slot. That 15 is the highest slot is a hypothesis, taken
from Swarm II's own limit of 15 application buttons (see `docs/PROTOCOL.md`);
slots 1 to 15 were each seen taking an upload. `apply` refuses a folder that
needs more.

Swarm II's own application buttons use the same slots. `apply` reads the
button tables of all five profiles and leaves every slot that such a button
shows alone, on any page, with a note; if too few slots are left, it refuses
before uploading any picture and names those buttons (positions it gives back
to their originals may already have been written).
`kb7screen clear-launchers <config> --profile N` empties them on one profile;
the positions the folder manages are left as they are.

### Game profiles

A game profile says which KB7 profile to show while a program runs. They are
the files in the `games/` folder of the configuration folder, one TOML file
per game:

`games/sc.toml`:

```toml
name = "Star Citizen"
program = "StarCitizen.exe"

[devices]
kb7 = 2                              # the KB7 profile (0 to 4) to show while it runs
```

`games/desktop.toml` is the Desktop game, the profile to show while no game
runs. It has no `program`, and `kb7` is required:

```toml
[devices]
kb7 = 0
```

The rules:

- `name` and `program` are required, except in `desktop.toml`, where `name`
  defaults to "Desktop". `program` is matched like `switch_when_running`:
  upper or lower case, with or without `.exe`. Unknown top-level keys are
  refused.
- `kb7` is optional in a game. A game without `kb7` leaves the keyboard
  alone: while it runs, the other running games, or the desktop profile,
  decide. Other keys under `[devices]` are ignored, so a file can already
  name devices Commander does not drive yet.
- The game that started last decides the profile. When it ends, the one
  started before it takes over again, and the desktop profile when none is
  left. When two games are found running at the same poll, the one listed first
  wins: files in `games/` by file name, then the games that come from
  layouts, by profile number.
- Names are unique, upper and lower case counting as the same, across all
  game files and the Desktop game. A program may be named in only one game
  file. The program of a layout's `switch_when_running` may not be the name
  of a game file either. Two layouts that name the same program still load;
  the one with the lower profile number wins.
- `switch_when_running` in a layout keeps working for a program that has no
  game file: it counts as a game named after the program, that shows the
  layout's profile. A program that is both in a layout's
  `switch_when_running` and in a file of `games/` is refused when the
  configuration is read; remove one of them.
- Without `games/desktop.toml` the desktop profile follows the rule below.

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
error; running it again writes the tables again. When it succeeds, it ends on the profile that `run` would choose: the one of the running
game that is listed first and has a `kb7` profile (`apply` cannot know which
started last), otherwise the desktop profile.

The desktop profile is the `kb7` of `games/desktop.toml`. Without that file
it is the lowest profile whose layout has no
`switch_when_running`. When every layout has one, as in a folder with only the
`game.toml` above, it is the lowest profile that no layout names (profile 0 in
that example); that profile has no buttons of kb7screen, and touches there are
left to the keyboard.

`run` waits for the keyboard, enables its notifications, switches to the
expected profile and then carries out the touches of buttons it manages.
The keyboard does not confirm a switch to the profile that is already active,
so where the active profile is not known (the first switch of `apply` and
`restore`, and when `run` connects for the first time) the switch goes
through another profile first. When the loop starts again within the same
`run` or Commander (a reload, or after Apply buttons), it keeps the profile
the keyboard was on, also one chosen on the keyboard by hand, unless a
game is running whose profile differs; that profile wins as always. Games
already running when the loop starts again count as started together, so the
first-listed of them wins. After a reload the keyboard is taken to be on that profile,
so nothing is switched at all; after Apply buttons, which switches profiles,
or when the keyboard had not confirmed the switch to that profile, the
switch back goes through another profile first. A profile tapped on the
keyboard during the second or two of a reload is not seen until the next
profile change: until then, touches act for the kept profile. When the keyboard was
unplugged, did not answer or was left to Swarm II in between, `run` connects
as at the first start. Every two seconds `run` checks the programs of the game profiles and
switches profile when one starts or ends, unless that profile is already
active. The game started last decides. Only touches of application buttons
act; a position that the keyboard shows as something else is left to the
keyboard. If the keyboard is unplugged, `run` waits for it and sets it up
again. When three attempts in a row to set it up or switch its profile fail
for another reason (the keyboard is there but its control interface times
out, while typing may still work), `run` logs "the keyboard does not answer;
unplug it and plug it back in" and Commander's tooltip and a balloon say the
same. `run` keeps trying meanwhile; the message ends as soon as the keyboard
answers again.

`run` reads the configuration folder once, when it starts. After changing the
folder, run `kb7screen reload <config>` (or choose Reload configuration in
Commander) to make it read the folder again. To change the pictures on the
keyboard, stop `run` with Ctrl+C, run `apply`, and start `run` again, or use
Apply buttons in Commander.

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

With `mode = "wave"` the ring runs through `colors` as it spreads: the colour at
each key is the cyclic blend of the list at the key's distance from the pressed
key, `spread` keys per full run. Wave mode needs `colors` with at least two
colours and no `color`. With `drift` above 0 the colours also move with time;
give it the `speed` of a `wave` layer with the same `colors` and the ring shows
the colours that layer shows at that moment, so the two keep in step.

| Setting | Kind | Default | Range | Meaning |
|---|---|---|---|---|
| `color` | colour | white | | Colour of the rings |
| `colors` | colour list | | not together with `color` | Colours of the rings in turn, by press number |
| `mode` | choice | `"press"` | `press`, `wave` | `press`: one colour per press; `wave`: the ring runs through `colors` |
| `spread` | number | 8 | 2 to 30 | Wave mode: keys per full run through `colors` |
| `drift` | number | 0 | 0 to 5 | Wave mode: cycles per second the colours move through `colors` |
| `speed` | number | 12 | 2 to 60 | Keys per second the ring spreads |
| `width` | number | 1.5 | 0.5 to 6 | Width of the ring in keys |
| `life` | duration | `"1.2s"` | 0.2 s to 5 s | How long a ring lasts |

```toml
effect = "ripple"
mode = "wave"
colors = ["#0000FF", "#8800FF", "#FF00AA"]
spread = 8
```

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

### Stored lighting

What the keyboard shows when nothing streams (Commander not running, a profile
without `[light]`, a moment during a reload) is the lighting stored on the
keyboard for that profile. `apply` sets it:

- A profile with `[light]` gets a still picture of its own layers: drawn at
  their starting point, so no ripples, every state at its `start` value, and
  waves and breathing at time zero.
- `[stored_light]` overrides that, or sets a profile without `[light]`:

```toml
[stored_light]
color = "#FFFFFF"     # every LED in one colour
brightness = 80       # percent, 0-100; optional
```

`color` and `brightness` are both optional, but one of them must be given.
Without `brightness` the keyboard keeps the brightness it holds; without
`color` the colours come from `[light]`, or stay as they are. A layout file may
hold only `profile = N` and `[stored_light]`, to set a profile kb7screen has no
buttons on.

`apply` writes the stored lighting only when it differs from what the keyboard
holds, and prints `stored lighting written: profile N` when it does. A profile
with neither `[light]` nor `[stored_light]` is left alone. Not yet checked on
the keyboard: the table is written as Swarm II writes it, but whether the
keyboard keeps it after it is unplugged has not been tried.

### Volume mixer

On Windows, `run` (and so Commander) keeps the keyboard's volume mixer page in
step with the computer's sound: the first slider is the system volume (of the
default output device), and the next ones are the programs on any output
device (a headset and speakers alike) with sound sessions, shown by their name (the
program's own name when it gives one, otherwise the executable's name). At
most four programs are shown; ones playing sound come first. A program that
plays keeps its place; an idle one (a paused player, for example) gives its
place to a program that starts to play. Among the rest, the ones `run` saw
first come first. Moving a slider on the keyboard sets that volume in Windows,
and a change made in Windows moves the slider on the keyboard. A program's
slider sets that program's volume on every output device it plays on; the
level it shows is the one on the default device when the program plays there.
Nothing needs to be configured.

The mixer is Windows only. When Windows audio cannot be used (no output
device, for example), `run` logs one line and carries on with the buttons and
the lighting. Not yet checked on the keyboard.

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
- **Never unplug the keyboard during `apply` or `background`.** A picture
  upload takes about 2.7 seconds, a background upload about 30 seconds, and
  each must run to the end. The keyboard does not type while an upload runs.
- **Ctrl+C during `apply`** does not cut a picture short: `apply` finishes the
  picture it is sending, writes the buttons of that profile whose pictures are
  in place, saves the ledger and ends with `stopped after the current picture;
  the ledger is saved`. Run `apply` again to finish the rest. `restore` stops
  only if it has not changed anything yet. Closing the console window asks
  for the same stop, but Windows ends the program a few seconds later, which
  may be too soon; use Ctrl+C. `background` does the same: Ctrl+C lets the
  upload finish and then says it stopped.
- **"Unplug and replug."** If an upload is interrupted, the keyboard may stay
  in upload mode and stops working as a keyboard. `apply` then prints a
  prominent message. Unplug the keyboard, plug it back in, and run `apply`
  again; the ledger remembers which pictures did not arrive.
- Only frames that are documented in `docs/PROTOCOL.md` are sent. Nothing that
  could touch firmware updates, the bootloader or a factory reset is
  implemented.
- `kb7screen` writes to the buttons you configured and to its own ledger. When
  a layout asks for it, `apply` also writes that profile's stored lighting
  and name, and while `run` holds the keyboard it fills the keyboard's volume
  mixer page. `restore` gives positions back what they held before; it does
  not give back stored lighting or profile names.
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
whoever publishes a `kb7screen` or Commander executable must ship that
`OFL.txt` with it, for example next to the executable in the same archive; the
releases do so as `OFL-BarlowCondensed.txt`. The libraries built into the
executables have their own licences, collected in `THIRD-PARTY-NOTICES.txt`
(next to this file); the releases ship it too.
