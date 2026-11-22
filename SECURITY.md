# Security and privacy reporting

This project reverse-engineers the USB protocol of a keyboard. Two kinds of
report need handling in private rather than in a public issue:

- **Privacy**: a capture, a log or a piece of `kb7cap` output in this repository
  (or in an issue, a pull request or a discussion) that leaks keystrokes or
  other personal data.
- **Hardware safety**: a command or code path that could brick a device:
  anything that looks like firmware update, bootloader entry or factory reset,
  or a bug in this project's code that would send such a command.

Classic software vulnerabilities count too, of course: this code parses
untrusted binary input (capture files) and will eventually talk to USB devices.

## How to report

Use GitHub's private reporting: **Security → Report a vulnerability** on
<https://github.com/Bananenbaas/commander>. That keeps the report between
you and the maintainer until it is resolved.

Please include:

- what the problem is and why it is sensitive;
- how to reproduce it, but **do not attach a raw capture**; a short reviewed
  hex excerpt or `kb7cap` output is enough;
- the Swarm II and KB7 firmware versions, if relevant;
- whether the issue is already public somewhere.

Expect an acknowledgement within a few days. This is a spare-time project, so
please allow reasonable time for a fix before disclosing publicly.

## Scope

In scope: the code in this repository, the documentation, and anything
committed here that should not have been.

Out of scope: vulnerabilities in Turtle Beach Swarm II or in the KB7 firmware
itself. Report those to Turtle Beach; this project is not affiliated with
them. If such a finding also affects users of this project, you are welcome to
report it here as well so the risk can be documented.

## A note on risk

Using this software is at your own risk and may void your warranty. The
project's safety rules (replay only observed commands, never implement
firmware or reset commands, no write fuzzing) exist to keep that risk small,
and they are documented in [`CONTRIBUTING.md`](CONTRIBUTING.md) and
[`docs/PROTOCOL.md`](docs/PROTOCOL.md).
