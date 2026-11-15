# Capture guide (Phase 0)

The goal of Phase 0 is to collect USB traffic between Swarm II and the KB7. Each
capture records **exactly one deliberate action**, so that two captures differ in
one variable only. Everything in `docs/PROTOCOL.md` is derived from these files.

## Before you start: privacy and safety

- **A keyboard capture records your keystrokes.** Do not type anything
  sensitive (passwords, messages) while a capture is running. Ideally don't
  type at all and use the mouse for every step.
- Raw captures stay local. `captures/` is git-ignored; never attach a raw
  `.pcapng` to a public issue. `kb7cap` hides boot-keyboard reports by default,
  but the KB7 may also report keys through other interfaces, so don't rely on that.
- **Never install a firmware update while capturing**, and decline firmware
  update prompts during this phase. A firmware change can change the protocol,
  and a failed update is the main way to brick the keyboard.
- Write down the **Swarm II version** and **KB7 firmware version** (shown in
  Swarm II) in `captures/NOTES.md`. The protocol may differ per firmware.
- Also write down **how the keyboard is connected**: the supplied USB-A
  connectors or its single USB-C port, and which port of the machine. The device
  hangs behind its own hub and reports USB 2.0 over USB-A; a different cabling
  may well enumerate differently, so captures made over different connections
  are not directly comparable.

## Windows setup (Swarm II machine)

1. Install **Wireshark**. In the installer, tick **Install USBPcap**. Reboot.
2. Optional but very useful: install **USB Device Tree Viewer** (UsbTreeView),
   select the KB7 and its child devices, and save the report as text
   (`captures/00-usbtreeview.txt`). It lists VID/PID, interfaces, endpoints and
   (usually) the HID report descriptors.
3. Start Wireshark **as administrator**. You will see interfaces `USBPcap1`,
   `USBPcap2`, … (one per root hub). Click the gear icon next to each one until
   you find the hub the KB7 is attached to.
4. In that gear dialog:
   - select **only the KB7** (and its sub-devices), not the whole hub, so your
     mouse and other devices are not recorded;
   - enable **"Inject already connected devices descriptors into capture data"**.
     Without it, the capture has no VID/PID and no HID report descriptors.
   - raise the snapshot length to the maximum (65535) so image uploads are not
     truncated.
5. Save each capture as **pcapng**, named as described below.

## Capture discipline

For every capture:

1. Start the capture.
2. Wait about 3 seconds without doing anything.
3. Perform the action **once**.
4. Wait about 3 seconds.
5. Stop and save.

Record each scenario **twice**. Bytes that differ between two identical actions
are noise (counters, sequence numbers, checksums). Knowing that first makes the
real differences stand out.

Naming: `<NN>-<area>-<action>[-<variant>]-<take>.pcapng`, for example
`03-rgb-static-ff0000-1.pcapng`.

## First batch of scenarios

| # | File stem | What to do | Why |
|---|---|---|---|
| 00 | `00-idle` | Swarm II open, do nothing for 30 s | Periodic traffic (keep-alive, live stats pushed to the screen?) |
| 01 | `01-plug-in` | Swarm II **closed**; start capture, then plug the KB7 in | Full enumeration and descriptors |
| 02 | `02-swarm-start` | Start Swarm II with the KB7 connected | Handshake, device-info queries |
| 03 | `03-rgb-static-<rrggbb>` | Set the whole keyboard to one static colour: `ff0000`, `00ff00`, `0000ff` | Simplest write command; colour bytes are easy to spot |
| 04 | `04-rgb-brightness-<pct>` | Change only the brightness: `100`, `50` | Second variable in the same command family |
| 05 | `05-touch-tap-<button>` | Tap one touchscreen button (Swarm II open), then the same with Swarm II closed | How touch input reaches the PC |
| 06 | `06-screen-icon-<image>` | Assign one image from `docs/test-images/` to one touch button: `solid-ff0000`, `solid-0000ff`, `split-red-blue` | Upload mechanism and pixel format (solid colours make e.g. RGB565 `f800` obvious) |
| 07 | `07-screen-stats-<on/off>` | Turn the PC-performance view on or off | Does the PC stream frames, or does the device draw widgets? |
| 08 | `08-profile-switch` | Switch to another profile | Profile storage on the device |

The key open question is in 06 and 07: whether the screen accepts raw images
from the PC or renders from assets stored on the device. The answer decides how
live dashboards will work.

### A quick descriptor snapshot

`00-descriptors-<take>.pcap` is a shortcut, not a scenario: start a capture with
`--inject-descriptors` and stop it again a few seconds later, without touching
the device. USBPcap then writes the device and configuration descriptors of
everything on that root hub into the file, which is enough to fill in the device
identity and the interface/endpoint tables. From the command line:

```sh
"C:\Program Files\USBPcap\USBPcapCMD.exe" -d \\.\USBPcap<N> \
    -o captures/00-descriptors-1.pcap -s 65535 -A --inject-descriptors
```

It does **not** replace `01-plug-in`: injection supplies no HID report
descriptors and no string descriptors. Only a real enumeration has those.

### Elevation, and why Wireshark cannot do this

Two traps, both found the hard way:

- **Wireshark cannot capture USB here at all.** As of Wireshark 4.6, USBPcap is
  no longer integrated: `dumpcap -D` lists only network adapters even when run
  elevated, and `dumpcap -i \\.\USBPcapN` fails with
  `Error opening adapter ... (123)`. The Wireshark installer drops
  `USBPcapSetup-<version>.exe` into its own program directory for you to run
  separately, and `USBPcapCMD.exe` is then the only thing that captures.
- **`USBPcapCMD` relaunches itself elevated.** Started from an unelevated shell
  it spawns a second, elevated process. A UAC prompt therefore decides when the
  capture really begins, and the elevated process cannot be terminated from the
  unelevated side. Driving it from an unelevated script does not work: the
  capture either starts after the intended window has passed, or cannot be
  stopped.

Run it from an **already elevated** context instead. A wrapper that is itself
elevated can both start the capture and stop it again:

```bat
@echo off
set "UP=C:\Program Files\USBPcap\USBPcapCMD.exe"
start "" /b "%UP%" -d \\.\USBPcap3 -o captures\01-plug-in-1.pcap ^
    -s 65535 -b 134217728 -A --inject-descriptors
ping -n 181 127.0.0.1 > nul
taskkill /F /IM USBPcapCMD.exe
```

The capture window then starts when elevation is granted, not when the wrapper
is launched. While `USBPcapCMD` runs it holds the output file open exclusively,
so wait for it to exit before reading the capture.

Find the right control device with `USBPcapCMD.exe` and no arguments: it prints
the device tree per root hub, so you can see which `\\.\USBPcapN` the keyboard
hangs off. Note that the KB7 sits behind its own integrated hub, so both that hub
and the keyboard have to be in the capture.

## Linux alternative (descriptors and replay testing)

If you have a Linux machine, plugging the KB7 in there is the fastest way to get
the descriptors:

```sh
lsusb                                  # find VID:PID and bus
lsusb -v -d <vid>:<pid>                # interfaces and endpoints
sudo cat /sys/kernel/debug/hid/*/rdesc # HID report descriptors (needs debugfs)
```

Capturing on Linux: `sudo modprobe usbmon`, then capture on `usbmonN` (N = bus
number) in Wireshark. `kb7cap` reads usbmon captures too.

## Analysing a capture

```sh
python -m kb7tools summary captures/01-plug-in-1.pcapng
python -m kb7tools dump captures/03-rgb-static-ff0000-1.pcapng -d <vid>:<pid> --dir out
python -m kb7tools diff captures/03-rgb-static-ff0000-1.pcapng captures/03-rgb-static-0000ff-1.pcapng -d <vid>:<pid>
```

`summary` shows devices, interfaces, HID report IDs (vendor-defined pages are
the interesting ones) and traffic per endpoint. `diff` aligns both captures and
marks the bytes that changed with `^^`.
