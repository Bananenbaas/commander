# Contributing

Thanks for wanting to help unlock the KB7. The project is in **Phase 0**:
collecting USB captures. Captures are the bottleneck, so a good capture is the
most valuable contribution there is.

Development conventions live in [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md).

## Contributing captures

1. Read [`docs/CAPTURE-GUIDE.md`](docs/CAPTURE-GUIDE.md) first. It covers the
   Wireshark/USBPcap setup, the naming scheme and the capture discipline.
2. **Never attach a raw `.pcap` / `.pcapng` to an issue or pull request.** A
   keyboard capture contains everything that was typed while it was recording.
   `captures/` is git-ignored for that reason.
3. Share the **output** of `kb7cap` instead, and read it before you post:

   ```sh
   python -m kb7tools summary captures/01-plug-in-1.pcapng
   python -m kb7tools diff captures/03-rgb-static-ff0000-1.pcapng \
                           captures/03-rgb-static-0000ff-1.pcapng -d <vid>:<pid>
   ```

4. Always mention your **Swarm II version** and the **KB7 firmware version**.
   The protocol may differ per firmware.
5. Record every scenario **twice**, so the noise (counters, sequence numbers,
   checksums) can be told apart from the real differences.

Use the *Protocol finding* issue template for anything you worked out from a
capture.

## Contributing to the protocol notes

[`docs/PROTOCOL.md`](docs/PROTOCOL.md) is the knowledge base, and it is kept
deliberately strict:

- every claim carries a status tag: `confirmed`, `observed`, `hypothesis` or
  `unknown`;
- every claim names the capture(s) it is based on;
- hex excerpts are short and reviewed, never whole captures;
- `confirmed` means it was both seen in a capture **and** reproduced by replay
  from this project's own code. Anything else is at most `observed`.

Promoting a claim to a higher status in the same pull request that adds the
evidence is fine, just say which capture did it.

## Contributing code

- Python **3.10+**, **standard library only**. If something seems to need a
  dependency, open an issue first.
- Every source file starts with `# SPDX-License-Identifier: GPL-2.0-or-later`.
- Everything in the repository is written in English.
- Before you open a pull request:

  ```sh
  python -m compileall -q kb7tools tests
  python -m unittest discover -s tests -t .
  ```

- New parsing behaviour needs a test. Tests build their own synthetic captures
  via `tests/synth.py` with the fake VID:PID **1234:5678**, and must not encode
  guesses about the real KB7 protocol.
- Keep pull requests focused: one capture area, parser fix or feature per PR.

## Hard rules

These are not negotiable, because they protect either someone's hardware or
someone's privacy:

- **Only commands observed in a capture are ever sent to a device.** No guessing,
  no fuzzing of write commands.
- **Firmware update, bootloader entry and factory reset are never implemented.**
  If you identify such a command, add it to the do-not-send list in
  `docs/PROTOCOL.md` and leave it there.
- **Clean room.** The protocol is derived solely from observing USB traffic of
  hardware we own (EU Directive 2009/24/EC art. 5(3)). Swarm II binaries,
  firmware images and assets are not analysed, decompiled or included, not in
  the repository, not in issues and not in pull requests.
- **No raw captures in public**, ever.

Security- or privacy-sensitive findings go to [`SECURITY.md`](SECURITY.md)
instead of a public issue.

## License

By contributing you agree that your contribution is licensed under
[GPL-2.0-or-later](LICENSE), the license of this project.
