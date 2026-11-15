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
| Physical connection | the supplied **USB-A** connectors, on an AMD xHCI root hub, behind the keyboard's own hub. The keyboard also has a single **USB-C** port, so far untested | observed (`00-descriptors`, `01-plug-in-1`, `-2`, `00-idle-1`) |

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

Both leads below predate `00-idle-1`, which showed the live channel is
interface 1 rather than these feature reports. They are kept because the sizes
still suggest what the reports are *for*, not how they are used.

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

There is no Swarm II handshake in `01-plug-in`: Swarm II was closed. What
follows is the plain Windows enumeration, identical in structure across both
takes.

| Step | Direction | Transfer | Payload | Status |
|---|---|---|---|---|
| 1 | host → device | `SET_IDLE` on interfaces 0, 1, 2 and 3 | — | observed (`01-plug-in-1`, `-2`) |
| 2 | **device → host** | report `0x03` on `0x8c` (interrupt IN, interface 2) | 8 bytes, see below | observed (`01-plug-in-1`, `-2`) |
| 3 | host → device | `SET_REPORT` output, report `0x00`, interface 3 | `01` — boot-keyboard LED report (NumLock) | observed (`01-plug-in-1`, `-2`) |

Step 2 is the only thing the device sends unprompted during enumeration, and it
matches report `0x03` in the report map (interface 2, usage page `0xff01`, 7-byte
input plus the report ID):

```
take 1   03 00 31 20 d1 da 00 00
take 2   03 00 31 20 c9 da 00 00
                     ^^
```

| Field | Reading | Status |
|---|---|---|
| Byte 0 | report ID `0x03` | observed |
| Bytes 1–3 | `00 31 20`, identical in both takes | observed |
| Byte 4 | **varies between otherwise identical runs** (`0xd1` / `0xc9`) — treat as noise, not payload | observed |
| Byte 5 | `0xda`, identical in both takes | observed |
| Bytes 6–7 | `00 00` in both takes | observed |
| What byte 4 means | unclear. With byte 5 as a little-endian pair it reads `0xdad1` / `0xdac9`, eight apart — could be a tick counter, a sensor reading or a checksum | hypothesis |

Comparing the two takes also confirms the report map itself is **stable**: all 45
report-descriptor entries are byte-identical between the captures, so the map in
this document does not depend on a single capture.

Nothing else differed between the takes. Total traffic was 109 transfers in
take 1 against 113 in take 2, with the extra ones belonging to neighbouring
devices on the same root hub, not to the KB7.

### Interface 1: the Swarm II state stream

With Swarm II open and **nothing being touched**, interface 1 carries a
continuous request/response stream. This is where Swarm II actually talks to the
device — not the `0xff03` feature reports on interface 2.

From `00-idle-1`: 40 seconds of deliberate idling produced 1.4 MB, against 12 kB
for a whole enumeration. Traffic on the KB7 was 4926 transfers out on `0x0d` and
4926 in on `0x85`, and nothing at all on `0x81`, `0x06`, `0x8c`, `0x0f` or `0x8e`.

**Frame format** — 64 bytes, fixed:

| Offset | OUT on `0x0d` | IN on `0x85` | Status |
|---|---|---|---|
| 0 | `a1` constant | `32` constant | observed (`00-idle-1`) |
| 1 | bank number, `01`–`06` | the **same** bank number echoed back | observed (`00-idle-1`) |
| 2–3 | `68 01` on bank 1, `00 00` on banks 2–6 | `00 00` | observed (`00-idle-1`) |
| 4–63 | 60 bytes of payload | 60 zero bytes | observed (`00-idle-1`) |

`0x0168` = **360** = 6 banks × 60 payload bytes, so bank 1 declares the total
payload length of the cycle. The device's reply carries no data at all: it is a
pure acknowledgement, one per frame.

**Timing**: 821 complete cycles in 41.5 s — **19.8 Hz**, with a bank-1 interval
of 50.6 ms median (47.5 ms min, 53.5 ms max). Six frames per cycle is ~123
frames per second in each direction.

**The payload is static.** Every one of the six banks had exactly **one** unique
payload across all 821 cycles. Swarm II resends unchanged state twenty times a
second; it does not animate and it does not stream anything that varies.

**The reassembled 360-byte payload** contains only three byte values:

| Value | Count per cycle |
|---|---|
| `0xfe` | 289 |
| `0x00` | 64 |
| `0x7f` | 7 |

And it has a clear stride: **every `0x7f` at offset p is matched by a `0x00` at
offset p+16**, for all seven occurrences without exception —
13→29, 150→166, 153→169, 196→212, 202→218, 204→220, 205→221. Beyond offset ~250
the zeros fall in runs of five to six at roughly the same stride of 16, and the
final 30 bytes (330–359) are all zero.

| Reading | Status |
|---|---|
| A table with a 16-byte stride, `0xfe` meaning an empty or unassigned slot | hypothesis |
| This is device **state** (key bindings, layout or lighting), not pixel data — 360 static bytes at 20 Hz is orders of magnitude too little for a screen image | hypothesis |
| What the seven `0x7f`/`0x00` pairs represent | unknown |
| Why state is resent 20 times a second instead of on change | unknown |

**Correction to an earlier guess.** The feature reports on interface 2 were
written up as the likely command surface because they are reachable with
`SET_REPORT`. The idle capture shows the live channel is interface 1's interrupt
endpoints instead. The interface 2 feature reports may still carry configuration;
nothing has been seen on them yet.

### RGB lighting
unknown. Not seen in `00-idle-1`: the state stream on interface 1 was static
throughout, so whatever lighting was active was not being driven from the PC
frame by frame. Scenario 03 should show which channel a colour change uses.

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
   Not at all while idle: `00-idle-1` shows only a static 360-byte state frame
   repeated at 20 Hz, with no image-sized traffic anywhere. Scenario 07 has to
   turn the PC-performance view on explicitly.
3. Is there a checksum or sequence counter in write commands?
4. Which interface carries keystrokes besides the boot keyboard (NKRO)?
   Interface 2 exposes a keyboard collection next to its vendor-defined ones, so
   it is the first candidate — but this has not been seen in traffic.
5. The device presents three OUT endpoints (`0x06`, `0x0d`, `0x0f`). Swarm II
   uses `0x0d` for its state stream; `0x06` and `0x0f` have not been seen in use.
6. **Does connecting over USB-C change anything?** Every capture so far was made
   over the supplied USB-A connectors, where the device reports `bcdUSB 0x0200`.
   Over the keyboard's single USB-C port the speed, the product ID or the
   interface layout could differ, which would mean the notes here apply to one
   cabling only. Worth a `01-plug-in` pair over USB-C to compare.
7. What are the 360 bytes of the interface 1 state stream, and what do the seven
   `0x7f`/`0x00` pairs sixteen bytes apart represent?
