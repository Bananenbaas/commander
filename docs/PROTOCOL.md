# KB7 USB protocol

Working notes on the Turtle Beach Command Series KB7 USB protocol, derived from
captures (see `CAPTURE-GUIDE.md`). Nothing here comes from Turtle Beach.

**Status legend**

| Tag | Meaning |
|---|---|
| `confirmed` | Seen in captures **and** reproduced by replay from our own code |
| `observed` | Seen consistently in captures, not yet replayed |
| `hypothesis` | Interpretation that still needs a capture to prove or disprove it |
| `unknown` | Not investigated yet |

Every entry names the capture(s) it is based on.

## Device identity

| Field | Value | Status |
|---|---|---|
| VID:PID | — | unknown |
| Firmware version | — | unknown |
| Swarm II version used | — | unknown |
| USB interfaces | — | unknown |

## Interfaces and endpoints

_Fill in from `kb7cap summary` on `01-plug-in`._

## HID reports

| Interface | Report ID | Type | Size | Usage page | Purpose | Status |
|---|---|---|---|---|---|---|

## Command catalogue

Each command: direction, transport (SET_REPORT / interrupt / bulk), byte layout,
example hex, reply, captures.

### Device info / handshake
unknown

### RGB lighting
unknown

### Touchscreen: button layout and pages
unknown

### Touchscreen: image upload
unknown. Pixel format, chunk size, addressing, checksum.

### Touch input events
unknown

### Profiles
unknown

## Do-not-send list

Commands identified as firmware update, bootloader entry or factory reset.
These are never replayed and never implemented in the library.

_None identified yet._

## Open questions

1. Does the screen accept raw frames from the PC, or does it render from assets
   stored on the device? (captures 06, 07)
2. Are live PC stats pushed as rendered images, as numbers, or not at all?
3. Is there a checksum or sequence counter in write commands?
4. Which interface carries keystrokes besides the boot keyboard (NKRO)?
