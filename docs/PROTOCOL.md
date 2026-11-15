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
| 2 | host → device | `SET_REPORT` output, report `0x00`, interface 3 | `01` — boot-keyboard LED report (NumLock) | observed (`01-plug-in-1`, `-2`) |
| 3 | **device → host**, **4.9 s later** | report `0x03` on `0x8c` (interrupt IN, interface 2) | 8 bytes, see below | observed (`01-plug-in-1`, `-2`) |

An earlier version of this table put the `0x03` report *between* the `SET_IDLE`
requests. That was an artefact of the parser timestamping interrupt IN transfers
when the request was queued instead of when the data arrived (see
`DEVELOPMENT.md`). With that fixed, the report arrives **4.90 s** after the LED
report in take 1 and **4.89 s** in take 2 — the same delay twice. After
plugging in, the screen stays dark and then shows a boot screen with the
firmware version for several seconds, so this report is very likely the keyboard
announcing that it has finished booting. Status: **hypothesis**.

It matches report `0x03` in the report map (interface 2, usage page `0xff01`,
7-byte input plus the report ID):

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

**The payload is the live LED table.** An earlier reading of this section
treated `0xfe` as filler marking an unassigned slot. That was wrong. The 360
bytes are per-LED colour, and `0xfe` is simply red at full brightness.

Setting a single key proved it. With the keyboard at 255, 51, 51 and **only
Escape** changed to 16, 64, 192, the reassembled payload moved in exactly three
bytes:

```
offset  0:  fe -> 0f        block 0, R plane, index 0
offset 16:  32 -> 3f        block 0, G plane, index 0
offset 32:  32 -> bf        block 0, B plane, index 0
```

Same LED index, same three planes, same sixteen-byte stride as report `0x11` —
and the same block-of-48 layout, just without the 10-byte header:

```
360 = 336 + 24
336 = 7 blocks x 48, each R[16] G[16] B[16]  ->  the same 112 LEDs
 24 = trailing bytes, purpose unknown
```

**Reassemble before interpreting.** The six banks carry 60 bytes each, while the
LED blocks are 48, so the two grids do not line up: only banks 1 and 5 begin on
a block boundary (payload offsets 0 and 240, both multiples of 48). An LED's
three bytes can therefore straddle two banks. Always concatenate all six banks
into the 360-byte payload first and index into that; reading LED positions
straight out of a single bank only works by accident.

**The values are scaled down by one.** `floor(v x 255/256)`:

| Configured | On the wire |
|---|---|
| 255 | `0xfe` (254) |
| 192 | `0xbf` (191) |
| 64 | `0x3f` (63) |
| 51 | `0x32` (50) |
| 16 | `0x0f` (15) |
| 0 | `0x00` (0) |

That also explains the byte histogram of a static 255, 51, 51: 101 bytes of
`0xfe` are reds at full, 188 bytes of `0x32` are the greens and blues, 64 zeros
are LEDs that are off, and the seven `0x7f` are LEDs sitting at 128.

| Finding | Status |
|---|---|
| The `a1` stream carries the live 112-LED colour table, in the same layout as report `0x11` | **confirmed** (`03-rgb-perkey-esc-1040c0-1`) |
| Values are transmitted as `floor(v x 255/256)` | observed (five distinct values) |
| What the trailing 24 bytes hold | unknown |
| Why the table is resent 20 times a second rather than on change | unknown |

#### Report `0x11` is configuration, not the display

This settles something that writing the table taught the hard way. Report `0x11`
can be written successfully from our own code: `HidD_SetFeature` with the
348-byte frame returns success and the device's status register, report `0x05`,
reads back `05 01 00 00` — byte for byte what Swarm II sees after its own writes.
`12-replay-led-block0-1` records our frame on the wire, identical in shape to
Swarm II's.

**And the lighting barely changes.** What is actually visible is a brief
flicker: the keyboard jumps to the written table and is back to Swarm II's
colours before you can make out what it showed. That is the stream winning.
Swarm II pushes its own copy every 50 ms, so anything written to `0x11` survives
at most one frame.

The flicker is useful evidence rather than a nuisance: it shows the write does
reach the LEDs, so `0x11` is not ignored. It is stored configuration that the
device applies once, and the stream is what the LEDs obey from then on.

#### Streaming our own LED frames works, and contends with Swarm II

Driving the lighting means writing `a1` frames to `0x0d` — the same endpoint and
mechanism as the `a3` volume frame. `13-replay-led-stream-green-1` does exactly
that: the 360-byte payload from a real capture with all 112 LEDs set to green,
split into the usual six banks, written at 30 Hz for eight seconds. 1188 frames
went out, none failed.

The keyboard strobed violently between our green and Swarm II's red. The capture
explains why precisely: in the eight-second window 356 bank-1 frames reached the
device, **198 ours and 158 Swarm II's** — a 56% share. The LEDs were following
whichever frame arrived last, roughly twenty-eight times a second.

| Finding | Status |
|---|---|
| The `a1` stream drives the LEDs, and frames written from our own code are rendered | **confirmed** (`13-replay-led-stream-green-1`) |
| A frame is 64 bytes: `a1 <bank 01–06> <len-lo> <len-hi>` plus 60 payload bytes, with the length only on bank 1 | **confirmed** |
| Writing goes to the interface 1 collection as a 65-byte `WriteFile`, exactly like the `a3` volume frame | **confirmed** |
| The device renders the most recent frame; there is no arbitration between writers | observed |
| Neither writer loses frames under contention: all 1188 of ours and all 3734 of Swarm II's appear in the capture | observed (`13-replay-led-stream-green-1`) |

**The architectural consequence.** Lighting cannot be shared. Whoever streams
last owns the LEDs, so taking over the keyboard means Swarm II has to stop
streaming first — which is the whole point of the project, but it also means a
replacement must *keep* streaming at 20 Hz or better for as long as it wants the
lighting to hold. There is no fire-and-forget equivalent for colour, unlike the
volume frame.

Do not run this test while Swarm II is open unless the strobing is wanted: it is
harmless, but it is unpleasant to look at.

Reading report `0x11` back directly does not work: `HidD_GetFeature` fails with
error 31 for any buffer length. Swarm II reads it through the selector register
instead (write `05 00 b0 00`, then read). Note also that a status read of report
`0x05` needs a 64-byte buffer; a 4-byte one fails with error 31 until a longer
read has succeeded once.

**Two channels, two jobs.** `00-idle-1` alone suggested interface 1 had replaced
the interface 2 feature reports as the command surface. `02-swarm-start-1` shows
both are used, for different things:

| Channel | Transport | Used for | Status |
|---|---|---|---|
| Interface 2 feature reports | `SET_REPORT` / `GET_REPORT` over the control endpoint | reading and presumably writing configuration, at startup | observed (`02-swarm-start-1`) |
| Interface 1 `0x0d` / `0x85` | interrupt | the `a1` state stream, plus `a3` frames | observed (`00-idle-1`, `02-swarm-start-1`) |

### Reading configuration: the selector register

At startup Swarm II reads a long list of configuration blocks. Each read is
three steps:

```
OUT 0x00   05 00 <selector> 00        write report 0x05, the selector register
IN  0x80   05 01 00 00                device reports ready
IN  0x80   <report id> <len> <data>   the selected block
```

Report `0x05` is a 3-byte feature report, which matches the 4 bytes on the wire
including the report ID. The reply block's own report ID and length match the
report map exactly — **all nineteen observed reads, without exception**, which is
what ties the report map to real behaviour.

| Selector | Report | Payload | Selector − report |
|---|---|---|---|
| `a0` | `0x20` | 91 B | +128 |
| `a1` | `0x21` | 347 B | +128 |
| `a2` | `0x22` | 347 B | +128 |
| `a3` | `0x23` | 347 B | +128 |
| `a5` | `0x26` | 175 B | +127 |
| `a6` | `0x27` | 175 B | +127 |
| `a8` | `0x2d` | 15 B | +123 |
| `ab` | `0x2b` | 91 B | +128 |
| `ac` | `0x2c` | 91 B | +128 |
| `af` | `0x17` | 43 B | +152 |
| `b0` | `0x11` | 347 B | +159 |
| `b3` | `0x30` | 179 B | +131 |
| `b4` | `0x31` | 687 B | +131 |
| `b5` | `0x31` | 687 B | +132 |
| `b6` | `0x32` | 175 B | +132 |
| `b7` | `0x32` | 175 B | +133 |
| `b8` | `0x25` | 163 B | +147 |
| `b9` | `0x29` | 583 B | +144 |
| `ba` | `0x16` | 103 B | +164 |

| Reading | Status |
|---|---|
| The selector is a **lookup table, not a formula** — the difference ranges from +123 to +164 | observed (`02-swarm-start-1`) |
| `b4` and `b5` both return report `0x31`, `b6` and `b7` both return `0x32`: two selectors, one block shape. Likely separate slots of the same kind of block, for instance per profile or per layer | hypothesis |
| Each selector was read five times in this capture, except `ba` once | observed (`02-swarm-start-1`) |
| Report `0x16` (103 B, selector `ba`) begins with the ASCII text `DESKTOP`: the name of the active Swarm II profile, which the device stores itself. Not a host name, as first assumed | observed (`02-swarm-start-1`) |
| Report `0x0e` is written as `0e 05 00 00 00` and `0e 05 01 00 00`, a register taking 0 and 1 — looks like enabling something before the reads | hypothesis |

Writing has **not** been observed: every exchange above is a read. Nothing here
is enough to safely write configuration back.

### `a3` frames on interface 1: the PC pushes values, the device renders

Besides the `a1` state stream, `0x0d` carries 64-byte `a3` frames. Unlike the
`a1` stream these are sent **only when something changes** — `00-idle-1` contains
none at all.

**System volume frame**, verified by prediction in `09-volume-50-1`:

```
a3 00 01 00 00 0d <vol> 00 01 00 <vol> 00   + zero padding to 64 bytes
                   ^^^              ^^^
                   offset 6         offset 10
```

I dragged the Windows volume slider while capturing, aiming for 50 and
settling on 51. Nine frames came out, and offsets 6 and 10 tracked the slider
exactly:

| Frame | Byte | Decimal |
|---|---|---|
| 1 | `0x40` | 64 |
| 2 | `0x36` | 54 |
| 3 | `0x35` | 53 |
| 4, 5 | `0x32` | 50 |
| 6 | `0x33` | 51 |
| 7, 8 | `0x32` | 50 |
| 9 | `0x33` | **51** — the value left on screen |

| Finding | Status |
|---|---|
| `a3 00 01 00 00 0d` introduces a system-volume frame; the level sits at offsets 6 **and** 10 | **confirmed** (`09-volume-50-1`, replayed in `11-replay-volume-07-1`) |
| The level is a plain percentage, 0–100 decimal, not a 0–255 scale | **confirmed** (`09-volume-50-1`, replayed in `11-replay-volume-07-1`) |
| `a3` frames are change-driven, not periodic | observed (`00-idle-1` has none, `09-volume-50-1` has nine) |
| Why the level is repeated at two offsets — two channels, or current and target | unknown |

Bytes 4–5 distinguish frame types. Seen so far:

| Bytes 4–5 | Body | Reading | Status |
|---|---|---|---|
| `00 0d` | `<vol> 00 01 00 <vol> 00` | system volume | observed (`09-volume-50-1`) |
| `03 0e` | `00 64 64 00 64 64 00` | per-application volume. The volume page on the screen shows the system level plus two applications, both at 100%, which matches two pairs of `0x64` | hypothesis |
| `01 02 02` | zeros | unknown | unknown |
| `01 02 04` | `00 00 02 00 00 0c` | unknown | unknown |

**This answers open question 2.** Live PC data is pushed as **numbers**, not as
rendered images: the host sends a percentage and the device draws the volume page
itself. The `Turtle Beach Global System Media Control` process that runs
alongside Swarm II is what feeds these values. For anything dashboard-like this
is the good outcome — a value per metric rather than a framebuffer.

#### Replayed: the first command sent from this project

`11-replay-volume-07-1` records our own frame going out. One `a3` frame was
written to interface 1 with the level set to 7, a value the dial cannot produce
from an even starting point, while Windows itself stood at 98:

```
t+18.300   a3 00 01 00 00 0d 07 00 01 00 07 00   (+ zero padding to 64)
```

The screen showed **7%**. The device sent nothing back: no acknowledgement on
`0x85` beyond the ordinary `32` replies, and nothing on `0x8c`. A volume frame is
fire-and-forget.

**How to write it on Windows.** The frame goes to the HID collection of
interface 1, which Windows exposes at a path containing `mi_01` with usage page
`0xff00`. `HidP_GetCaps` reports an output report length of **65**: one report-ID
byte plus the 64-byte frame. Since the report descriptor declares no report ID,
that leading byte is `0x00`, and the 64 bytes on the wire are the frame itself.
A plain `WriteFile` of those 65 bytes is enough — no feature report, no control
transfer.

Windows' own enumeration independently confirms the report map derived from the
captures: every collection's report length matches, including `0x000c` at 4
bytes for the dial, `0xff01` at 8, `0xff02` at 12 and `0xff03` at 1044.

This is the first thing this project has sent to the hardware. It changes only
what the screen displays; it does not touch the host's actual volume, and the
next real volume change resyncs it.

### Device notifications on `0x8c`

Report `0x03`, the report the device sends unprompted, is not fixed after all.
Across captures:

```
01-plug-in-1   03 00 31 20 d1 da 00 00
01-plug-in-2   03 00 31 20 c9 da 00 00
02-swarm-start 03 00 31 20 fc 50 03 00
02-swarm-start 03 00 31 00 87 06 00 00
02-swarm-start 03 00 31 20 43 20 04 00
```

An earlier version of this section claimed byte 2 was always `0x31`. That is
**wrong**: the dial capture shows `0xcc` there. Every byte after the report ID
varies.

```
10-dial-volume-up-1   03 00 cc 01 00 00 00 00   (x56, one per dial click)
```

This is the channel the device uses to tell the host something happened. It
fires once per dial click, alongside the Consumer report described below, so it
is a general "something was operated" notification rather than anything
volume-specific. Worth watching during the touch scenarios.

**Caveat on all of the above**: `02-swarm-start` has only **one** take so far.
The capture guide asks for two, so the varying bytes have not been separated from
the stable ones the way they were for `01-plug-in`.

### RGB lighting: report `0x11`

Setting a static colour writes **report `0x11`** as a feature report over the
control endpoint — the same report the startup sequence *reads* with selector
`b0`. It is read/write, 347 bytes of payload, and the device acknowledges each
write with `05 01 00 00` on `0x80`.

Nothing lighting-related appears on interface 1: with a static colour the `a1`
stream stays unchanged, so lighting is configuration, not a per-frame stream.

**Frame layout**, from `03-rgb-static-ff0000-1`, a static 255, 51, 51:

| Offset | Bytes | Meaning | Status |
|---|---|---|---|
| 0 | `11` | report ID | observed |
| 1 | `5c` | constant across every `0x11` frame seen | observed |
| 2 | `01` | constant | observed |
| 3 | `00` / `04` | varies between the read and the write | unknown |
| 4–5 | `01 06` | constant | observed |
| 6 | `63` / `64` | 99 and 100 — reads like a brightness percentage | hypothesis |
| 7 | `0b` / `03` | differs between the written and the read frame — effect or mode | hypothesis |
| 8–9 | `01 00` | constant | observed |
| 10–345 | 336 bytes | the LED table, see below | observed |
| 346–347 | checksum | 16-bit sum, see below | **confirmed by arithmetic** |

**The LED table is planar in groups of sixteen.** The 336-byte data area is
`336 = 7 × 48`, and each 48-byte block holds sixteen reds, then sixteen greens,
then sixteen blues:

```
block n:  R[0..15]  G[0..15]  B[0..15]
7 blocks  ->  112 LEDs
```

The run lengths prove it. A single colour of (255, 51, 51) produces repeating
`ff` × 16 followed by `33` × 32 — the 1:2 ratio of one channel at 255 against
two at 51, with the boundary falling every 48 bytes. The plane order is
therefore **R, G, B**.

Individual bytes depart from the set colour (`0x80` and `0x00` appear in the red
plane, `0x00` in the others), which is why the table is per-LED rather than a
single colour triplet.

**Addressing one LED.** Setting a single key proved the scheme outright. In
`03-rgb-perkey-esc-1040c0-1` the keyboard stayed at 255, 51, 51 and **only
Escape** was set to 16, 64, 192 (`10 40 c0`). Between the first and the last frame
of that capture exactly **five** bytes differ:

```
offset  10   01 -> 10     block 0, R plane, index 0
offset  26   33 -> 40     block 0, G plane, index 0
offset  42   33 -> c0     block 0, B plane, index 0
offset 346   6a -> 13     checksum
offset 347   98 -> 99     checksum
```

Nothing else moved. That fixes the whole addressing scheme:

```python
def offsets(led):           # led = 0 .. 111
    block, i = divmod(led, 16)
    base = 10 + block * 48
    return base + i, base + 16 + i, base + 32 + i   # R, G, B
```

| Finding | Status |
|---|---|
| An LED's three bytes sit sixteen apart inside its own 48-byte block | **confirmed** (`03-rgb-perkey-esc-1040c0-1`) |
| The plane order is R, G, B | **confirmed** — a colour with three distinct channel values landed in that order |
| **Escape is LED index 0** | **confirmed** (`03-rgb-perkey-esc-1040c0-1`) |
| The checksum covers the colour bytes: changing three data bytes changed the trailer | **confirmed** |
| Which key each of the 112 slots drives | **confirmed** — see the LED map below |

While the colour picker is dragged, Swarm II writes the **whole table** each
time, and it sends partial colours on the way: of the seven frames in that
capture the first carries only the new red, the fourth red and green, and the
last all three channels. Writing the table is evidently cheap enough to do on
every mouse move. The checksum held for all seven.

**The checksum is a plain 16-bit sum.** Add bytes 0 through 345 — the report ID
included, the trailer excluded — keep the low 16 bits and store them
little-endian:

```python
csum = sum(frame[:346]) & 0xFFFF
frame[346] = csum & 0xFF
frame[347] = csum >> 8
```

Verified against all seven distinct `0x11` frames across `02-swarm-start-1` and
`03-rgb-static-ff0000-1`, reads and writes alike. No CRC is involved.

| Still unknown | Why it matters |
|---|---|
| Bytes 3, 6 and 7 of the header | brightness and effect are guesses; a brightness capture (`04-rgb-brightness`) would settle byte 6 |
| Whether a write needs the preceding read | Swarm II read `0x11` at startup before writing it |

### Touchscreen: button layout and pages
unknown

### Touchscreen: image upload
unknown. Pixel format, chunk size, addressing, checksum.

### The volume dial: a complete round trip

The keyboard has a volume dial above Escape. `10-dial-volume-up-1` captures 56
clicks of it, and shows the full path from a physical control to the value drawn
on the screen.

**One click, device → host, three reports within half a millisecond:**

```
+0.0 ms   ep 0x8c   01 40 00 00                 report 0x01, Consumer page: bit set
+0.2 ms   ep 0x8c   01 00 00 00                 report 0x01, all bits clear - released
+0.3 ms   ep 0x8c   03 00 cc 01 00 00 00 00     report 0x03, the notification
```

Report `0x01` is the 3-byte Consumer report from the report map (usage page
`0x000c`). The value is a **bitmap**, not a usage code: one bit goes up and comes
back down like an ordinary key press and release — **`0x40` turns the volume
up, `0x20` turns it down**. The notification comes *last*; an earlier version of
this section put it first, which was the parser's timestamp artefact.

**Then, about 10 ms later, host → device:**

```
ep 0x0d   a3 00 01 00 00 0d <level> 00 01 00 <level> 00
```

| Finding | Status |
|---|---|
| Each click produces exactly one report `0x03` and one press/release pair of report `0x01` — 56 clicks gave 56 and 112 | observed (`10-dial-volume-up-1`) |
| The dial is **relative**: it reports a step, nothing more. Windows decides the level and the host pushes the absolute value back for display | observed (`10-dial-volume-up-1`) |
| One click moves Windows volume by **2 percentage points**, and the level saturates at 100 | observed (`10-dial-volume-up-1`) |
| The host sends each level **twice**, roughly 50 ms apart | observed (`09-volume-50-1`, `10-dial-volume-up-1`) |
| Round-trip latency from the device's report to the host's first `a3` frame: median **10.5 ms**, at least 3.5 ms | observed (`10-dial-volume-up-1`) |
| Bit `0x40` of report `0x01` is volume up, bit `0x20` volume down: of 56 clicks, the level rose after 31 of the 42 `0x40` presses and fell after 10 of the 14 `0x20` presses, the rest falling at 100% or at a change of direction | observed (`10-dial-volume-up-1`) |
| What the other bits of report `0x01` are | unknown |

**What this means for the project.** The screen is a slave display for host-side
state: the keyboard does not know its own volume. Any replacement for Swarm II
therefore has to take over both halves — read the device's input reports *and*
push values back — or the screen will simply stop updating. It also means the
hardware does not need to be taught anything to show a value; it only needs to
be sent one.

### Touch input events: report `0x33`

`05-touch-tap-scrlk-1`: with Swarm II running and the screen on the `DESKTOP`
profile page, I tapped the on-screen **SCR LK** button twice, three seconds
apart. Each tap:

```
+0.0 ms    ep 0x8c  33 01 04 03 00 47 01 01 00 00 00 00   report 0x33: touched
+0.6 ms    ep 0x8e  00 00 47 00 00 00 00 00                boot keyboard: usage 0x47 down
+1.0 ms    host     SET_REPORT LED 05 (tap 1) / 01 (tap 2)  Windows toggles the Scroll Lock LED
+58 ms     ep 0x8c  33 01 04 03 00 47 01 00 00 00 00 00   report 0x33: released
+60 ms     ep 0x8e  00 00 00 00 00 00 00 00                boot keyboard: all keys up
```

The second tap was the same apart from the LED value and a 94 ms hold.

| Finding | Status |
|---|---|
| Report `0x33` (12 bytes on the wire, `0xff02`) is the touch event for a screen button | observed (`05-touch-tap-scrlk-1`) |
| Byte 7 is the touch state: `01` touched, `00` released | observed |
| Byte 5 is the button's assigned key, as a HID keyboard usage: `0x47` is Scroll Lock | observed |
| **The keyboard sends the keystroke itself**, on the boot-keyboard interface, within a millisecond of the touch. Windows sees an ordinary Scroll Lock key press; Swarm II does not translate the touch | observed |
| Bytes 1–4 and 6 (`01 04 03 00`, `01`) | unknown — see below |

On this page the buttons sit in a 3-column grid with SCR LK in the second row,
second column: index 4 counting row by row from 0. Byte 2 is `04`, so it could
be the button's position on the page, with byte 1 the page and byte 3 the action
type. That is a **hypothesis** from a single button; a tap on a different button
will confirm or refute it.

Note for capture hygiene: a screen button bound to a key produces a real
keystroke on `0x8e`, which `kb7cap` always hides. Report `0x33` carries the key
code as well, but it is on a vendor page, so it is shown whenever the capture
contains the report descriptor — and hidden, like everything on a HID interface,
when it does not.

### LED map

Mapped by writing report `0x11` from our own code with Swarm II fully closed, so
that the keyboard shows the stored table, and photographing the result. Each
pass lit sixteen consecutive indices in a repeating red, green, blue, white
cycle, which pins every lit key to its index modulo four; the order along the
row fixes the rest. Ambiguous slots were then resolved by lighting only the
candidates, each in its own colour.

| Index | LED | Index | LED |
|---|---|---|---|
| 0 | Esc | 56 | Left Shift |
| 1–12 | F1–F12 | 57 | — (ISO `<>` position) |
| 13 | — | 58–64 | Z X C V B N M |
| 14 | `` ` `` | 65–67 | `,` `.` `/` |
| 15–24 | 1 2 3 4 5 6 7 8 9 0 | 68 | — |
| 25–27 | `-` `=` Backspace | 69 | Right Shift |
| 28 | Tab | 70 | Up arrow |
| 29–38 | Q W E R T Y U I O P | 71–73 | Left Ctrl, Windows, Left Alt |
| 39–41 | `[` `]` `\` | 74 | — |
| 42 | Caps Lock | 75 | Space |
| 43–51 | A S D F G H J K L | 76–77 | — |
| 52–53 | `;` `'` | 78–79 | Right Alt, Fn |
| 54 | — (ISO `#` position) | 80–81 | menu key, Right Ctrl |
| 55 | Enter | 82–84 | Left, Down, Right arrows |
| | | 85–86 | media previous, next |
| | | 87–90 | the four triangular top buttons, left to right |
| | | 91–95 | — |
| | | 96–105 | wrist rest light strip, ten segments left to right |
| | | 106–111 | — |

94 of the 112 slots drive something.

| Finding | Status |
|---|---|
| The main block is laid out as rows of fourteen: 0–13, 14–27, 28–41, 42–55, 56–69, with the bottom row from 70 | **confirmed** |
| The matrix is wired for an **ISO** layout: slots 54 and 57 sit exactly where the ISO-only `#` and `<>` keys would be, and are empty on this ANSI unit | **confirmed** for the empty slots; an ISO unit lighting them is a hypothesis |
| The up arrow sits at 70, at the start of the bottom row, apart from the other three arrows (82–84) | **confirmed** |
| The volume dial and its play/pause button have no slot in this table | observed — they never lit in any pass |
| The wrist rest strip is ten LEDs, 96–105, driven from the same table | **confirmed** |

**Report `0x11` is what the keyboard shows on its own.** With Swarm II and its
device service closed, nothing streams `a1` frames, and a written `0x11` table
appears immediately and stays. Combined with the flicker seen while Swarm II
runs, the model is: `0x11` holds the keyboard's own lighting, and the `a1`
stream is a live overlay that wins for as long as somebody keeps sending it.

### Profiles
Report `0x16` holds the name of the active Swarm II profile — `DESKTOP` on
this keyboard — read at startup with selector `ba`. How profiles are switched
is still unknown; scenario 08 should show it.

## Do-not-send list

Commands identified as firmware update, bootloader entry or factory reset.
These are never replayed and never implemented in the library.

_None identified yet._

## Open questions

1. Does the screen accept raw frames from the PC, or does it render from assets
   stored on the device? (captures 06, 07) Two pieces of evidence for onboard
   rendering: the keyboard draws a boot screen with its firmware version before
   any PC software is involved, and the volume page is drawn from a numeric
   percentage rather than a pushed image. Whether raw frames are *also* accepted
   is what scenario 06 has to settle.
2. ~~Are live PC stats pushed as rendered images, as numbers, or not at all?~~
   **Answered for volume**: as numbers. An `a3` frame carries a plain percentage
   and the device renders the page itself (`09-volume-50-1`). Whether the
   PC-performance view works the same way is still open — scenario 07.
3. Is there a checksum or sequence counter in write commands?
4. Which interface carries keystrokes besides the boot keyboard (NKRO)?
   Interface 2 exposes a keyboard collection next to its vendor-defined ones, so
   it is the first candidate. Its Consumer report `0x01` and notification report
   `0x03` have now both been seen carrying real input from the volume dial, which
   makes interface 2 the input channel — but no actual keystroke has been
   observed on it yet.
5. The device presents three OUT endpoints (`0x06`, `0x0d`, `0x0f`). Swarm II
   uses `0x0d` for its state stream; `0x06` and `0x0f` have not been seen in use.
6. **Does connecting over USB-C change anything?** Every capture so far was made
   over the supplied USB-A connectors, where the device reports `bcdUSB 0x0200`.
   Over the keyboard's single USB-C port the speed, the product ID or the
   interface layout could differ, which would mean the notes here apply to one
   cabling only. Worth a `01-plug-in` pair over USB-C to compare.
7. What are the 360 bytes of the interface 1 state stream, and what do the seven
   `0x7f`/`0x00` pairs sixteen bytes apart represent?
