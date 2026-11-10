# Changelog

All notable changes to this project are documented here.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added
- `kb7cap` capture toolkit (stdlib only): pcap/pcapng reader, USBPcap and
  Linux usbmon transfer pairing, device/configuration/string/HID report
  descriptor parsing, and `summary`, `dump` and `diff` commands.
- Boot-keyboard reports are hidden by default in `dump` and `diff`.
- Capture guide for Phase 0, protocol notes skeleton, PNG test images with
  known pixel values.
- CI: unit tests on Python 3.10 and 3.12; Dependabot for GitHub Actions.
