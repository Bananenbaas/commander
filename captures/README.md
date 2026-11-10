# captures/

Local working directory for raw USB captures. **Everything in here except this
file is git-ignored**, because a raw capture of a keyboard contains every key
pressed while it was recording.

Naming scheme (see `docs/CAPTURE-GUIDE.md`):

    <NN>-<area>-<action>[-<variant>].pcapng
    e.g. 03-rgb-static-ff0000.pcapng, 03-rgb-static-0000ff.pcapng

Findings go into `docs/PROTOCOL.md` as short, reviewed hex excerpts, never as
whole capture files.
