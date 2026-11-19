# KB7 USB protocol

Working notes on the Turtle Beach Command Series KB7 USB protocol, derived from
captures (see `CAPTURE-GUIDE.md`). The protocol itself comes from observed
traffic only. Where the user-facing behaviour of a key or button is taken from
Turtle Beach's public support documentation, the section says so and cites it.

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

### Hints from the Swarm II installation

Not from captures, and not from file contents: these rest only on the folder
names, file names and file sizes of the Swarm II installation. No file is
opened, analysed or reproduced. Every row is a hypothesis about the hardware
unless a capture or the USB descriptors confirm it, and none of it is used to
derive protocol.

| Hint | Source | Status |
|---|---|---|
| Swarm II keeps the KB7 firmware in a folder named after the VID:PID and version, `Firmware\COMMAND_KB7\10F5_5038\139\` — the same `10f5:5038` and `0x0139` the device reports | folder name | consistent with `00-descriptors` |
| The main controller is probably a **Sonix** part: Swarm II ships a `Turtle Beach Sonix Updater` and a `SonixUSBClassSwitchTool` in `Thirdparty\Sonix\` | file and folder names | hypothesis |
| The main firmware image is about 23.6 MB, large for keyboard firmware alone, which fits the screen drawing its own pages from stored graphics | file size | hypothesis |
| A **second microcontroller** exists, with its own firmware file `MCU2\AP_AT423_V1.15.bin` (17 KB); the name suggests an Artery AT32F423. Which part of the keyboard it drives is unknown | file name and size | hypothesis |

These matter mainly for the do-not-send list: a firmware update exists as a
mechanism, with a dedicated updater, for two separate chips.

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
| What byte 4 means | unclear. With byte 5 as a little-endian pair it reads `0xdad1` / `0xdac9`, eight apart. The same event appears after profile switches and screen changes with bytes 4–6 different every time, but not as a steadily rising counter (see "Device notifications") | unknown |

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

**The values are the colours scaled by the current brightness.** At the 99 %
brightness these captures were made at, every value came out one lower than
configured:

| Configured | On the wire at 99 % |
|---|---|
| 255 | `0xfe` (254) |
| 192 | `0xbf` (191) |
| 64 | `0x3f` (63) |
| 51 | `0x32` (50) |
| 16 | `0x0f` (15) |
| 0 | `0x00` (0) |

An earlier version of this section read that as a fixed `floor(v x 255/256)`
encoding. `04-rgb-fn-keys-and-mic-1` shows it is brightness: raising the
brightness from 99 % to 100 % with Fn + Up moved the same red from 254 to 255,
and stepping down to 20 % took it through 232, 206, 180, 155, 129, 103, 77 and
52. Those steps are close to, but a point or two above, a straight
`v x percent / 100`; the exact curve is unknown.

That also explains the byte histogram of a static 255, 51, 51: 101 bytes of
`0xfe` are reds at full, 188 bytes of `0x32` are the greens and blues, 64 zeros
are LEDs that are off, and the seven `0x7f` are LEDs sitting at 128.

| Finding | Status |
|---|---|
| The `a1` stream carries the live 112-LED colour table, in the same layout as report `0x11` | **confirmed** (`03-rgb-perkey-esc-1040c0-1`) |
| The host applies the brightness itself, by scaling the colours it streams | observed (`04-rgb-fn-keys-and-mic-1`) |
| The exact brightness curve | unknown — nine points observed for a value of 255 |
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
| A frame streamed after a profile switch stays on the LEDs: switching from the profile `kb7screen run` streamed (SC) to one it does not (DESKTOP), the keyboard first showed DESKTOP's stored `0x11` lighting, then the one frame `run` sent after the switch replaced it and stayed (seen on WASD) | observed (`kb7screen run`, owner's observation, no capture) |
| The LEDs also keep the last frame when the stream stops without a switch (the streaming program ends or crashes) | hypothesis — expected from the row above, not observed |
| A set cut short after bank 1 left the LEDs frozen until the keyboard was unplugged; later sets were not acknowledged | observed (owner, `kb7screen run`, no capture); cause: hypothesis — the keyboard waits for banks 2–6 of the started set |
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
instead (write `05 00 b0 00` for profile 0, then read). Note also that a
status read of report `0x05` needs a 64-byte buffer; a 4-byte one fails with
error 31 until a longer read has succeeded once.

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
OUT 0x00   05 <profile> <selector> 00   write report 0x05, the selector register
IN  0x80   05 01 00 00                  device reports ready
IN  0x80   <report id> <len> <data>     the selected block
```

Byte 1 of the selector write is the profile, `00`–`04`; an earlier version of
this section read it as a constant `00`. Both startup captures
(`02-swarm-start-1`, `02-swarm-start-2`) write each selector once per profile,
as `05 00 <selector> 00`, `05 01 <selector> 00` … `05 04 <selector> 00`.

Report `0x05` is a 3-byte feature report, which matches the 4 bytes on the wire
including the report ID. The reply block's own report ID and length match the
report map exactly — **all nineteen observed reads, without exception**, which is
what ties the report map to real behaviour.

| Selector (byte 2) | Report | Payload | Selector − report |
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
| Each selector was read five times in this capture, once per profile, except `ba` once | observed (`02-swarm-start-1`) |
| Report `0x16` (103 B, selector `ba`) holds the names of all five profiles, see "Profiles". Not a host name, as first assumed | observed (`02-swarm-start-1`) |
| **Byte 1 of the selector write chooses the profile slot, 0–4, and the block names it again** (byte 3 of `0x29`, byte 2 of `0x25`). At startup every selector read five times is written with byte 1 `00`…`04`, and the five blocks returned count `00`…`04` in their profile byte. The lighting block `0x11` differs between slots; the screen block `0x29` is identical in all five apart from that byte | observed (`02-swarm-start-1`, `02-swarm-start-2`, reports `0x11`, `0x21`, `0x29`) |
| Reading one profile on its own: `05 01 b9 00` returns the `0x29` block of profile 1 (byte 3 `01`), and `05 01 b8 00` its `0x25` block (byte 2 `01`) | observed (`34-read-profile-1`) |
| Report `0x0e` is written as `0e 05 00 00 00` and `0e 05 01 00 00`, a register taking 0 and 1 — looks like enabling something before the reads | hypothesis |
| From a cold start (`02-swarm-start-2`, the keyboard silent since the PC booted), `0e 05 01 00 00` is Swarm II's **first write**, before any selector read, and the screen redraws (`31` event) two seconds later. Replayed on its own, it switches on the notifications described under "Without Swarm II" | **confirmed** (`19-replay-0e-1`) |

Writing configuration does not go through the selector: Swarm II writes a
block directly with `SET_REPORT`, with byte 3 set to the slot it is writing
(`00`, the active `DESKTOP` profile, in every write seen so far). See report
`0x11` for lighting and report `0x29` for the screen buttons.

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
| `00 0d` | `<vol> 00 <unmuted> 00 <vol> 00` | system volume. Byte 8 is `01` while sound is on and `00` while muted | observed (`09-volume-50-1`, `05-touch-tap-volumeicon-1`) |
| `03 0e` | `00 64 64 00 64 64 00` | per-application volume. The volume page on the screen shows the system level plus two applications, both at 100%, which matches two pairs of `0x64` | hypothesis |
| `06 0e` | `00 64 64 00 64 64 00` | same body as `03 0e`; sent next to it after every profile switch. Possibly the second application, with `03 0e` the first | hypothesis (`08-profile-switch-desktop-sc-1`) |
| `01 02 02` | `<muted>` then zeros | mute state: byte 6 is `01` while muted and `00` otherwise. Sent at startup with `00` | observed (`02-swarm-start-1`, `05-touch-tap-volumeicon-1`) |
| `01 02 04` | `00 00 02 00 00 0c` | unknown | unknown |
| `01 02 0b` | `00 00 <value> 00` | CPU temperature in °C for the widget at position 2: `10` = 16, drawn as "16 °C". Sent once in 3.5 minutes, so change-driven like the rest; with no host sending, the widget shows 0 °C after a redraw | observed (`15-screen-assign-app3-cputemp-1`, screen) |

**The volume mixer page** (`44-mixer-slider-1`): the screen listed the system
volume, then `Steam` and twice `Opera GX Internet Browser`, each at 100.
Dragging the `Steam` slider on the keyboard sent a stream of touch reports
`33 00 03 07 00 01 03 <level>`, one per step, with the level a percentage
(`62` = 98 down to `2a` = 42, the value the screen then showed). Swarm II sent
nothing back for it. Moving the Windows system volume from 99 to 75 and back
to 100 produced the system-volume `a3 … 00 0d` frames above, one per step.

| Finding | Status |
|---|---|
| A mixer slider moved on the keyboard reports `33 00 03 07 00 01 03 <level>`, level 0–100 | observed (`44-mixer-slider-1`) |
| Byte 3 `07` is the slider widget type; byte 2 `03` and byte 6 `03` name the slider (here the first application) | hypothesis — one slider moved |
| How the application names and their levels reach the screen | unknown — sent before the capture started |

Read with the last two rows, bytes 4–5 of the `01 …` frames look like
`<position> <kind>`: the speaker sits at position 2, which carries the mute
state (`02`) — and, after the CPU temperature widget was dropped there, the
temperature (`0b`). A hypothesis with two data points.

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
volume-specific.

Byte 2 selects the kind of event. Seen so far:

| Byte 2 | Body | Meaning | Status |
|---|---|---|---|
| `01` | `03 00 01 <slot> 00 00 00 00` | the active profile changed to `<slot>` | observed (`08-profile-switch-desktop-sc-1`) |
| `31` | `03 00 31 <view> <b4> <b5> <b6> 00` | the screen has drawn a new view and wants live values: after every profile switch, about 4.9 s after plug-in, and on entering and leaving the on-screen settings. Swarm II answers each one by re-sending the mute state and volume frames. `<view>` is `20` for a profile's main page, `04` for the settings view, and `01` for a single widget, with its position in byte 4, after a report `0x35` write | observed / hypothesis for the meaning |
| `cc` | `03 00 cc 01 00 00 00 00` | the volume dial was turned | observed (`10-dial-volume-up-1`) |
| `06` | `03 00 06 <down> 00 <usage> 00 06` | a screen button bound to a modifier on its own | observed (`15-screen-assign-break-1`) |
| `fc` | `03 00 fc <down> 00 00 00 00` | the Fn key pressed (`01`) or released (`00`) | observed (`04-rgb-fn-keys-and-mic-1`) |
| `02` | `03 00 02 <key> <down> 00 00 00` | a key pressed or released that the keyboard handles itself: Fn-layer keys and the action bar. `<key>` is an internal number, not the LED index or a HID usage: Up `3a`, Down `3b`, Microphone Off `57`, RGB on/off `58` | observed |
| `0c` | `03 00 0c <percent> 00 00 00 00` | the LED brightness changed, in steps of ten from 20 to 100 | observed |
| `11` | `03 00 11 <off> 00 00 00 00` | the RGB on/off button: `01` when it switched the LEDs off, `00` when back on | observed |
| `f1` | `03 00 f1 08 57 11 <down> 00` | sent alongside the Microphone Off button's own `02` event | observed; meaning unknown |
| `f1` | `03 00 f1 01 <id> <down> 00` | an application button touched (`01`) or released (`00`), alongside its `0x33`. `<id>` is 16-bit little-endian and follows the position, not the image slot: `5f 12` at position 3, `61 12` at 5, `62 12` at 6, i.e. `0x125c` + position | observed (`27-touch-app-swarm-1`, `28-touch-app-0e-1`, `32-convert-ins-slot3-1`) |
| `fa` | `03 00 fa …` | hundreds per second while Fn is held; bytes 3–5 change as other keys are pressed | observed; meaning unknown |

**What `04-rgb-fn-keys-and-mic-1` shows about who does what.** Swarm II was
running and sent nothing to the keyboard but its lighting stream throughout.

- **Brightness**: the keyboard computes the new level from Fn + Up/Down and
  reports it (`0c`); Swarm II applies it by scaling the colours it streams.
- **RGB on/off**: the keyboard switches its LEDs off and on by itself — the
  stream carried on unchanged throughout.
- **Microphone Off**: the keyboard only reports the press; the host mutes the
  microphone, and nothing comes back to the keyboard.
- **Fn + Left/Right** (previous/next effect) produced key events but no visible
  change and no stream change, with a Swarm II profile active. Whether they do
  something without Swarm II is open.

Bytes 4–6 of the `31` event vary every time, and they are not a simple counter:
across five profile switches they happened to rise (`11 1b 83` … `b9 30 87`),
but in `06-screen-background-ff0000-1` they went from `0c 1e 01` to `3a f4 00`.
Their meaning is unknown, and so is that of the "noise" byte in the plug-in
section, which belongs to the same event.

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
| 6 | `63` / `64` | brightness in percent: Swarm II wrote 99 while the brightness was at 99 %, which the Fn-key capture confirms independently through the stream values | observed, by agreement with `04-rgb-fn-keys-and-mic-1` |
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

### Touchscreen: button layout and pages — report `0x29`

`15-screen-assign-break-1`: in Swarm II I changed one screen button, BREAK, to
the Left Alt key. Swarm II wrote two feature reports and nothing else:

```
SET_REPORT 0x29   584 bytes   29 48 02 00 …                 the page's button table
SET_REPORT 0x35    12 bytes   35 0c 01 05 01 00 05 00 01 00 4e 00
```

Report `0x29` is the button table of the screen page, in the same form Swarm II
reads at startup with selector `b9`. Compared with that read, only the BREAK
entry, the slot byte and the checksum changed. Each button is a 16-byte entry,
**position *p* at offset 7 + 16p**:

| Bytes | Meaning | Example |
|---|---|---|
| 0 | modifier, as a HID keyboard usage | `e2` Left Alt; `00` for a plain key |
| 1 | key, as a HID keyboard usage | `46` Print Screen, `47` Scroll Lock, `48` Pause |
| 2 | kind of binding: `0c` a key, `06` a modifier on its own, `07` a macro, `0b` an application | |
| 3 | `00` for keys, modifiers and macros; for an application, its **image slot** | |
| 4–15 | label, ASCII, NUL-terminated. Drawn only for macro and application buttons, see below | `PRINT`, `SCR LK` |

Two more kinds, set from Swarm II:

```
macro        01 00 07 00  "<10 chars>"   position 3  (15-screen-assign-macro-1)
application  00 01 0b 01  "app1"         position 3  (15-screen-assign-app-1)
application  00 01 0b 02  "app2"         position 4  (15-screen-assign-app-2)
application  00 01 0b 03  "app3"         position 5  (15-screen-assign-app3-cputemp-1)
```

For a macro, byte 0 is `01`, meaning unknown. For an application, byte 1 is
`01` every time (meaning unknown) and byte 3 is the image slot, numbered as the
upload opener `a5 41`, `42`, `43`. The macro's name was eleven characters long in
Swarm II, which wrote only the first ten; the keyboard draws eleven when
given eleven, so the cut is Swarm II's.

The `DESKTOP` page as stored:

```
pos  0-2   all zero — the three icon buttons; their actions are not in this table
pos  3     00 46 0c 00  PRINT
pos  4     00 47 0c 00  SCR LK
pos  5     00 48 0c 00  BREAK   ->   e2 00 06 00  ALT
pos  6-11  INS, HOME, PGUP, DEL, END, PGDOWN  (usages 49-4e)
```

Swarm II overwrote the label without clearing it: the new entry reads `ALT\0K`,
the `K` left over from `BREAK`. Harmless, since the label is NUL-terminated.

| Finding | Status |
|---|---|
| Report `0x29` holds the screen page's button bindings and labels, 16 bytes per position | observed (`02-swarm-start-1`, `15-screen-assign-break-1`) |
| Header: `29 48 02 <slot>`; offsets 199–581 hold 30 further non-zero bytes of unknown meaning | observed / unknown |
| The trailer is the same 16-bit sum as report `0x11`: `sum(bytes[0:582]) & 0xffff`, little-endian at 582–583 | **confirmed by arithmetic** on six frames |
| Report `0x35` sets the **widget type of one position**, see below | observed (`15-screen-assign-break-1`, `15-screen-remove-print-1`) |
| Where the three icon buttons (positions 0–2) get their actions | unknown |

**Report `0x35` decides whether a position is drawn at all.**
`15-screen-remove-print-1`: in Swarm II I deleted the PRINT button from the
`DESKTOP` page, and after a pause put it back. Each time Swarm II wrote report
`0x29` and then report `0x35`. Both `0x29` writes were byte for byte the table
already stored, label `PRINT` included; only `0x35` changed:

```
                   pos     type       value   sum
BREAK -> ALT   35 0c 01 05 01 00 05 00 01 00 4e 00   (15-screen-assign-break-1)
PRINT deleted  35 0c 01 03 01 00 00 00 00 00 46 00
PRINT restored 35 0c 01 03 01 00 03 00 0b 00 54 00
PRINT -> macro 35 0c 01 03 01 00 05 00 02 00 4d 00   (15-screen-assign-macro-1)
```

The macro button is drawn as a large hexagonal "M" icon with the `0x29` label
under it. Setting an application did not write `0x35` at all; Swarm II wrote
report `0x25` instead, the same information for the whole page (below).
Dropping the CPU temperature widget on the speaker button
(`15-screen-assign-app3-cputemp-1`) wrote:

```
CPU temp       35 0c 01 02 01 00 39 00 00 00 7e 00   position 2, type 39
```

so the three icon buttons of the top row take a type like any other position.

**Report `0x25` is the page's whole type table.** 164 bytes: a 10-byte header
`25 a4 00 00 00 00 01 00 00 00`, then four bytes per position from offset 10,
`<type> 00 <value> 00` — bytes 6 and 8 of a `0x35` frame — and the same 16-bit
sum at bytes 162–163. As stored after three applications had been set:

```
pos  0-2    3b 00 00 00 | 3e 00 00 00 | 48 00 00 00   lock, snipping, speaker
pos  3-5    05 00 06 00  x3                           applications
pos  6-11   03 00 0e 00 … 03 00 13 00                 INS … PGDOWN, keys
pos 12-37   further entries, meaning unknown
```

| Type | Value | Widget | Seen in |
|---|---|---|---|
| `00` | `00` | empty slot | `0x35` |
| `03` | key number (`0b` PRINT, `0c` SCR LK, `0d` BREAK, …) | a key | `0x35`, `0x25` |
| `05` | `01` | a modifier on its own | `0x35` |
| `05` | `02` | a macro | `0x35` |
| `05` | `06` | an application | `0x25`; from our own code written as `0x35` and as `0x25` |
| `39` | `00` | CPU temperature | `0x35` |
| `3b`, `3e`, `48` | `00` | the top row's lock, snipping and speaker buttons; `48` is also byte 3 of the speaker's touch report | `0x25` |

The type is also byte 3 of the touch report `0x33`.

With the type set to `00` the keyboard draws **nothing** at that position: no
tile, no label, the background image shows through.

| Finding | Status |
|---|---|
| Byte 3 is the position, numbered as in touch report `0x33` (PRINT `03`, BREAK `05`) | observed |
| Byte 6 is the widget type, with the same values as byte 3 of touch report `0x33`: `03` a key, `05` a modifier on its own, and `00` an **empty slot** | observed |
| An empty slot is not drawn; the background shows through. The slot's label stays in `0x29` | observed (`15-screen-remove-print-1`, and on the screen) |
| Bytes 10–11 are the 16-bit sum of bytes 0–9, little-endian, as in `0x11` and `0x29` | confirmed by arithmetic on three frames |
| Byte 8 is `0b` for the restored PRINT key and `01` for Left Alt — not a HID usage. It is the value that goes with the type: a key's internal number, or the subtype of type `05` (see the type table under report `0x25`) | observed |
| Bytes 2 and 4 (`01` in every frame) | unknown — byte 2 may be the page |

So a custom layout can leave key positions empty and show its own artwork from
the background image in their place. Whether the three icon buttons
(positions 0–2) can be emptied the same way is untested.

**Replayed** (`22-replay-0x35-print-1`, no Swarm II component running, report
`0x0e` written first): the two PRINT frames above, sent on their own with
`HidD_SetFeature` and a 12-byte buffer.

```
35 0c 01 03 01 00 00 00 00 00 46 00   ->  03 00 31 01 03 00 00 00   PRINT disappears
35 0c 01 03 01 00 03 00 0b 00 54 00   ->  03 00 31 01 03 00 00 00   PRINT is back
tap on PRINT                          ->  33 01 03 03 00 46 01 01 …, and the keyboard sends Print Screen itself
```

| Finding | Status |
|---|---|
| Report `0x35` alone empties and restores a position; report `0x29` does not have to be written with it | **confirmed** (`22-replay-0x35-print-1`) |
| The keyboard answers each `0x35` write with `03 00 31 01 <position>`: a redraw of that one widget. `31` with view `01` is new; until now only `20` (main page) and `04` (settings) were seen | observed |
| The restored button takes its key from the stored `0x29` table: the tap reported key `0x46` and Windows received Print Screen | confirmed |
| **An empty slot does not report touch.** With PRINT emptied, several taps on that spot produced no report `0x33`, no notification and no key | observed (`24-touch-empty-slot-1`), but without a control: nothing in that capture shows the notifications were still on. `0x0e` had been written 13 minutes earlier with no unplug in between, so they probably were |
| **The label of a key widget is not taken from `0x29`.** Writing the stored `0x29` block back unchanged was accepted; writing it with only PRINT's label changed to `TEST` (checksum recomputed) was accepted too, but the screen kept showing `PRINT`, also after re-sending the `0x35` frame. Likewise the position bound to Left Alt still shows `BREAK`, though `0x29` holds `ALT`. The firmware draws its own label and graphic for the widget | observed (`23-replay-0x29-label-1`, screen) |

**Consequence for a custom screen.** Key, modifier and macro buttons are drawn
by the firmware in its own style, and an empty slot shows the background but
(probably) ignores touch. The way to a custom screen is the **application
button**: its picture is uploaded from the host, its label can be set or left
empty, and its touch is reported — see "Application buttons" below.

**A modifier button does not send a keyboard report.** Tapping the reassigned
button produced touch report `33 01 05 05 00 00 01 01` — byte 3 now `05` — and
nothing at all on the boot keyboard. The Alt arrives instead in the vendor
notification report:

```
03 00 06 01 00 e2 00 06    pressed  (06 = modifier binding, e2 = Left Alt)
03 00 06 00 00 e2 00 06    released
```

Windows ignores that report, so for such a button it is the host software that
has to produce the Alt key. A plain key is sent by the keyboard itself; a
modifier on its own is not. A replacement for Swarm II therefore has to read
report `0x03` as well as report `0x33`.

While the binding was being changed, the keyboard also sent a burst of 23
notifications of another kind within a tenth of a second. Their purpose is
unknown.

### Touchscreen: image upload — `a5` frames

Swarm II only lets a custom image file be used as the **background**, and the
keyboard holds **one** such image. (It also uploads a picture for every
application button, taken from the program's icon — see "Application
buttons".) Setting it is two steps:
the image is uploaded from Swarm II, and then chosen on the keyboard's own
screen.

`06-screen-background-ff0000-1` uploads the solid red test image
`docs/test-images/solid-ff0000.png`. The upload is a third opcode on interface
1, next to `a1` and `a3`, and the device answers every frame on `0x85`:

```
host -> device (0x0d)          device -> host (0x85)
a5 00                          a5 00            start of an image
a5 f1                          a5 f1            start of a 4096-byte block
a5 3e <62 bytes>   x66         ff ff ff ...     data; byte 1 is the payload length
a5 04 <4 bytes>                ff ff ff ...     the block's last 4 bytes
a5 f2                          a5 f2            end of the block
... next block ...
a5 ff                                           end of the image
```

| Finding | Status |
|---|---|
| **The screen is 480 x 800 pixels, portrait.** The first upload is exactly 768,000 bytes, i.e. 384,000 pixels at two bytes each | observed |
| **Pixels are RGB565, little-endian**: the red image arrives as `00 f8` throughout, i.e. `0xF800` | observed |
| Data moves in blocks of 4096 bytes (66 frames of 62 bytes plus one of 4); the final block is shorter (2048 bytes for the background) | observed |
| Byte 1 of a data frame is its payload length (`3e` = 62); `00`, `40`, `41`, `f1`, `f2` and `ff` are control values. The openers seen: `00` background, `40` its thumbnail, `41` an application button's picture | observed |
| The device acknowledges every data frame with a frame of `ff`, and the host waits for it: one data frame every 2.0 ms | observed |
| No checksum is visible anywhere in the upload | observed |
| **Pixel order is row by row from the top left**, 480 pixels per row, with nothing between rows: a 480 x 800 image of four solid quadrants (red top left, green top right, blue bottom left, white bottom right) arrives as 400 rows of 240 red + 240 green, then 400 rows of 240 blue + 240 white, and the screen shows it upright | observed (`06-screen-background-quadrants-1`) |

**Throughput is about 23 KiB/s**: the full-screen image took 32 seconds. Live
full-screen video over this channel is out of the question, which is consistent
with how the volume page works — the host sends values and the device draws.

**The framing in full** (`06-screen-background-quadrants-1`): a block holds up
to 4096 bytes, split into data frames of 62 bytes and one shorter frame for the
remainder (`3e` x 66 + `04` for a full block). Unused bytes of a frame are zero.
The background is 187 full blocks plus one of 2048 bytes; the thumbnail 15 full
blocks plus one of 1608. Control frames (`a5 00`, `a5 40`, `a5 f1`, `a5 f2`,
`a5 ff`) are echoed back unchanged, data frames are answered with 64 bytes of
`ff`. The device answers a data frame within about 2 ms and an `a5 f2` (end of
block) after about 34 ms.

#### Replayed: a background uploaded from our own code

`20-replay-background-stripes-1`, with no Swarm II component running: a test
pattern of eight horizontal colour bands (red, orange, yellow, green, cyan,
blue, purple, white) uploaded as background and thumbnail, framed exactly as
above. Before sending anything, the framing was checked by re-framing the
image from `06-screen-background-quadrants-1` and comparing: all 14,006 frames
were byte-identical to Swarm II's.

| Finding | Status |
|---|---|
| The upload works from our own code; every frame was acknowledged as expected, and the thumbnail appeared in the keyboard's background picker | **confirmed** |
| The bands appear on the screen in the order sent, top to bottom | confirmed |
| The upload alone does not change the screen; the new background has to be chosen on the keyboard, as with Swarm II | observed |
| Choosing it is handled entirely by the keyboard: the host only sees the `31` redraw events (`04` on entering the settings view, `20` on returning to the main page) | observed |
| **The speed is set by the device**: sending each frame as soon as the previous one is acknowledged gave 23.2 KiB/s, the same as Swarm II. A full screen takes 32 seconds whoever sends it | observed |
| The background survives unplugging the keyboard: it is stored on the device | observed (`21-replug-0e-persist-1`) |

**A second, smaller upload follows.** After the background, Swarm II sends
another image opened with `a5 40` instead of `a5 00`: 63,048 bytes, which as
RGB565 is 31,524 pixels. **It is the thumbnail the keyboard shows when the
background is chosen on its screen**, and it has its own fixed format:

| Finding | Status |
|---|---|
| The first 42,032 bytes are **142 x 148 pixels**, row by row from the top left | observed (`06-screen-background-quadrants-1`) |
| They hold the whole background scaled down, not cropped: each quadrant of the test image arrives as 71 x 74 pixels. The scale is 480/142 horizontally and 800/148 vertically, so the thumbnail is squashed to almost square, which is how it looks on the screen | observed |
| The remaining 21,016 bytes are all `ff` in both uploads so far | observed (`06-screen-background-ff0000-1`, `-quadrants-1`) |
| Those bytes are a one-byte-per-pixel alpha mask of the 142 x 148 image, here fully opaque. The application-button upload has the same layout, where the mask is confirmed | hypothesis for the thumbnail |

Swarm II offers no animated formats: its file picker allows only `.png`,
`.jpg` and `.jpeg`.

Choosing the image on the screen afterwards sends no image data. It produces
only notification events, after each of which Swarm II re-sends the live values.

### Touchscreen: application buttons — an image per button

`15-screen-assign-app-1`: in Swarm II I set the PRINT position of the `DESKTOP`
page to "start application", with a program of my choice. The keyboard then showed the
program's icon with the name under it, and **no tile**: the icon sits straight
on the background image. Swarm II wrote, in this order:

```
a5 41 ... a5 ff        image upload, opener 41, 63,048 bytes   (2.7 s)
SET_REPORT 0x29        position 3 -> 00 01 0b 01 "app1"
SET_REPORT 0x25        164 bytes, 200 ms after 0x29
```

No `0x35`. The keyboard answered with a `31` redraw about 2 s later.

**The upload is the button's picture, with an alpha mask.** The 63,048 bytes are
two parts:

| Bytes | Content |
|---|---|
| 0 – 42,031 | 142 x 148 pixels, RGB565 little-endian, row by row from the top left |
| 42,032 – 63,047 | 142 x 148 alpha values, one byte per pixel: `00` transparent, `ff` opaque |

In Swarm II's upload the alpha is above zero on exactly the 5,135 pixels that
form the icon, and zero on the 15,873 black pixels around it; 8 dark pixels on
the icon's edge also have alpha. Many alpha values lie between `00` and `ff`,
which smooths the icon's edge.

**Touch.** Tapping the button sends a touch report and a notification, and
nothing else; with Swarm II running, Swarm II then starts the program on the
PC and sends nothing back to the keyboard:

```
33 01 03 05 00 00 01 01    report 0x33: position 03, kind 05, touched
03 00 f1 01 5f 12 01 00    notification f1, touched
33 01 03 05 00 00 01 00    released
03 00 f1 01 5f 12 00 00    released
```

The keyboard starts nothing itself: with the notifications off, a tap sends
nothing at all (`26-touch-app-noswarm-1`).

#### Replayed: our own picture on a button

With no Swarm II component running except its session monitor, which sent
nothing in any capture. Before sending, the framing was checked by re-framing
Swarm II's own pixels: all 1065 frames were byte-identical.

| Finding | Status |
|---|---|
| Report `0x0e` from our own code is enough: with it written, every tap gave the `0x33` and `f1` events above, and the dial its `cc` events as a control | **confirmed** (`28-touch-app-0e-1`) |
| An `a5 41` upload of our own picture, followed by the `0x29` block Swarm II wrote, is stored and drawn on the button: a test picture with an opaque border in four colours, an opaque black square and a white square with alpha `ff` on its left half and `80` on its right half appeared exactly so, with the background showing everywhere else | **confirmed** (`30-replay-app-icon-alpha-1`, screen) |
| Black is not transparent by itself; only the alpha decides | confirmed |
| The alpha mask is honoured per pixel: a first test picture had magenta (`1f f8`) in the mask, and showed vertical stripes of alternately 12% and 97% opacity | observed (`29-replay-app-icon-2`, screen) |
| The new picture appears at the button's next redraw: after a tap, also with the notifications off, or after unplugging | observed |
| The picture survives unplugging | observed |
| The label is drawn from `0x29` in a thin font, inside the 142 x 148 area near its bottom edge. Eleven characters (`ABCDEFGHIJK`) were drawn in full | **confirmed** (`31-replay-app-label-1`, screen) |
| **An empty label (twelve `00` bytes) draws no text at all**, leaving only the uploaded picture | **confirmed** (`31-replay-app-label-1`, screen) |
| Report `0x25` was not needed for any of the above | observed |

So an application button is fully the host's: its own pixels with transparency,
its own label or none, no firmware graphics, and its touch reported to the host,
which decides what happens.

Three things went wrong on the way, and each is a rule now:

- **After writing `0x0e`, wait for its redraw.** The keyboard redraws the
  screen for about 2 s and does not answer on interface 1 meanwhile: an
  `a5 41` sent 3 ms after `0x0e` was echoed only after 2 s
  (`25-replay-app-icon-1`).
- **Never leave an upload open.** In that attempt the sender gave up after 1 s,
  so the keyboard was left waiting inside an upload and stopped working as a
  keyboard until it was unplugged. Opening the upload without sending data did
  not clear the stored picture.
- **Wait 200 ms between `0x29` and `0x25`**, as Swarm II does. Sent 1 ms after
  `0x29`, the `0x25` control transfer was not accepted and Windows cancelled it
  after 5 s; a retry was stalled. The keyboard kept working
  (`29-replay-app-icon-2`).

| Open | Status |
|---|---|
| How many image slots exist | open: 1–9 seen, see below |
| The `0x25` header and its entries past position 11 | unknown |
| Byte 1 of an application's `0x29` entry (`01`), and the `01` before the id in `f1` | unknown |

#### More buttons, and one made from our own code

Two more applications from Swarm II (`15-screen-assign-app-2`,
`15-screen-assign-app3-cputemp-1`) followed the same pattern: upload opener
`a5 42`, then `a5 43`, with the matching slot in byte 3 of the `0x29` entry and
type `05` / `06` in `0x25`. Starting Swarm II did not re-upload any picture;
our picture in slot 1 stayed. Its next `0x29` write did put its own label,
`app1`, back.

Then, with Swarm II closed (`32-convert-ins-slot3-1`), INS was turned into an
application button from our own code, sharing slot 3 with the `app3` button:

```
a5 43 ... a5 ff                        new picture in slot 3
SET_REPORT 0x29                        position 6 -> 00 01 0b 03 "slot3"
(200 ms)
SET_REPORT 0x35                        35 0c 01 06 01 00 05 00 06 00 54 00
                                       -> 03 00 31 01 06   redraw of position 6
SET_REPORT 0x0e                        notifications on
```

| Finding | Status |
|---|---|
| `0x29` plus `0x35` turned a key position of the active profile into an application button; `0x25` was not needed there. With nothing sent afterwards this works, but `0x35` is queued and names no profile, so it is not a safe recipe: see "Type writes are queued" below | observed (`32-convert-ins-slot3-1`, screen) |
| INS showed the new picture and its label at once; the `app3` button showed the same picture after the next full redraw: **positions can share a slot** | confirmed (screen) |
| Taps on both are reported: `33 01 06 05 …` with `f1 … 62 12` for INS, `33 01 05 05 …` with `f1 … 61 12` for `app3` | **confirmed** |

The recipe for buttons of our own, every step measured:

1. Write `0x0e` once after plugging in and wait for its `03 00 31 20` redraw;
   taps then arrive as `0x33` with the position, and the host decides what
   they do.
2. Switch to the profile with `0x06` and wait for `03 00 01 <profile>`. If
   the active profile is not known, switch to another profile first (see
   below).
3. Upload the picture: `a5 4<slot>`, 142 x 148 RGB565 plus the alpha mask.
4. `0x29`: the profile's table with the position's entry
   `00 01 0b <slot> <label, or twelve 00>`.
5. Wait 200 ms, then the profile's whole `0x25` table, read before and with
   the position's type set to `05` / `06`.
6. Wait for the keyboard's full redraw `03 00 31 20 …` before sending
   anything that could change the active profile.

#### Type writes are queued; a switch to the active profile is not confirmed

Measured with our own code (`37-kb7screen-apply-sc-1`,
`38-copy-factory-to-desktop-1`, `38-copy-factory-to-desktop-2`):

| Finding | Status |
|---|---|
| **Report `0x35` carries no profile and is queued by the keyboard.** Seven `0x35` frames sent within 2 ms were taken over one by one, from 80 ms to 2.7 s after sending, each answered by `03 00 31 01 <position>`. A `0x06` switch to another profile sent 10 ms after them made the queued type changes land on **that other profile**: the wrong profile was changed | observed (`37-kb7screen-apply-sc-1`) |
| **A switch (`0x06`) to the profile that is already active is not confirmed**: no `03 00 01 <profile>` came within 5 s. Switching to another profile first and then to the target gives two confirmations | observed (`38-copy-factory-to-desktop-1`, `-2`) |
| **A whole `0x25` table written from our own code is taken over** when sent at least 200 ms after the `0x29` write: profile 0's tables (`0x29`, then 200 ms, then `0x25`) were replaced, and the keyboard answered with one full redraw `03 00 31 20 …` about 0.2–2 s later | observed (`38-copy-factory-to-desktop-1`, `-2`) |

So a profile's types are written as its whole `0x25` table, and nothing that
may switch profile is sent before the `03 00 31 20` redraw that follows it.
`0x35` is not used to set up buttons. Whether `0x25` is queued the same way, or
written to the profile in its own byte 2, is not measured; waiting for the
redraw covers both.

#### Slots 4–9, the top row, and a second profile

`33-convert-home-slot4-1`, from our own code: HOME became an application button
with a new picture in slot 4 (`a5 44`), shown with its label, and its taps
reported as `33 01 07 05 …` with `f1 01 63 12 …` — the id `0x125c` + 7, as
predicted.

`15-screen-assign-apps-toprow-profile-1`, from Swarm II: applications on PGUP,
DEL, END and PGDN, one on the lock button at the top left, then a switch to
profile 1 (`SC`) and one application there.

```
PGUP   a5 44   00 01 0b 04 "app4"         03 00 12 00 -> 05 00 06 00
DEL    a5 45   00 01 0b 05 "app5"         03 00 11 00 -> 05 00 06 00
END    a5 46   00 01 0b 06 "app6"         03 00 10 00 -> 05 00 06 00
PGDN   a5 47   00 01 0b 07 "<10 chars>"   03 00 13 00 -> 05 00 06 00
lock   a5 48   00 01 0b 08 "app8"         3b 00 00 00 -> 05 00 06 00
SET_REPORT 0x06   06 04 01 05             -> 03 00 01 01, 03 00 31 20 …   profile 1
SCR LK a5 49   00 01 0b 09 "app9"         (tables of profile 1)
SET_REPORT 0x06   06 04 00 05             -> 03 00 01 00, 03 00 31 20 …   profile 0
```

| Finding | Status |
|---|---|
| Slots 1–9 exist (`a5 41`–`a5 49`); slots 1–4 have been written from our own code. The highest slot is unknown | observed |
| **Slots are numbered across profiles**: the first application on profile 1 got slot 9. Each profile has its own `0x29` and `0x25` tables, with the profile in byte 3 of the `0x29` header and byte 2 of the `0x25` header | observed |
| **The top row can hold application buttons**: the lock button (position 0) became one, showing its picture without a tile | observed (screen) |
| **The host can switch the active profile**: `SET_REPORT 0x06`, `06 04 <profile> 05`. The keyboard announces the switch as when the profile name is tapped, but not a switch to the profile that is already active (see "Type writes are queued" above) | observed |
| An application button with slot `00` is drawn with the firmware's default "Open App" icon inside a tile | observed (screen) |
| A picture whose alpha is zero everywhere shows only the label: Swarm II uploaded such a picture for `app4`, presumably failing to read that program's icon | observed (`a5 44` upload, screen) |
| Swarm II cuts labels to ten characters (`app7`'s longer name) | observed |

**Swarm II and our own code must not both manage the buttons.** Swarm II hands
out slots from its own count, so it gave slot 4 — already in use by our HOME
button — to `app4`. It also reads the button table at startup and, on writing
it back, turned the two positions it did not know (INS and HOME) into
application buttons with slot `00` and an empty label. Whoever writes last
wins.

#### Slots up to 15, and pages 2 and 3

`40-swarm-slots-pages-1`: with our own pictures in slots 1–8 (profile 1), Swarm
II was given seven application buttons on page 1 of profile 2 (`DEFAULT 03`),
then a website button on page 2 and a third-party application on page 3. Swarm
II refused an eighth application button with "You can have up to 15 Launch
Application widgets on the display".

- **Swarm II uploaded to slots 9 to 15** (`a5 49` … `a5 4f`) and left 1–8
  alone, so this time it counted the slots already named in the button tables
  rather than only its own.
- **Byte 2 of report `0x35` is the page.** `35 0c 02 04 01 00 05 00 05 00`
  put the website button (type `05`, value `05`) on page 2, position 4;
  `35 0c 03 04 01 00 42 00 00 00` put the third-party application (type `42`)
  on page 3, position 4.
- **Report `0x29` holds pages one after the other, twelve entries each.** The
  website button's entry `07 0b 00 "hohoho.nl"` (binding kind `07`) is entry
  16, i.e. page 2 position 4; page 2's numeric keypad keys fill entries 12–23.
  With 35 entries the table ends at page 3 position 10.
- **Report `0x25` gives every page a marker in front of its twelve types.**
  Page 1's marker is header byte 6 (`01`), page 2's is entry 12 (`02 00 00 00`)
  and page 3's entry 25 (`03 00 00 00`), so page *g* position *p* is entry
  `13 x (g - 1) + p`; 38 entries cover three pages exactly.

| Finding | Status |
|---|---|
| Image slots 9–15 exist and take uploads | observed (`40-swarm-slots-pages-1`) |
| 15 is the highest slot | hypothesis — Swarm II's own limit of 15 application widgets |
| Byte 2 of `0x35` is the page, 1–3 | observed (`40-swarm-slots-pages-1`) |
| `0x29` entry of page *g* position *p* is `12 x (g - 1) + p` | observed for page 2; page 3 from the table size |
| `0x25` entry of page *g* position *p* is `13 x (g - 1) + p`, with page markers at 12 and 25 | observed (`40-swarm-slots-pages-1`) |
| Page 3 position 11 has no `0x29` entry | hypothesis, from the table size |
| **Byte 1 of a `0x33` touch is the page**, 1–3: `33 02 06 03 00 59 01 01` is a tap on page 2 position 6 (a keypad key, `59`), `33 03 09 1e 00 00 01 01` one on page 3 position 9 | observed (`41-tap-page2-1`, `42-tap-page3-1`; page 1 is `01` in every earlier capture) |

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
| Bit `0x10` of report `0x01` is Mute: the on-screen speaker button sends it (`05-touch-tap-volumeicon-1`) | observed |
| The full bitmap, from the Consumer collection in interface 2's report descriptor: 18 one-bit fields in this order | read from the descriptor (`01-plug-in-1`) |
| Bits `0x01` Play/Pause, `0x04` Scan Previous and `0x08` Scan Next match the presses in `18-standalone-media-1` | observed |

The Consumer bitmap, bit by bit (byte 1, then byte 2, then the low two bits of
byte 3; usages on page `0x0c`):

| Bit | Usage | Function | Seen from |
|---|---|---|---|
| `01 00 00` | `0xcd` | Play/Pause | dial press, screen button |
| `02 00 00` | `0xb7` | Stop | — |
| `04 00 00` | `0xb6` | Scan Previous Track | button left of the dial, screen button |
| `08 00 00` | `0xb5` | Scan Next Track | button right of the dial, screen button |
| `10 00 00` | `0xe2` | Mute | screen speaker button |
| `20 00 00` | `0xea` | Volume Decrement | dial, screen button |
| `40 00 00` | `0xe9` | Volume Increment | dial, screen button |
| `80 00 00` | `0x183` | AL Consumer Control Configuration (media player) | — |
| `00 01 00` | `0x18a` | AL Email Reader | — |
| `00 02 00` | `0x192` | AL Calculator | — |
| `00 04 00` | `0x194` | AL Local Machine Browser | — |
| `00 08 00` | `0x221` | AC Search | — |
| `00 10 00` | `0x223` | AC Home | — |
| `00 20 00` | `0x224` | AC Back | — |
| `00 40 00` | `0x225` | AC Forward | — |
| `00 80 00` | `0x226` | AC Stop | — |
| `00 00 01` | `0x227` | AC Refresh | — |
| `00 00 02` | `0x22a` | AC Bookmarks | — |

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
| Byte 2 is the button's **position** on the page, counted row by row from 0 in the 3-column grid | observed — predicted before tapping BREAK (`05`) and the speaker button (`02`), and right both times |
| Byte 3 is the kind of action: `03` for a button bound to a keyboard key, `05` for a modifier on its own and for an application button, `48` for the speaker button, which mutes | observed; the full list of values is unknown |
| Bytes 1, 4 and 6 (`01`, `00`, `01` in every tap so far) | unknown — byte 1 may be the page, byte 4 the modifier keys of a shortcut |

Three buttons on the `DESKTOP` page, side by side:

```
SCR LK    33 01 04 03 00 47 01 01   key 0x47 Scroll Lock, sent on 0x8e
BREAK     33 01 05 03 00 48 01 01   key 0x48 Pause, sent on 0x8e
speaker   33 01 02 48 00 00 01 01   no key; Consumer report 0x01 bit 0x10 (Mute) instead
```

**The speaker button mutes, and the whole loop is visible.** The keyboard sends
the Mute media key itself (Consumer report `01 10 00 00`), Windows mutes, and the
host then pushes the new state back: the system-volume `a3` frame with its mute
byte set to `00`, followed by a mute-state frame `a3 00 01 01 02 02 01`. A second
tap reverses all three. That identifies two parts of the `a3` frames that were
unknown before — the mute byte and the mute-state frame.

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
| | | 85–86 | Previous Track, Next Track |
| | | 87–90 | the four triangular top buttons, left to right: Game Mode, Profile Cycle, Microphone Off, RGB on/off |
| | | 91–95 | — |
| | | 96–105 | wrist rest light strip, ten segments left to right |
| | | 106–111 | — |

94 of the 112 slots drive something.

| Finding | Status |
|---|---|
| The main block is laid out as rows of fourteen: 0–13, 14–27, 28–41, 42–55, 56–69, with the bottom row from 70 | **confirmed** |
| The matrix is wired for an **ISO** layout: slots 54 and 57 sit exactly where the ISO-only `#` and `<>` keys would be, and are empty on this ANSI unit | **confirmed** for the empty slots; an ISO unit lighting them is a hypothesis |
| The other empty main-block slots match the extra keys of the **Japanese** layout, which the KB7 is sold in: 68 where `ろ` sits between `/` and Right Shift, and 74, 76 and 77 where `無変換`, `変換` and `かな` flank the space bar. So the table is likely one superset of every layout variant | hypothesis — needs an ISO or JIS unit |
| The up arrow sits at 70, at the start of the bottom row, apart from the other three arrows (82–84) | **confirmed** |
| The volume dial and its play/pause button have no slot in this table | observed — they never lit in any pass |
| The wrist rest strip is ten LEDs, 96–105, driven from the same table | **confirmed** |

**Report `0x11` is what the keyboard shows on its own.** With Swarm II and its
device service closed, nothing streams `a1` frames, and a written `0x11` table
appears immediately and stays. Combined with the flicker seen while Swarm II
runs, the model is: `0x11` holds the keyboard's own lighting, and the `a1`
stream is a live overlay that wins for as long as somebody keeps sending it.

### Default functions (from vendor documentation)

Taken from Turtle Beach's support article *Command Series KB7 – Layout and
Secondary Functions* (support.roccat.com). This is what the keyboard does for
the user, not protocol; it is listed because it tells which functions the
keyboard handles on its own and which ones are worth capturing.

The KB7 is sold in American, German, French, Japanese, UK, Swiss and Nordic
layouts. Fn functions work without any software installed; Swarm II can
reassign some of them.

| Input | Function |
|---|---|
| Fn + F4 … F12 | Print Screen, Scroll Lock, Pause, Page Up, Page Down, Insert, Home, End, Delete |
| Fn + Left / Right | previous / next RGB effect |
| Fn + Down / Up | RGB brightness down / up |
| Fn + Right Alt | colour cycle |
| Fn + Menu | Copilot shortcut |
| Dial, clockwise / counter-clockwise / push | volume up / volume down / play-pause |
| Action bar, left to right | Previous Track, Next Track, Game Mode toggle, Profile Cycle, Microphone Off, RGB on/off |

Two of these connect to what the captures show:

- **Microphone Off works only while Swarm II is running**, according to the
  article. That fits the pattern seen with a screen button bound to a modifier:
  the keyboard reports the press, and the host software has to act on it.
- **The LED controls are onboard.** Brightness and effect can be changed without
  software, which makes them the natural way to pin down header bytes 6 and 7 of
  report `0x11`.

Swarm II lets keys be reassigned on three layers — Fn, Game Mode and Easy Shift
(Fn + Caps Lock) — according to the PC Games Hardware review of the KB7. There
are exactly three 347-byte blocks of unknown purpose, `0x21`, `0x22` and `0x23`,
which makes one key table per layer a reasonable **hypothesis**.

### Profiles

**The keyboard stores five profiles.** Configuration blocks carry the profile
slot (in byte 3; in byte 2 of `0x25`), and Swarm II reads every block once per
slot at startup, naming the slot in byte 1 of the selector write.

Report `0x16` (103 bytes, selector `ba`, read once) holds all five names, in
twenty-byte fields from offset 2:

| Slot | Name |
|---|---|
| 0 | `DESKTOP` |
| 1 | `SC` — the complete name, kept short to fit the profile label on the screen |
| 2 | `DEFAULT 03` |
| 3 | `DEFAULT 04` |
| 4 | `DEFAULT 05` |

Some fields contain bytes after the name's terminating NUL; whether that is
data or leftover content, as in the `ALT\0K` button label, is unknown.

| Finding | Status |
|---|---|
| Five profile slots, with their names in report `0x16` | observed (`02-swarm-start-1`) |
| The lighting differs per slot. The screen button tables were the same in all five at first; each profile has its own, and they differ once one is changed (see "Slots 4–9, the top row, and a second profile") | observed |
| The keyboard switches profiles **itself**: tapping the profile name on the screen steps through the five slots in order and wraps around | observed (`08-profile-switch-desktop-sc-1`) |
| **Renaming a profile is one write of the whole block**: `SET_REPORT` feature `0x16`, 104 bytes (`16 68`, then the five 20-byte name fields from offset 2, then a 16-bit little-endian sum of the first 102 bytes). Renaming `DEFAULT 03` to `TEST` and back changed only slot 2's field, which Swarm II wrote zero-filled after the name; the other fields went back with their bytes after the NUL unchanged. Nothing else was written for it; a `GET_REPORT` feature `0x05` (4 bytes) followed about 150 ms later | observed (`43-rename-profile-1`, `-2`) |
| Each switch is announced with notification `03 00 01 <slot>`, followed by the `03 00 31 20 …` "profile loaded" event | observed |
| Swarm II writes and reads nothing on a switch. It only follows: the `a1` lighting stream changes to the new profile's colours, and it re-sends the mute state, both application volumes and the system volume | observed |

So the slots are complete profiles held on the device, and the host's only job
on a switch is to resume streaming for the new one. **The host can switch too**:
choosing another profile in Swarm II wrote `06 04 <profile> 05` as report
`0x06`, and the keyboard announced the switch exactly as when the name is
tapped (`15-screen-assign-apps-toprow-profile-1`).

### Without Swarm II

Three captures after a cold start of the PC, with Swarm II, its device service
and its session monitor not running at all:

- `16-standalone-keys-1`: Fn + Down and Fn + Up, Fn + Right and Fn + Left, the
  RGB on/off button off and on, and five taps on the profile name to step
  through all five slots and back to `DESKTOP`.
- `17-standalone-dial-1`: the volume dial turned both ways, then the volume
  buttons on the screen's default page.
- `18-standalone-media-1`: the dial pressed twice, the buttons left and right
  of the dial twice each, then Play/Pause, Previous and Next on the screen
  twice each.

**The bus is silent until something is operated.** In a 5-second idle capture
the keyboard sent nothing at all, and the host sent nothing to it.

**What the keyboard sent:**

```
dial, one click            01 40 00 00  /  01 20 00 00, released within 1 ms
screen volume buttons      01 40 00 00  /  01 20 00 00, held 60-100 ms
dial press, screen play    01 01 00 00
left of dial, screen prev  01 04 00 00
right of dial, screen next 01 08 00 00
RGB on/off (by order)      03 00 02 83 01 ... / 03 00 02 83 00 ...
each profile switch        03 00 31 20 <b4> <b5> <b6> 0f
```

| Finding | Status |
|---|---|
| Without Swarm II the keyboard is a plain keyboard with media keys: the dial, the media buttons next to it and the screen's default media buttons all send ordinary Consumer reports, and Windows acts on them | observed (`17`, `18`) |
| The screen's default page draws its own media buttons and a banner asking to connect Swarm II for the full volume mixer, and it shows a volume level while the dial is turned | observed on the screen |
| No touch event (report `0x33`) is sent for the default page's buttons; a screen button produces only its Consumer report, held for about as long as the finger stays down | observed (`17`, `18`) |
| The dial sends no `cc` notification, Fn sends no `fc`, and Fn + Up/Down sends neither a key event nor the `0c` brightness level. The brightness still changes on the keyboard | observed (`16`) |
| The RGB on/off button is reported as key `83`, where it was `58` with Swarm II running, and the `11` "LEDs off/on" event is not sent. That this pair of events is the RGB button follows from the order of the actions only | observed (`16`) |
| Fn + Left/Right do nothing visible without Swarm II either | observed (`16`) |
| A profile switch changes the screen and the lighting to what the slot holds, and sends only the `31` event; the `01 <slot>` notification is missing | observed (`16`) |
| Bytes 4–6 of the `31` event rose across the five switches (`af c5 cb` … `26 29 ce`) and byte 7 was `0f` each time | observed; meaning still unknown |

So the extra notifications are not sent by default.

#### Replayed: report `0x0e` switches the notifications on

`19-replay-0e-1`, after a reboot of the PC with no Swarm II component running:

```
before the write   Fn pressed three times        nothing on the bus
11:22:16.452       SET_REPORT 0x0e  0e 05 01 00 00   (HidD_SetFeature, 5-byte buffer)
+1.9 s             03 00 31 20 …                  the screen redraws
then               Fn three times                 03 00 fc 01 / 03 00 fc 00, with the fa stream
                   dial one click                 01 40 00 00, 01 00 00 00, 03 00 cc 01
```

| Finding | Status |
|---|---|
| Writing `0e 05 01 00 00` turns on the extended notifications: Fn (`fc`, `fa`) and the dial (`cc`) report from then on, where the same actions before the write sent nothing | **confirmed** (`19-replay-0e-1`) |
| The screen redraws about two seconds after the write, as it does when Swarm II starts | observed |
| Nothing else is needed: no selector read, no stream, no other write | confirmed |
| `0e 05 00 00 00`, also seen from Swarm II, switches them off again: Fn pressed three times after it sent nothing, and `0e 05 01 00 00` turned them back on. The screen redraws after either write | **confirmed** (`19-replay-0e-off-1`) |
| The setting is **not kept** across a power cycle: after unplugging and reconnecting the keyboard it announced its boot (`31` event, about 5 s after enumeration) and Fn sent nothing. A host has to write it after every connection, as Swarm II does at startup | observed (`21-replug-0e-persist-1`) |

This is the first step of any replacement for Swarm II: one feature report, and
the keyboard reports everything Swarm II gets.

## Do-not-send list

Commands identified as firmware update, bootloader entry or factory reset.
These are never replayed and never implemented in the library.

_No command identified yet._

Known to exist, and never to be triggered from this project:

- **Firmware update of the main controller.** Swarm II carries the 1.39 image
  and a separate Sonix updater, plus a Sonix USB class-switch tool, which
  suggests the keyboard is switched into a different USB mode for flashing.
  Any report seen to do that goes here.
- **Firmware update of the second microcontroller** (`MCU2`, see "Hints from
  the Swarm II installation").

No capture so far has included an update, and none should be made with a real
update running: a failed or interrupted flash can leave the keyboard unusable.

## Open questions

1. Does the screen accept raw frames from the PC, or does it render from assets
   stored on the device? (captures 06, 07) Two pieces of evidence for onboard
   rendering: the keyboard draws a boot screen with its firmware version before
   any PC software is involved, and the volume page is drawn from a numeric
   percentage rather than a pushed image. **Largely answered by
   `06-screen-background-ff0000-1`**: the screen renders on its own, and raw
   pixels are accepted only as a stored background image, 480 x 800 in RGB565,
   at about 23 KiB/s. **Button pictures can be uploaded too**: an application
   button shows a 142 x 148 picture with an alpha mask, sent by the host, and
   our own picture is drawn the same way (`30-replay-app-icon-alpha-1`).
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
