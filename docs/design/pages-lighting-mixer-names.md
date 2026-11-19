# Pages 2 and 3, stored lighting, the volume mixer and profile names

Design for four features unlocked by the captures of 2026-11-19 (`41`–`46`,
see `docs/PROTOCOL.md`). Status: approved 2026-11-19; implemented, desktop check pending. Builds on
[`screen-app.md`](screen-app.md), [`lighting.md`](lighting.md) and
[`commander-tray.md`](commander-tray.md).

## Intent

The owner wants more buttons for Star Citizen than fit on one page (power
triangle, defence, mining), the keyboard to look right when Commander is not
running (today every profile shows a red stored table), the keyboard's volume
mixer page to show and control the real programs (today it shows
"APP NAME 1" to "6" at 50), and the profiles to carry their own names.

## Goals and how they look in a layout file

### 1. Buttons on pages 2 and 3

`page = 2` or `page = 3` on a `[[button]]` is accepted (today only 1). Positions
stay 0–11 per page; page 3 position 11 is refused (it has no `0x29` entry).
`apply` writes `0x29` entry `12(page-1)+position` and `0x25` entry
`13(page-1)+position`; `run` matches touches by page (byte 1 of `0x33`) and
position. The 15 picture slots are shared by all pages and profiles. The
preview draws one panel per page.

### 2. Stored lighting

What the keyboard shows when nothing streams — when Commander is not running,
on a profile without `[light]`, and for a moment during a reload — is the
profile's stored `0x11` table. `apply` sets it per key:

- **A profile with `[light]`** gets a still picture of its own layers: the
  layers drawn at their starting point (no ripples, states at their `start`
  value, waves and breathing at time zero). White keys stay white, the rim
  and WASD keep their colours. Nothing extra to configure.
- **`[stored_light]`** overrides that, or sets a profile without `[light]`:

```toml
[stored_light]
color = "#FFFFFF"     # every LED in one colour
brightness = 80       # percent, 0–100; optional
```

  A layout file may consist of only `profile = N` and `[stored_light]`, to set
  a profile kb7screen has no buttons on.
- The brightness (byte 6 of `0x11`) stays what the keyboard holds unless
  `[stored_light]` gives one.
- `apply` writes the block only when it differs from what the keyboard holds.
  A profile with neither `[light]` nor `[stored_light]` is left alone.

### 3. The volume mixer (Windows)

While `run` (and so Commander) holds the keyboard, it keeps the mixer page in
step with Windows:

- It follows the playback sessions of the default output device through the
  Windows audio session API (COM, called directly; no new dependency), and is
  told by Windows when something changes: a session appears or ends
  (`IAudioSessionNotification`), a program's volume changes
  (`IAudioSessionEvents`), the system volume changes
  (`IAudioEndpointVolumeCallback`), or the default device changes
  (`IMMNotificationClient`). On a change it writes report `0x28`: the system
  volume first, then up to four programs (only the level frames for programs 1-2 and 3-4,
  `03 0e` and `06 0e`, have been observed) by their display name (the executable
  name without `.exe` when a program gives none). Nothing is polled while
  nothing changes.
- Which programs: sessions that are expired are dropped; a program that
  plays sound comes before one that is only listed (Windows keeps sessions of
  programs that once played). A shown program keeps its row while it plays;
  free rows go to playing programs first, then by first appearance. Rows are
  shown in first-appearance order, so pausing does not reorder them.
- It sends the levels as `a3` frames (`00 0d` for the system volume, `03 0e`
  and `06 0e` for the program pairs), when they change.
- A slider moved on the keyboard (`33 00 <row> 07 …  <level>`) sets that
  program's volume in Windows; the system slider sets the system volume.
- Linux: not in this round.

No configuration is needed; a switch to turn it off can come later if wanted.

### 4. Profile names

```toml
name = "SC"           # top level of a layout file, beside profile = 1
```

`apply` writes report `0x16` (all five names, the other four as read) when the
name differs. At most 19 printable ASCII characters; the screen shows short
names best.

## Non-goals

- Restoring factory buttons of a profile (no capture of a reset).
- Choosing the background from the PC (the keyboard does that itself).
- The mixer on Linux; mute buttons on the mixer page; program icons.

## Anti-goals

- Changing anything the layout files do not ask for: no `[stored_light]`
  means the stored table is untouched; no `name` means the name is untouched.
- Breaking the existing page 1 behaviour or the ledger format.
- Logging or storing which programs play sound beyond the running process.

## Approaches considered

| Choice | Chosen | Instead of | Why |
|---|---|---|---|
| Where stored lighting lives | a `[stored_light]` block in the layout file, written by `apply` | written by `run` at start | `apply` already owns writes that persist on the keyboard; `run` stays a streaming loop |
| Mixer audio access | direct COM calls through `golang.org/x/sys/windows` | a COM library | no new dependency, as with the tray |
| Mixer refresh | Windows audio notifications (COM callbacks implemented in Go) | polling about once a second | changes appear at once and nothing runs while nothing changes; chosen by the owner over the smaller polling build |
| Stored lighting | a still picture of the profile's own layers, per key, with `[stored_light]` to override | one colour per profile | the keyboard looks like the owner's lighting when Commander is off, without extra configuration; chosen by the owner |

## Testing

Unit tests for the new table writes (`0x11`, `0x16`, `0x28`, page indexes),
config parsing and refusals, touch matching on pages 2 and 3, the mixer's
change detection and frame building with a fake audio source. The Windows
audio calls get a small manual check (`kb7screen` command or log lines). A
desktop check with the owner per feature.

## Left to the builder

Package layout, how the COM callbacks are kept alive and on which thread
they run, how the mixer's audio part is split for testing, log wording.
