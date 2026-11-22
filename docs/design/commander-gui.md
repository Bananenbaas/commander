# Commander's window

Design for the window in which a player sets up Commander without editing
files. Status: approved 2026-11-22; part 2 (the window
shell and its pages without editing) is implemented, its desktop check is still
to do; part 3 (saving and the Buttons tab) is implemented, its desktop check is
still to do. Builds on
[`commander-tray.md`](commander-tray.md),
[`commander-update.md`](commander-update.md), [`lighting.md`](lighting.md),
[`pages-lighting-mixer-names.md`](pages-lighting-mixer-names.md) and
[`screen-app.md`](screen-app.md).

## Intent

Commander does a lot for the KB7 today, but all of it is set up in TOML files
by hand: buttons on three pages with actions and pictures, layered lighting,
stored lighting, the profile names and switching to a profile while a program
runs. The tray menu only reloads, applies, and holds a few settings. Earlier
designs kept deferring "the GUI" to later; this is that design.

Two things shape it:

- **It is for an average gamer.** Someone who has never seen a TOML file must
  be able to set up everything that works today, in words a gamer uses
  ("When Star Citizen starts, switch to this"), not in protocol or file terms.
- **Commander is meant to grow into one program for gaming devices from
  different makers.** The Mountain Everest Max and DisplayPad are the next
  devices wanted. Each maker ships its own software today, and those programs
  know nothing about each other. Commander should let devices of different
  makers work together, switching together per game. Only the KB7 exists
  today, but the window must be built as "a system with devices" from the
  start, so a new device adds a page and changes nothing else.

## Goals

- **Everything that works today can be set up in the window**, plus game
  profiles (below). Nothing needs a file editor any more.
- **Game profiles.** A game profile names the program that starts it and, per
  device, which of that device's profiles to use, or "don't switch". When the
  program starts, every device switches together; when it ends, everything
  goes back to the default game profile, "Desktop". A player with one device
  never has to make a game profile.
- **Device profiles** are the building blocks: per device, reusable, not tied
  to a game. For the KB7 they are its five profile slots.
- **Saving puts it on the device.** The player presses Save; Commander writes
  the configuration and does the reload or the apply that the change needs.
  The player never chooses between "reload" and "apply".
- **Previews from the real code.** The button grid is drawn by the same code
  as `kb7screen preview`, the keyboard picture by the same code as
  `light-preview`, so what the window shows is what the device will show.
- **Looks like commander.work.** The site's colours, Barlow Condensed for
  headings, the cut-corner buttons, a light and a dark theme.

## Non-goals for now

- Devices other than the KB7. The structure is ready for them; their pages
  come with their own designs and measurements.
- Linux. There is no tray program there today and none comes with this.
- Sharing or importing profiles from other people, a profile store, cloud
  anything.
- New features for the KB7 itself. The window sets up what works today.
- Game functions by name ("landing gear") that follow a game's key bindings;
  actions remain keys, waits, programs and links.

## Anti-goals

These would make it a failure even if it works:

- **Showing protocol or file terms to the player**: report IDs, slot numbers
  of pictures, `switch_when_running`, layer stacks on the first screen of the
  lighting tab, TOML errors without saying which field in the window is wrong.
- **The maker as structure.** A Mountain device must work in the window
  exactly like a Turtle Beach device; the maker appears at most as a detail on
  the device page.
- **A browser.** The window is a normal Windows window: own icon, own title
  bar, no address bar, no tab. No web server, no open port, no page from the
  internet.
- **Touching the keyboard from the window.** All keyboard work stays with the
  supervisor and its run loop; the window only writes configuration and asks
  for a reload or apply. The hardware safety rules in `DEVELOPMENT.md` are not
  reopened by this design.
- **Breaking hand-edited configurations or the command line.** `kb7screen`
  keeps reading the same files and keeps working without Commander.
- **Templated styling** such as a thick coloured stripe along one side of the
  selected item.

## Constraints

- Go, built with `CGO_ENABLED=0`; dependencies kept to the minimum
  (`DEVELOPMENT.md`). Commander is a windowless Windows program with a tray
  icon and a supervisor that owns the keyboard
  (`commander-tray.md`).
- The KB7 has five profile slots, three pages of twelve positions in a grid of
  three columns and four rows (page 3 position 11 excluded), and 15 picture
  slots shared by all pages and profiles (`pages-lighting-mixer-names.md`).
  The window refuses a sixth KB7 profile or a sixteenth picture with a plain
  sentence.
- The active profile cannot be read from the keyboard, only followed
  (`screen-app.md`).
- Today at least one KB7 profile must remain without a program, for the
  desktop (`layout.Load` refuses otherwise); game profiles replace that rule
  (see Switching rules).
- Every lighting effect already declares its settings (`light.Spec`, with a
  `Kind` per setting: colour, colour list, number, choice, duration, chord,
  name). The advanced lighting editor draws its fields from those
  declarations, so a new effect needs no window code.
- The mixer page follows Windows by itself while Commander runs; it has no
  settings today.

## Approaches considered

How the window is built:

| Approach | For | Against |
|---|---|---|
| **A Windows window hosting WebView2** (chosen) | The site's CSS carries over almost as is, light and dark included; lists, drag and drop, text fields and colour pickers exist; previews are the PNGs the preview code already makes; new device pages are cheap | One new dependency (a Go binding to WebView2 without cgo); needs the WebView2 runtime, which ships with Windows 11 and an updated Windows 10; Linux would need another route later |
| Gio, a GUI toolkit in pure Go | No outside rendering engine; full control of every pixel | Many controls built by hand (sortable lists, text fields with selection, colour picker, key recording); several times the work; every new device page costs more; the site's style redone in code |
| Native Windows controls | No new dependency at all | Cannot carry this style; looks like stock Windows |
| Fyne | A full toolkit | Needs cgo |

Chosen binding: `github.com/wailsapp/go-webview2` (MIT): it builds without
cgo and, without the `native_webview2loader` build tag, uses the WebView2
runtime installed in Windows instead of loading an embedded Microsoft DLL
from memory. A probe on the owner's machine (2026-11-22, runtime 157) opened
a window, passed a message from the page to Go and closed cleanly.

Chosen by the owner: WebView2. The owner also wants the door to Gio kept
open: the window talks to Go through a narrow, fixed set of calls (below), so
switching later means rebuilding the screens, not Commander.

How the main window is organised (sketches compared with the owner):

| Option | Chosen |
|---|---|
| **Sidebar** with game profiles, devices and Commander's settings; the selection on the right | yes |
| A home screen with a tile per device, each opening full-window | no: back-and-forth for every change |
| Game first: pick the game profile, a tab per device inside it | no: odd for a player with one device and no game profiles |

How a button is edited: **an edit panel to the right of the grid** (chosen),
not a pop-up, because a player usually fills a whole page in one go and
should keep seeing the other buttons.

How lighting is edited: **simple first, the layers behind "Advanced"**
(chosen), not the layer stack on the first screen, because most players want
"a wave in my colours with WASD white" and should not have to think in layers.

## Design

### The window and the tray

- The tray icon stays, with its menu. A new first item, **Open Commander**,
  opens the window; so does a left click on the icon (the right click keeps
  the menu). Closing the window leaves Commander running in the tray. A
  second Open Commander brings the open window to the front.
- **The window is a process of its own** (chosen by the owner,
  2026-11-22): Open Commander starts `commander.exe` again in window mode,
  and that process talks to the running Commander over a named pipe that
  only the signed-in user can open. The Go binding to WebView2 ends the
  process with `os.Exit` on most WebView2 errors, so in the Commander
  process itself a failing window would drop the keyboard without the clean
  stop (held keys, stored lighting). In a process of its own, a failing or
  closed window never touches the keyboard. The window process ends when
  its window is closed or when Commander quits.
- Tray items that are settings (Start with Windows, Check for updates) also
  appear on the window's Settings page; both stay in step.
- When the WebView2 runtime is missing, Open Commander shows a message box
  that says so and links Microsoft's download page; the tray keeps working.

### Layout of the window

```
+----------------------------------------------------------------+
| [helmet] COMMANDER        ● Running · Star Citizen     - [] x  |
+----------------+-----------------------------------------------+
| GAME PROFILES  |  KB7                                          |
|  Desktop  def. |  Turtle Beach Command Series                  |
|  Star Citizen  |  [DESKTOP 1] [SC FLIGHT 2] [SC MINING 3] [+]  |
|  + New         |  BUTTONS  LIGHTING  MIXER  PROFILE            |
| DEVICES        |  ---------------------------------------------|
|  KB7 connected |  (tab content)                                |
|  DisplayPad    |                                               |
|   not conn.    |                                               |
| COMMANDER      |                                               |
|  Settings      |                                               |
|  Updates       |                                               |
+----------------+-----------------------------------------------+
```

- **Title bar**: the helmet, the name, and what runs now ("Running · Star
  Citizen", or what is wrong, with the red state the tray icon shows today).
- **Sidebar**: game profiles (Desktop first, marked default; the active one
  marked), devices (connected in green, known but absent in the site's warm
  orange, kept with their settings), Commander's Settings and Updates.
- **Device page**: name and a one-line description of what it can do, the
  device's profiles as cut-corner chips (the KB7 shows its slot number), and a
  tab per capability the device has. The KB7 has Buttons, Lighting, Mixer and
  Profile.
- **Capabilities, not devices, own the tabs.** A device declares what it has
  (a touchscreen with buttons, per-key lighting, a mixer, a profile name), and
  the page is built from those parts. The DisplayPad's buttons with own
  pictures are expected to reuse the button editor.

### Game profile page

- Name.
- **Switch to this when**: a program, picked from the programs running now or
  by browsing to an `.exe`.
- **What each device does**: per known device, one of its profiles or
  "(don't switch)", with an Edit link to that device profile. A device that is
  not connected keeps its choice.
- When the program ends: back to Desktop.
- Desktop is the default game profile: it has no program and cannot be
  deleted. It names a profile for every device (it has no "don't switch") and
  decides what each device shows when no game runs. Desktop and a game may
  use the same device profile.
- Save, Delete.

Switching rules (builder's call, approved with this document):

- **The game started last wins**, per device. When it ends, the most
  recently started game that still runs takes over again, else Desktop. A
  game with "don't switch" for a device leaves that device to the game below
  it in this order, or to Desktop.
- The rule "at least one KB7 profile without a program" goes: programs now
  belong to game profiles, and Desktop always names a KB7 profile.
  `layout.Load` and `DesktopProfile` change with it, and `kb7screen run`
  follows the same rules.

### KB7, Buttons tab

- Page chips 1, 2, 3, and the grid of the selected page drawn as the keyboard
  will show it (three columns, four rows). Clicking a button selects it;
  clicking an empty place adds one there. Dragging a button to another place
  moves it (to an empty place) or swaps the two.
- **Edit panel** on the right, for the selected button:
  - Title.
  - Look: Commander style (a colour from a palette or any colour, and a
    symbol) or Own picture (a PNG). The grid redraws as the player types.
  - **When tapped**: a numbered list of steps: press keys, wait, start a
    program, open a website, file or folder. **Record keys** records a key
    combination or a sequence with its pauses; **Add step** adds one by hand.
    Steps can be reordered and removed. Keys are shown as key caps, never as
    codes.
  - Options: Hold while touched (the `hold` mode), Hold to confirm (the
    `confirm` duration), and the lighting state the button flips, if any.
  - Save, Delete.

### KB7, Lighting tab

- A live preview of the keyboard on top, animated, drawn by the lighting
  engine.
- **Simple view** (default):
  - Base effect as cards: One colour, Wave, Breathing, Off; with its colours
    and speed.
  - Extras with a tick: Ripple when a key is pressed; Highlight keys (pick
    keys on the picture, one colour); Key states (each a name, the key that
    flips it, the keys it lights, a colour).
- **Advanced** opens the full layer stack: a list of layers, top first, with
  on/off and drag to reorder; the selected layer's settings drawn from its
  effect's `Spec`; where a layer shows is picked by clicking keys on the
  picture. A stack that the simple view cannot show (more than one base
  effect, layers in an order the simple view does not produce, settings it
  does not have) opens in Advanced directly, with a note why.
- **When Commander is not running** the keyboard shows: a still of this
  lighting (the default), or one colour with a brightness (`[stored_light]`).

### KB7, Mixer tab

Shows what the keyboard's mixer page follows: the system volume and the
programs with sound, with their levels, live. Nothing to set today; the tab
says that the keyboard follows Windows by itself while Commander runs.

### KB7, Profile tab

The profile's name on the keyboard, its slot (1 to 5), which game profiles
use it, Duplicate (into a free slot) and Delete (refused while a game profile
uses it, naming which).

### Settings and Updates

- **Settings**: Start with Windows; the configuration folder (with "Open
  folder"); Open log; theme: follow Windows (default), light, dark.
- **Updates**: the running version, Check for updates automatically, Check
  now, and when one is found, Update to v… with the same confirmation as the
  tray. All behaviour as in `commander-update.md`.

### Look

The site's tokens, in `site/style.css`, carried over: dark `--bg #080c18`,
raised `#10172a`, lines `#222c47`, text `#e8edf7` and muted `#9ba8c2`, accent
`#00aaff` with ink `#00121c`, warm `#ff9a3d`; and the light set. Barlow
Condensed SemiBold (already bundled, with its licence) in capitals for
headings, tabs, chips and buttons; the system font for body text. Buttons and
chips have the cut corner of the site's buttons and of the KB7 button style.
The selected sidebar item has a soft accent background, no side stripe. The
theme follows Windows unless the player chooses.

A reference mock-up approved by the owner was made during the design; it used
`site/img/panel.png` as the button grid.

### Files: what the window reads and writes

- **KB7 device profiles stay the layout files they are today**, one per slot,
  in the configuration folder, so `kb7screen` keeps working on the same
  folder.
- **Game profiles** go in a subfolder `games/` of the configuration folder,
  one TOML file per game profile: its name, its program, and per device the
  device profile (for the KB7 by slot number) or nothing for "don't switch".
  Desktop is `games/desktop.toml`. A subfolder, not a `games.toml` next to the
  layouts, because released `kb7screen` and Commander versions read every
  `*.toml` directly in the folder and refuse a file without `profile`; they
  never look into subfolders. `kb7screen run` reads `games/` too, so the
  command line switches profiles as Commander does.
- **Existing `switch_when_running`** is migrated all at once, the first time
  the window saves anything: every layout's program becomes a game profile
  with only the KB7 in it, Desktop is created from today's desktop profile
  (`DesktopProfile`), and `switch_when_running` is removed from the layouts.
  Until then the window shows those programs as game profiles read from the
  layouts. After the migration, a program found both in a layout (added by
  hand) and in `games/` is a load error that names both files.
- **Hand-written files lose their comments on save** (chosen by the owner).
  The TOML library cannot keep comments, key order or formatting. Before the
  window overwrites a file for the first time, it copies the original to
  `backup/` in the configuration folder (with the date in its name), and says
  so once. Every setting is carried across, including fields the window has
  no control for, such as a button's firmware `label`.
- **Writing**: the window never writes a file that does not load. A save
  builds the new file, loads the folder with it through the same code `run`
  uses, and only then replaces the file (write to a temporary file, then
  rename). A load error is shown at the field it concerns.
- **After a save** Commander decides: a change to actions, lighting layers or
  game profiles is a reload; a change to titles, looks, positions, pages,
  stored lighting or profile names needs an apply, which the supervisor
  already does (pause the run, apply, start again). The status in the title
  bar shows it ("Putting buttons on the KB7…").
- **Files edited by hand** while the window is open: the window notices the
  change and reloads its view; unsaved edits in the window are kept and the
  player is told the file changed.

### The narrow set of calls

The window's pages talk to Commander only through these calls (names
indicative), carried over the named pipe, so a later move to another toolkit
replaces the window process and keeps these:

- `state()`: devices with their capabilities and connection, game profiles,
  the run state; plus a push when any of these change.
- `deviceProfile(device, id)` and `saveDeviceProfile(...)`: a device profile
  as plain data, and saving it with the result (ok, or errors per field).
- `gameProfiles()` and `saveGameProfile(...)`, `deleteGameProfile(...)`.
- `renderButtons(device, profile, page, draft)`: the grid as an image from a
  draft not yet saved; `renderButton(...)` for one.
- `lightFrames(draft)`: frames of the lighting preview for a draft.
- `effects()`: the effect `Spec`s, for the advanced lighting editor.
- `runningPrograms()`, `pickProgram()`, `pickPicture()`: the program list and
  the file dialogs.
- `recordKeys(start|stop)`: key recording for an action.
- `settings()` and `saveSettings(...)`; `checkUpdate()`, `installUpdate()`.
- `mixer()`: live levels for the Mixer tab.

No call writes to a device directly.

## Testing

- The Go side of every call has unit tests without a keyboard, as today:
  loading and saving profiles and game profiles, refusing what does not load,
  the migration of `switch_when_running`, the reload-or-apply decision.
- `games/` gets tests in `layout` like the layout files, including the
  switching order with two games running and "don't switch".
- A folder touched by the window still loads in the released `kb7screen`
  (the `games/` subfolder is ignored there).
- Saving a hand-written file keeps every setting, makes the backup once, and
  drops only comments and formatting.
- The pages get a desktop checklist, run on Windows 11 with the KB7, as the
  tray had: every tab, every save path, a game starting and ending, the
  window closed and opened, missing WebView2, light and dark.

## Left to the builder

- Which Go binding to WebView2 (it must build with `CGO_ENABLED=0`), and how
  the window's message loop sits next to the tray's.
- The exact names and data shapes of the calls.
- How the pages are written (plain HTML, CSS and JavaScript without a build
  step, like the site, is the default; anything else needs a reason).
- How key recording reads keys (the Raw Input code in `internal/keys` reads
  the KB7 only; recording on any keyboard may need more).
- The symbol set shown in the look picker.
