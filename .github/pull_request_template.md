## What this changes

<!-- One or two sentences. Link the issue if there is one. -->

## Why

<!-- What problem does it solve, or what did a capture reveal? -->

## Checklist

- [ ] `python -m compileall -q kb7tools tests` passes
- [ ] `python -m unittest discover -s tests -t .` passes
- [ ] New or changed parsing behaviour is covered by a test
- [ ] Tests use the fake VID:PID 1234:5678 and encode no guesses about the real protocol
- [ ] New source files carry `# SPDX-License-Identifier: GPL-2.0-or-later`
- [ ] Standard library only — no new dependencies
- [ ] Everything added is written in English

### If this touches `docs/PROTOCOL.md`

- [ ] Every claim has a status tag (`confirmed` / `observed` / `hypothesis` / `unknown`)
- [ ] Every claim names the capture(s) it is based on
- [ ] `confirmed` is only used for behaviour reproduced by replay from this project's own code
- [ ] Hex excerpts are short and were reviewed for keystrokes

### Safety and privacy

- [ ] No raw capture files are included in this PR
- [ ] No firmware-update, bootloader or factory-reset command is implemented or replayed
- [ ] No Swarm II binaries, assets or decompiled code are included
