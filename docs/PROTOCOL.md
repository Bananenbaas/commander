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
| VID:PID | `10f5:5038` (Turtle Beach) | observed (`00-descriptors`) |
| bcdDevice | `0x0139` | observed (`00-descriptors`) |
| bcdUSB | `0x0200` — full/high speed, not SuperSpeed | observed (`00-descriptors`) |
| Device class | `0x00` — composite, class per interface | observed (`00-descriptors`) |
| bMaxPacketSize0 | 64 | observed (`00-descriptors`) |
| USB interfaces | 4, all HID (class `0x03`) | observed (`00-descriptors`) |
| Configurations | 1; `bMaxPower` 250 → 500 mA, `bmAttributes` `0xa0` (bus powered, remote wakeup) | observed (`00-descriptors`) |
| Integrated hub | `10f5:503d`, bcdDevice `0x1720`, class `0x09` — the passthrough port, a sibling device on the same bus | observed (`00-descriptors`) |
| Firmware version | 1.39 — `bcdDevice` `0x0139` matches the version Swarm II reports | observed (`00-descriptors`) |
| String descriptors | indices present (`iManufacturer` 1, `iProduct` 2, `iSerialNumber` 3, `iConfiguration` 4, `iInterface` 5) but the strings themselves are not in a capture yet | unknown |
| Swarm II version used | — | unknown |

## Interfaces and endpoints

All four interfaces are HID with a single alternate setting, and every endpoint
is interrupt with `wMaxPacketSize` 64. Interfaces 0, 1 and 2 each have an **OUT**
endpoint, so there are three separate write channels to the device.

| IF | Subclass / protocol | IN ep | OUT ep | bInterval IN/OUT | Report descriptor | Status |
|---|---|---|---|---|---|---|
| 0 | `0x00` / `0x00` — vendor-defined | `0x81` | `0x06` | 4 / 4 | 31 bytes | observed (`00-descriptors`) |
| 1 | `0x00` / `0x00` — vendor-defined | `0x85` | `0x0d` | 4 / 4 | 34 bytes | observed (`00-descriptors`) |
| 2 | `0x00` / `0x00` — vendor-defined | `0x8c` | `0x0f` | 1 / 4 | 457 bytes | observed (`00-descriptors`) |
| 3 | `0x01` / `0x01` — HID boot keyboard | `0x8e` | — | 1 / — | 68 bytes | observed (`00-descriptors`) |

`bcdHID` is `0x0111` on all four. Interface 3 is the boot keyboard: keystrokes
travel on `0x8e`, which is why `kb7cap` hides that endpoint by default.

Interface 2 has by far the largest report descriptor (457 bytes) and Windows
splits it into six HID collections — consumer control, a system controller, a
keyboard and three vendor-defined ones. That makes it the likely home of the
media keys and the extra key rows rather than of the touchscreen. Interfaces 0
and 1 are small and purely vendor-defined, which makes them the candidates for
the touchscreen and lighting command channels. **Which interface carries what is
not established**: nothing has been matched to a Swarm II action yet.

| Question | Status |
|---|---|
| Does interface 0 or 1 carry the touchscreen commands? | hypothesis |
| Which interface carries RGB lighting? | unknown |
| What are the 457 bytes of interface 2's report descriptor? | unknown — needs the descriptor itself, which `--inject-descriptors` does not fetch |

## HID reports

The report descriptors themselves are not in a capture yet: USBPcap's
`--inject-descriptors` supplies the device and configuration descriptors but not
the HID report descriptors. Those need a real `01-plug-in` capture, where
Windows fetches them during enumeration.

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
   Interface 2 exposes a keyboard collection next to its vendor-defined ones, so
   it is the first candidate — but this has not been seen in traffic.
5. The device presents three OUT endpoints (`0x06`, `0x0d`, `0x0f`). Does Swarm II
   use all three, and does each one own a subsystem?
