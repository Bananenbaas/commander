# Commander updates from GitHub releases

Design for letting Commander find a newer release and replace itself. Status:
draft for approval. Builds on [`commander-tray.md`](commander-tray.md).

## Intent

Releases now exist (`v0.1.0-beta.1` and on), built and checksummed by
`.github/workflows/release.yml`. Getting a new version today means opening the
releases page, downloading, stopping Commander, replacing the file and
starting it again. Commander should do that itself, when the user wants it
and only after asking. The settings window that will hold this later does not
exist yet, so for now it lives in the tray menu.

## Goals

- **Opt-in checking.** A menu item **Check for updates** with a tick, off by
  default. While ticked, Commander checks when it starts and once a day after
  that.
- **Check now**: a menu item that checks at once, ticked or not, and says the
  result in a balloon ("Commander v… is up to date", or the update found, or
  why the check failed).
- **Ask, never act alone.** When a newer release is found, a balloon says so
  ("Commander v0.1.0-beta.2 is available — click to update") and a menu item
  **Update to v0.1.0-beta.2** appears. Clicking either asks for confirmation in
  a message box that names both versions and links the release page; only Yes
  installs.
- **Install**: download `commander-windows-amd64.exe` and `SHA256SUMS.txt` of
  that release, check the file against its checksum, put the new file in place
  of the running one, stop the loop cleanly, start the new one with the same
  configuration folder, and end. The new Commander logs its version, removes
  the old file, and says in a balloon that it was updated.
- **Pre-releases count.** Every release so far is a beta; the newest release
  by version, beta or not, is the one offered.

## Non-goals

- Updating `kb7screen.exe` (the command line). Users of the command line
  download it themselves.
- Linux: there is no Commander there.
- Updating without asking, or in the background while a game runs.
- A settings window (comes later; the menu items move there then).
- Rolling back to an older release, or choosing a release.

## Anti-goals

- Running anything that did not pass the checksum of its own release.
- A new dependency, a separate updater program, or anything needing
  administrator rights.
- Leaving the user without a running Commander when a step fails.
- Nagging: one balloon per new version per run of Commander; a failed
  automatic check is logged, not shown.
- Sending anything about the user: the requests carry no identifiers beyond a
  `User-Agent` of `Commander/<version>`.

## Approaches considered

| Approach | For | Against |
|---|---|---|
| **Commander replaces itself, own code** (chosen) | No second program, no new dependency; the Windows calls it needs are few and already used nearby | The swap and its fallback must be tested carefully |
| A separate small updater program | Survives a Commander that crashes mid-update | A second executable to ship and keep up to date |
| A self-update library | Less own code | A new dependency for a few hundred lines, against how Commander is built |

Chosen by the owner: ask first (not update by itself), and the first approach.

## Design

### Finding the newest release

`GET https://api.github.com/repos/Bananenbaas/kb7-commander/releases?per_page=20`
over HTTPS, without authentication (60 requests an hour per address is far
more than one a day needs). Drafts are skipped. Each release's tag is read as
a semantic version: `v`, then `MAJOR.MINOR.PATCH`, then optionally `-` and
dot-separated pre-release parts. Versions compare by semantic-versioning
rules: numeric parts as numbers (`0.1.0-beta.10` > `0.1.0-beta.9`), and a
release above its pre-releases (`0.1.0` > `0.1.0-beta.9`). A tag with fewer
parts (`v1.0`), build metadata (`+...`) or anything else is not a version and
is skipped. The newest release that is newer than the running version and has
both `commander-windows-amd64.exe` and `SHA256SUMS.txt` among its assets is
the update. A development build (version `dev`) never offers an update;
**Check now** says it is a development build.

Timeouts: 15 s for the list, 5 minutes for the download; the download is
refused above 64 MB.

### Installing

The file names below stand for the running executable's real path as
`os.Executable` gives it, whatever the user named it (`commander.exe`,
`commander-windows-amd64.exe`, ...): `<exe>`, `<exe>.new`, `<exe>.old`. The
downloaded asset's own name is not used on disk.

1. Confirm with the user (message box, Yes/No).
2. Download `SHA256SUMS.txt`, find the line for
   `commander-windows-amd64.exe`, download the executable to `<exe>.new`
   (same folder, so the renames stay on one volume). Hash while writing; a
   mismatch deletes `.new` and stops with a message.
3. Rename the running `<exe>` to `<exe>.old` (Windows allows renaming a
   running executable), then `<exe>.new` to `<exe>`. If the second rename
   fails, the first is undone.
4. Only now stop the loop cleanly (as Quit does: keys released, stored
   lighting back; an upload in progress is finished first, as with any stop)
   and release the run lock and the per-user lock.
5. Start the new `<exe>` with the same folder argument and the flag
   `--updated-from <old version>`, then end.

Steps 2 and 3 run while the loop keeps going, so a failure there needs no
restart: `.new` is deleted, the old file stays (or is renamed back), and a
balloon and the log say what went wrong. The folder may not be writable (for
example under `Program Files`): that is one of those failures, and the message
says to move Commander to a folder of the user's own. If step 5 cannot start
the new file, the renames of step 3 are undone and the old file is started
the same way instead, without the flag, so the user is left with the old
Commander running.

After step 5 there is no watchdog: if the new file starts but fails at once,
`<exe>.old` is still there and the log says so; renaming it back is the way
out. This is accepted for now.

One update operation runs at a time: a check, a confirmation or an install.
**Check now** while one runs says so in a balloon; an automatic check that
falls due then is skipped. Declining the confirmation keeps the **Update to
...** menu item; the balloon for that version is not shown again in this run.

### The new Commander after an update

Started with `--updated-from <version>` (the old version string as stamped,
`v` included), it logs `updated from <old> to <new>`. Because the old process
may still be ending, it retries taking the run lock and the per-user lock for
up to 10 s before it gives up with the usual "already running" message. Once
its tray icon is up it deletes `<exe>.old` and shows a balloon "Commander
updated to v..."; if the delete fails it is left for the next start, which,
like every start, quietly deletes a leftover `<exe>.old` or `<exe>.new`. The
argument parser accepts the flag next to the folder; the flag is not
remembered and not written to the `Run` value.

### Settings

`%APPDATA%\Commander\settings.toml` gains `check_updates = true|false`
(missing = false). Ticking the menu item writes it. "Start with Windows" needs
no change: it points at the path of the executable, which stays the same.

### Code layout (suggested)

An `internal/update` package without Windows calls: version parsing and
comparison, picking the update from a release list, reading
`SHA256SUMS.txt`, downloading with a checksum and a size limit. It takes the
base URL and an `http.Client`, so tests run against `httptest`. The swap,
restart and menu wiring live in `cmd/commander` (Windows only). The tray gets
balloon clicks (`NIN_BALLOONUSERCLICK`): a balloon may carry its own click
action, and a click counts only for the balloon shown last when that one has
an action. Menu items get a label and a visibility that are asked each time
the menu opens, like `Checked` today.

## Testing

- Unit tests: version parsing and ordering (including pre-releases, numeric
  pre-release parts and invalid tags), picking the update (drafts, missing
  assets, older and equal versions, `dev`), checksum file parsing, download
  with a good checksum, a bad one, a too-large body and a server error, all
  against `httptest`; the swap's rename-and-undo with temporary files; the
  argument parser with `--updated-from`.
- A desktop check: a `dev`-built Commander says it is a development build; a
  build stamped with an older version finds the current release, asks,
  installs, restarts with the stored lighting kept in between, deletes
  `.old`, and still starts with Windows after a reboot.

## Left to the builder

The exact wording of the menu items, balloons and message box; how the daily
check is timed; how the release-page link is shown in the message box; the
package and function names.
