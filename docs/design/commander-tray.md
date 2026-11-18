# Commander — a tray program around `kb7screen run`

Design for stopping, reloading and starting `run` without a terminal or Task
Manager. Status: approved; implemented, desktop check done (see Desktop check). Builds
on [`screen-app.md`](screen-app.md) and [`lighting.md`](lighting.md).

## Intent

`run` keeps the screen buttons and the lighting alive, so it runs all the
time, hidden. Today the only way to stop it is Task Manager, which skips the
clean shutdown (keys released, the stored lighting put back, the keyboard
closed), and every change to the configuration means killing it and starting
it again by hand.

**Commander** is the new name of the application as a whole: today it drives
the KB7, later perhaps other devices. Its first face is a small program in the
Windows notification area (system tray) with an icon of its own: it runs
`run`, and its menu stops it cleanly, reloads the configuration and switches
starting with Windows on and off. `kb7screen` stays the command-line tool.

## Goals

- `commander.exe`: a Windows program without a console window that runs the
  same loop as `kb7screen run` for one configuration folder, with a tray icon.
- Tray menu:
  - **Reload configuration**: loads the folder again and, if it is valid,
    restarts the loop with it, cleanly; if it is not, keeps running the old
    one and shows the error.
  - **Apply buttons**: stops the loop cleanly, runs `apply` on the folder,
    starts the loop again — for changes to pictures, titles or positions,
    which `apply` refuses while `run` runs.
  - **Start with Windows**: a tick mark; on and off.
  - **Open configuration folder**, **Open log**.
  - **Quit**: the clean stop.
- The icon's tooltip says what it is doing: the active profile, "waiting for
  the keyboard", "waiting for Swarm II to close".
- `kb7screen stop <config>` and `kb7screen reload <config>` do the same from
  the command line, for any `run` of that folder (also on Linux).
- An icon designed for Commander, used for the tray and the executable.

## Non-goals for now

- A settings or layout editor (the GUI comes later; this menu is not it).
- The auto-update (comes with releases and the GUI).
- A tray icon on Linux: notification areas differ per desktop there. Linux
  keeps `kb7screen run` with `stop` and `reload`.
- Renaming the repository, the Go module or `kb7screen`.

## Anti-goals

- Anything that can stop `run` without its clean shutdown when a clean one
  was possible.
- A console window flashing at Windows start.
- New dependencies that need cgo, or a library for the tray.
- Writing to the system outside the user's own space: starting with Windows
  is the user's own `Run` key, nothing machine-wide, no administrator rights.

## Approaches considered

| Approach | For | Against |
|---|---|---|
| **Two programs from the same code** (chosen): `kb7screen.exe` as now, `commander.exe` built as a windowless Windows program | No console window ever; command-line output stays where it is | Two files to ship |
| One program that hides its own console | One file | A black window flashes at every start; output and a hidden program fight in one executable |
| A tray library (e.g. getlantern/systray) | Less own code | Needs cgo on Linux and macOS; an extra dependency for one menu |

Chosen by the owner: two programs; the tray built on the Windows API itself.

## Design

### Requests to a running loop

A running loop already holds `.kb7screen-run.lock` in the configuration
folder. `kb7screen stop` and `reload` write a request file next to it,
`.kb7screen-request`, containing one word, `stop` or `reload`. The loop looks
for it at every poll (every 2 s), removes it and acts:

- `stop`: the same path as Ctrl+C — release held keys, send the stored
  lighting, close the keyboard, end.
- `reload`: load the folder; if that fails, log the error and keep running;
  otherwise stop cleanly as above and start again with the new configuration,
  without releasing the lock in between.

`stop`/`reload` without a running loop (lock not held) say so and write
nothing. They write the request atomically (a temporary file renamed into
place), wait up to 10 s for it to be taken (the file gone) and report whether
it was. A request file found when the lock is first taken is stale (left by a
crash) and is deleted unread.

A reload tears down and rebuilds everything that depends on the
configuration — the light engine, the key reader, the loop — and keeps only
the lock, the process and the keystroke and program launchers.

Commander uses the same two actions in-process, without the file. A `stop`
request to a folder that Commander runs ends Commander itself, cleanly.

**One keyboard, one owner.** Besides the per-folder lock, `kb7screen run` and
Commander take a per-user named mutex (`Local\Commander-KB7`), so two of them
on different folders cannot fight over the keyboard; the second one says which
is running and ends. `apply` and `restore` refuse while it is held, except when
Commander runs them itself.

### Commander

- Started as `commander.exe [config folder]`. The folder comes from the
  argument, else from Commander's own settings
  (`%APPDATA%\Commander\settings.toml`: the folder and nothing else for now).
  An argument is remembered there. Without either, a message box explains how
  to start it once with a folder, and it ends.
- One Commander per folder: it takes the run lock; if that is held it says
  "Commander is already running for this folder" and ends.
- The tray icon: a hidden window receiving the notification-area messages,
  `Shell_NotifyIconW` for the icon, tooltip and balloon messages, a pop-up
  menu on right click; left click opens the menu too.
- The loop runs on its own goroutine as `run.Run` does today; the tray's
  window thread only sends it requests and shows its state. `run.Deps` gains a
  way to report state changes (active profile, waiting reasons) and to take
  requests.
- **Apply buttons**: stop the loop, call `apply` in-process on the same
  folder (with the same refusals: Swarm II running), show the result as a
  balloon, start the loop again. The lock stays held throughout and `apply`
  is told it may run under it.
- **Start with Windows**: the value `Commander` under
  `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`, set to
  `"<path of commander.exe>" "<config folder>"`; the tick shows whether the
  value exists and points at this executable.
- Log: `%LOCALAPPDATA%\Commander\commander.log`, the lines `run` logs today,
  with the date; when it passes 1 MB it is renamed to `commander.log.1` and a
  new one started. Key presses are never logged (as now).
- Errors that need the user (configuration invalid at reload, apply failed,
  keyboard upload left open) show a balloon; the upload-left-open case also
  says to unplug the keyboard, as the command line does.
- **Commander never disappears on an error.** An invalid configuration at
  start (for instance after an edit and a reboot) or a loop that ends with an
  error leaves Commander in the tray in an error state: the icon gets a red
  mark, the tooltip and a balloon say what is wrong, and **Reload** starts the
  loop from scratch once it is fixed. Only Quit, a `stop` request and the end
  of the Windows session end it.
- **Apply buttons** restarts the loop in every case, also when `apply` refused
  (Swarm II running, keyboard absent) or failed, and says the outcome in a
  balloon.
- **End of the Windows session** (log off, shut down): the clean stop, limited
  to 5 s; an upload in progress is never cut short by Commander itself.

### The icon

Drawn in code like the button symbols, so it can be regenerated: a dark
rounded square with a retro space helmet in the project's blue (`#00AAFF`) —
dome, visor, a white stripe and an antenna — readable at 16 × 16. An own
design in a general retro style, not a copy of any existing character. Rendered at 16, 20, 24, 32, 48 and 256 pixels into a
`.ico`, embedded in `commander.exe` as its resource and used for the tray.

## Testing

- Unit tests: request file handling (write, take, wait, no lock → refused),
  reload with a broken configuration keeps the old one, the loop's state
  reports, the `Run` key value building and parsing, log rotation, the icon
  generator (sizes present, valid `.ico` header).
- The tray window itself has no unit test; a hardware and desktop checklist:
  start, tooltip, every menu item, start with Windows after a reboot, quit
  leaves the stored lighting, a second Commander refused.

## Desktop check (2026-11-18, Windows 11)

All on the owner's PC with the owner's Star Citizen configuration folder.

| Check | Result |
|---|---|
| Icon in the notification area, tooltip | Seen; tooltip followed the profile (`profile 0`, then `profile 1`) |
| Menu | Reload configuration, Apply buttons, Start with Windows, Open configuration folder, Open log, Quit |
| Reload after a lighting change | The changed colour showed once back on the lit profile |
| Reload with a syntax error | Balloon with the parse error; the old lighting kept running; Reload after the fix took the new one |
| Apply buttons | Balloon and log: 0 pictures uploaded, 9 unchanged; loop running again |
| Second Commander on the same folder | Refused with a message box; the first one kept running |
| `kb7screen stop` from a terminal | "the loop has stopped"; Commander ended, icon gone |
| Quit | Effects stopped; the keyboard showed its stored lighting (the same as with no program running) |
| Start with Windows, reboot | Tick shown, `Run` value written; after a reboot Commander started and took the keyboard |

Seen during the check, not changed here:

- Every (re)start of the loop — Reload and Apply buttons included — switches
  the keyboard to the profile `run` expects (the desktop profile when no
  listed program runs), also when the owner had tapped to another profile.

## Left to the builder

Package layout (`cmd/commander`, an `internal/tray` for the Windows parts),
the exact menu wording, balloon texts, how the `.ico` resource gets into the
executable (a generated `.syso` committed to the repository is fine), and the
icon's exact shape within the description above.
