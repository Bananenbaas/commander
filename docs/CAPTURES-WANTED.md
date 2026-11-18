# Captures wanted

What the next Swarm II capture session should record, and why. Each item is
one scenario in the sense of [`CAPTURE-GUIDE.md`](CAPTURE-GUIDE.md): one action
per capture, every scenario twice, nothing typed while recording. Names follow
the existing numbering (`NN-short-name-1`). When an item is done, its findings
go into [`PROTOCOL.md`](PROTOCOL.md) and the item is removed here.

## Profiles

1. **Rename a profile.** In Swarm II, rename one profile (for example
   `DEFAULT 03` to `TEST`), then rename it back. Wanted: how report `0x16`
   (the five names) is written, and whether the keyboard answers or redraws.
   Goal: Commander can name profiles itself.
2. **Reset a profile to its defaults**, if Swarm II offers it (for `DEFAULT 03`,
   whose launcher buttons were emptied on 2026-11-18). Wanted: the tables
   `0x29` and `0x25` (and anything else) it writes, so the defaults are known
   and `kb7screen` can put them back.

## Screen

3. **Tap a button on page 2 and on page 3** with the notifications on (Swarm II
   running). Wanted: byte 1 of the touch report `0x33` on those pages. Goal:
   buttons on pages 2 and 3.
4. **Choose a background on the keyboard while recording**, and separately
   **upload a new background while it is the chosen one**. Wanted: whether the
   host can choose a background itself, and whether an upload over the chosen
   one shows at once (the owner's observation, not measured yet).
5. **The volume mixer page.** With Swarm II running and two or three programs
   playing sound, open the mixer page on the keyboard, change a program's
   volume in Windows, then drag a slider on the keyboard. Wanted: how Swarm II
   sends the program names, icons and levels, and what a slider move reports.
   Goal: Commander fills the mixer page (today it shows the placeholders
   "APP NAME 1" to "6" at 50).

## Lighting

6. **Save a lighting setting to a profile.** In Swarm II, give one profile a
   static colour and save it, then a different one. Wanted: the full sequence
   that stores report `0x11` for a profile (writing `0x11` alone only makes
   the keyboard flicker). Goal: Commander can set the lighting the keyboard
   shows without any program running.
7. **Brightness from the host.** Move Swarm II's brightness setting, if it has
   one, from low to high. Wanted: whether the host can set the brightness, not
   only read it.

## Robustness

8. **A long session.** On 2026-11-18, after about an hour in Star Citizen with
   lighting streamed, the keyboard stopped answering on its control interface
   (profile switch and notification writes timed out; typing still worked)
   until it was unplugged. A capture of `kb7screen run` (not Swarm II) during
   a long session, ending at the switch that fails, would show the last
   exchanges before the hang. Large; only if it happens again.
