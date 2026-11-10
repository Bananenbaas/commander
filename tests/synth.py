# SPDX-License-Identifier: GPL-2.0-or-later
"""Build synthetic USBPcap / usbmon captures for tests.

The fake device uses VID:PID 1234:5678 on purpose: tests must not suggest
anything about the real KB7 protocol.
"""

from __future__ import annotations

import struct

VID, PID = 0x1234, 0x5678

DEVICE_DESC = struct.pack("<BBHBBBBHHHBBBB", 18, 1, 0x0200, 0, 0, 0, 64, VID, PID, 0x0100, 1, 2, 0, 1)

# Configuration: iface 0 = HID boot keyboard (ep 0x81 IN),
# iface 1 = HID vendor (ep 0x82 IN, ep 0x03 OUT).
_IFACE0 = bytes([9, 4, 0, 0, 1, 0x03, 0x01, 0x01, 0]) + bytes([7, 5, 0x81, 0x03, 8, 0, 1])
_IFACE1 = (bytes([9, 4, 1, 0, 2, 0x03, 0x00, 0x00, 0])
           + bytes([7, 5, 0x82, 0x03, 64, 0, 1]) + bytes([7, 5, 0x03, 0x03, 64, 0, 1]))
_CFG_BODY = _IFACE0 + _IFACE1
CONFIG_DESC = struct.pack("<BBHBBBBB", 9, 2, 9 + len(_CFG_BODY), 2, 1, 0, 0xA0, 50) + _CFG_BODY

# Vendor page 0xFF00: report 0x05 = 64-byte feature, report 0x06 = 32-byte output.
HID_REPORT_DESC = bytes([
    0x06, 0x00, 0xFF,  # usage page 0xFF00
    0x09, 0x01,        # usage 1
    0xA1, 0x01,        # collection application
    0x85, 0x05,        # report id 5
    0x75, 0x08,        # report size 8
    0x95, 0x40,        # report count 64
    0x09, 0x02,
    0xB1, 0x02,        # feature
    0x85, 0x06,        # report id 6
    0x95, 0x20,        # report count 32
    0x09, 0x03,
    0x91, 0x02,        # output
    0xC0,              # end collection
])


def string_desc(text: str) -> bytes:
    raw = text.encode("utf-16-le")
    return bytes([2 + len(raw), 3]) + raw


def setup_bytes(bm: int, req: int, value: int, index: int, length: int) -> bytes:
    return struct.pack("<BBHHH", bm, req, value, index, length)


class UsbPcapBuilder:
    """Emits USBPcap (linktype 249) packets for one device."""

    def __init__(self, bus: int = 1, device: int = 5):
        self.bus, self.device = bus, device
        self.packets: list[tuple[float, bytes]] = []
        self.irp = 0x1000
        self.t = 0.0

    def _pkt(self, irp: int, info: int, ep: int, xfer: int, payload: bytes, stage: int | None = None,
             status: int = 0) -> None:
        hlen = 28 if stage is not None else 27
        hdr = struct.pack("<HQIHBHHBBI", hlen, irp, status, 0, info, self.bus, self.device, ep, xfer, len(payload))
        if stage is not None:
            hdr += bytes([stage])
        self.t += 0.001
        self.packets.append((self.t, hdr + payload))

    def _next_irp(self) -> int:
        self.irp += 8
        return self.irp

    def control_in(self, setup: bytes, data: bytes) -> None:
        irp = self._next_irp()
        self._pkt(irp, 0, 0x80, 2, setup, stage=0)
        self._pkt(irp, 1, 0x80, 2, data, stage=1)
        self._pkt(irp, 1, 0x80, 2, b"", stage=2)

    def control_out(self, setup: bytes, data: bytes, data_in_setup_packet: bool) -> None:
        irp = self._next_irp()
        if data_in_setup_packet:
            self._pkt(irp, 0, 0x00, 2, setup + data, stage=0)
        else:
            self._pkt(irp, 0, 0x00, 2, setup, stage=0)
            self._pkt(irp, 0, 0x00, 2, data, stage=1)
        self._pkt(irp, 1, 0x00, 2, b"", stage=2)

    def interrupt_out(self, ep: int, data: bytes) -> None:
        irp = self._next_irp()
        self._pkt(irp, 0, ep, 1, data)
        self._pkt(irp, 1, ep, 1, b"")

    def interrupt_in(self, ep: int, data: bytes) -> None:
        irp = self._next_irp()
        self._pkt(irp, 0, ep, 1, b"")
        self._pkt(irp, 1, ep, 1, data)

    def enumerate(self) -> None:
        self.control_in(setup_bytes(0x80, 6, 0x0100, 0, 18), DEVICE_DESC)
        self.control_in(setup_bytes(0x80, 6, 0x0200, 0, 9), CONFIG_DESC[:9])
        self.control_in(setup_bytes(0x80, 6, 0x0200, 0, len(CONFIG_DESC)), CONFIG_DESC)
        self.control_in(setup_bytes(0x80, 6, 0x0301, 0x0409, 255), string_desc("Example Corp"))
        self.control_in(setup_bytes(0x80, 6, 0x0302, 0x0409, 255), string_desc("Test Keyboard"))
        self.control_in(setup_bytes(0x81, 6, 0x2200, 1, len(HID_REPORT_DESC)), HID_REPORT_DESC)


def write_pcapng(path: str, linktype: int, packets: list[tuple[float, bytes]], nanosecond: bool = False) -> None:
    def block(btype: int, body: bytes) -> bytes:
        body += b"\x00" * ((4 - len(body) % 4) % 4)
        total = len(body) + 12
        return struct.pack("<II", btype, total) + body + struct.pack("<I", total)

    out = block(0x0A0D0D0A, struct.pack("<IHHq", 0x1A2B3C4D, 1, 0, -1))
    opts = b""
    if nanosecond:
        opts = struct.pack("<HH", 9, 1) + bytes([9]) + b"\x00\x00\x00" + struct.pack("<HH", 0, 0)
    out += block(1, struct.pack("<HHI", linktype, 0, 65535) + opts)
    divisor = 10 ** 9 if nanosecond else 10 ** 6
    for ts, data in packets:
        units = int(round(ts * divisor))
        out += block(6, struct.pack("<IIIII", 0, units >> 32, units & 0xFFFFFFFF, len(data), len(data)) + data)
    with open(path, "wb") as fh:
        fh.write(out)


def write_pcap(path: str, linktype: int, packets: list[tuple[float, bytes]]) -> None:
    out = struct.pack("<IHHiIII", 0xA1B2C3D4, 2, 4, 0, 0, 65535, linktype)
    for ts, data in packets:
        sec = int(ts)
        out += struct.pack("<IIII", sec, int(round((ts - sec) * 1e6)), len(data), len(data)) + data
    with open(path, "wb") as fh:
        fh.write(out)


def usbmon_packet(urb_id: int, ev: str, xfer: int, ep: int, dev: int, bus: int, ts: float,
                  setup: bytes | None, data: bytes, mmapped: bool = True) -> bytes:
    sec = int(ts)
    hdr = struct.pack("<QBBBBHbbqiiII8s", urb_id, ord(ev), xfer, ep, dev, bus,
                      0 if setup is not None else ord("-"), 0 if data else ord("<"),
                      sec, int((ts - sec) * 1e6), 0, len(data), len(data), setup or b"\x00" * 8)
    if mmapped:
        hdr += b"\x00" * 16
    return hdr + data
