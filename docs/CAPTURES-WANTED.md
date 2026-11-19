# Captures wanted

What the next Swarm II capture session should record, and why. Each item is
one scenario in the sense of [`CAPTURE-GUIDE.md`](CAPTURE-GUIDE.md): one action
per capture, every scenario twice, nothing typed while recording. Names follow
the existing numbering (`NN-short-name-1`). When an item is done, its findings
go into [`PROTOCOL.md`](PROTOCOL.md) and the item is removed here.

Done on 2026-11-19 (captures 41 to 47): taps on pages 2 and 3, renaming a
profile, the mixer list, levels and sliders, storing a profile's lighting and
brightness.

## Screen

1. **Media controls on the volume page.** With Swarm II running and music
   playing, open the volume page on the keyboard, then tap pause/play, next
   and previous, and let a track change by itself. Wanted: how Swarm II sends
   what is playing (title, state) and what a tap on those controls reports.
   Swarm II runs a helper named "Global System Media Control" for it. Goal:
   Commander shows and drives the media controls; today that part of the page
   is missing while Commander runs.
2. **A fifth and sixth program on the mixer.** With five or more programs
   playing, see which level frames Swarm II sends beyond `03 0e` and `06 0e`.
   Goal: more than four programs on the mixer page.

## Robustness

8. **A long session.** On 2026-11-18, after about an hour in Star Citizen with
   lighting streamed, the keyboard stopped answering on its control interface
   (profile switch and notification writes timed out; typing still worked)
   until it was unplugged. A capture of `kb7screen run` (not Swarm II) during
   a long session, ending at the switch that fails, would show the last
   exchanges before the hang. Large; only if it happens again.
