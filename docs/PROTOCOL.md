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
| String descriptors | indices present (`iManufacturer` 1, `iProduct` 2, `iSerialNumber` 3, `iConfiguration` 4, `iInterface` 5). Windows fetches only index 5 (22 bytes, language 0x0409) during enumeration; 1, 2 and 3 were never requested, so the manufacturer, product and serial strings are still unseen | partly observed (`01-plug-in-1`) |
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

## HID reports

From the report descriptors fetched during enumeration in `01-plug-in-1`.

| IF | Report ID | Usage page | Direction / size | Status |
|---|---|---|---|---|
| 0 | `0x00` | `0xff90` vendor | in 64 B, out 64 B, feature 64 B | observed (`01-plug-in-1`) |
| 1 | `0x00` | `0xff00` vendor | in 64 B, out 64 B | observed (`01-plug-in-1`) |
| 2 | `0x01` | `0x000c` consumer | in 3 B | observed (`01-plug-in-1`) |
| 2 | `0x02` | `0x0001` generic desktop | in 1 B | observed (`01-plug-in-1`) |
| 2 | `0x03` | `0xff01` vendor | in 7 B | observed (`01-plug-in-1`) |
| 2 | `0x04` | `0x0007` keyboard/keypad | in 20 B | observed (`01-plug-in-1`) |
| 2 | `0x33` | `0xff02` vendor | in 11 B | observed (`01-plug-in-1`) |
| 2 | 37 further IDs | `0xff03` vendor | **feature** only, 3–1043 B | observed (`01-plug-in-1`) |
| 3 | `0x00` | `0x0008` LED / `0x0007` keyboard | in 8 B, out 1 B | observed (`01-plug-in-1`) |

Interface 2 carries 42 reports in total. The ordinary input ones are the media
keys (`0x01`), a system control (`0x02`) and a 20-byte keyboard report (`0x04`,
wide enough for NKRO) alongside the 8-byte boot keyboard on interface 3.

The remaining 37 IDs on usage page `0xff03` are **feature** reports — not output
reports. A feature report is read and written with `GET_REPORT` / `SET_REPORT`
over the control endpoint, which matches the traffic mix in `01-plug-in-1`
(74 control IN and 11 control OUT transfers against a handful of interrupts).
Expect Swarm II's command surface to live here rather than on the interrupt OUT
endpoints `0x06` / `0x0d` / `0x0f`.

### Feature report sizes on interface 2, usage page `0xff03`

| Size | Report IDs |
|---|---|
| 1043 B | `0x08` |
| 1027 B | `0x2e`, `0x2f` |
| 687 B | `0x31` |
| 583 B | `0x29` |
| 519 B | `0x38` |
| 347 B | `0x11`, `0x21`, `0x22`, `0x23` |
| 307 B | `0x28` |
| 263 B | `0x37` |
| 179 B | `0x30` |
| 175 B | `0x26`, `0x27`, `0x32` |
| 169 B | `0xa6` |
| 163 B | `0x25` |
| 127 B | `0x0f` |
| 103 B | `0x16` |
| 91 B | `0x20`, `0x2b`, `0x2c`, `0x39` |
| 43 B | `0x17` |
| 19 B | `0x0d` |
| 15 B | `0x24`, `0x2a`, `0x2d` |
| 13 B | `0x12` |
| 12 B | `0x09` |
| 11 B | `0x35` |
| 7 B | `0x36` |
| 4 B | `0x0e` |
| 3 B | `0x05`, `0x06`, `0xa5` |

Two leads worth stating as leads and nothing more:

| Lead | Reasoning | Status |
|---|---|---|
| `0x08` (1043 B) and `0x2e`/`0x2f` (1027 B) carry bulk data, possibly screen images | 1027 = 1024 + 3 and 1043 = 1024 + 19: a 1 KiB payload behind a small header | hypothesis |
| `0x33` (in, 11 B, `0xff02`) carries touch events | the only vendor **input** report outside `0xff03`, so it is the one the device can push unprompted | hypothesis |

Neither has been tied to a Swarm II action. That work starts at scenario 03,
where a single deliberate change is diffed between two captures.

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
