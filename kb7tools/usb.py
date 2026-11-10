# SPDX-License-Identifier: GPL-2.0-or-later
"""Turn raw USB capture packets into paired transfers.

Supported link types:
  249  USBPcap (Windows)
  189  Linux usbmon, 48-byte header
  220  Linux usbmon, 64-byte memory-mapped header

A Transfer is one submission/completion pair: setup (control only), the
payload in the direction of the endpoint, and the completion status.
"""

from __future__ import annotations

import struct
from dataclasses import dataclass, field
from typing import Iterable, Iterator

from .pcap import RawPacket

LINKTYPE_USBPCAP = 249
LINKTYPE_USB_LINUX = 189
LINKTYPE_USB_LINUX_MMAPPED = 220

XFER_ISO = 0
XFER_INTERRUPT = 1
XFER_CONTROL = 2
XFER_BULK = 3
XFER_NAMES = {XFER_ISO: "iso", XFER_INTERRUPT: "intr", XFER_CONTROL: "ctrl", XFER_BULK: "bulk"}

USBPCAP_INFO_PDO_TO_FDO = 0x01  # set on completions travelling back up the stack
USBPCAP_STAGE_SETUP = 0
USBPCAP_STAGE_DATA = 1
USBPCAP_STAGE_STATUS = 2
USBPCAP_STAGE_COMPLETE = 3

DESC_DEVICE = 0x01
DESC_CONFIGURATION = 0x02
DESC_STRING = 0x03
DESC_HID_REPORT = 0x22
DESC_NAMES = {
    DESC_DEVICE: "device",
    DESC_CONFIGURATION: "configuration",
    DESC_STRING: "string",
    0x21: "hid",
    DESC_HID_REPORT: "hid-report",
}

REQ_GET_DESCRIPTOR = 0x06
HID_REQUEST_NAMES = {0x01: "GET_REPORT", 0x09: "SET_REPORT", 0x0A: "SET_IDLE", 0x0B: "SET_PROTOCOL"}
HID_REPORT_TYPES = {1: "input", 2: "output", 3: "feature"}


@dataclass(frozen=True)
class Setup:
    bm_request_type: int
    b_request: int
    w_value: int
    w_index: int
    w_length: int

    @classmethod
    def parse(cls, raw: bytes) -> "Setup":
        return cls(*struct.unpack("<BBHHH", raw[:8]))

    @property
    def is_in(self) -> bool:
        return bool(self.bm_request_type & 0x80)

    @property
    def is_standard(self) -> bool:
        return (self.bm_request_type >> 5) & 0x03 == 0

    @property
    def is_class(self) -> bool:
        return (self.bm_request_type >> 5) & 0x03 == 1

    def describe(self) -> str:
        if self.is_standard and self.b_request == REQ_GET_DESCRIPTOR:
            dtype = self.w_value >> 8
            name = DESC_NAMES.get(dtype, f"0x{dtype:02x}")
            return f"GET_DESCRIPTOR {name} idx={self.w_value & 0xFF} wIndex={self.w_index}"
        if self.is_class and self.b_request in HID_REQUEST_NAMES:
            rtype = HID_REPORT_TYPES.get(self.w_value >> 8, f"0x{self.w_value >> 8:02x}")
            return (f"HID {HID_REQUEST_NAMES[self.b_request]} {rtype} "
                    f"id=0x{self.w_value & 0xFF:02x} iface={self.w_index} len={self.w_length}")
        return (f"bmRT=0x{self.bm_request_type:02x} bReq=0x{self.b_request:02x} "
                f"wValue=0x{self.w_value:04x} wIndex=0x{self.w_index:04x} wLength={self.w_length}")


@dataclass
class Transfer:
    ts: float
    bus: int
    device: int
    endpoint: int  # includes direction bit 0x80
    xfer_type: int
    setup: Setup | None = None
    data: bytes = b""
    status: int | None = None  # None = never completed in this capture
    complete: bool = False

    @property
    def address(self) -> str:
        return f"{self.bus}.{self.device}"

    @property
    def direction_in(self) -> bool:
        if self.xfer_type == XFER_CONTROL and self.setup is not None:
            return self.setup.is_in
        return bool(self.endpoint & 0x80)

    @property
    def direction(self) -> str:
        return "IN " if self.direction_in else "OUT"

    @property
    def ep_number(self) -> int:
        return self.endpoint & 0x0F

    @property
    def descriptor_type(self) -> int | None:
        s = self.setup
        if s is not None and s.is_standard and s.b_request == REQ_GET_DESCRIPTOR and s.is_in:
            return s.w_value >> 8
        return None


@dataclass
class _Pending:
    transfer: Transfer
    chunks: list[bytes] = field(default_factory=list)


def iter_transfers(packets: Iterable[RawPacket]) -> Iterator[Transfer]:
    """Pair submissions and completions into transfers, in submission order."""
    pending: dict[tuple[int, int], _Pending] = {}
    order: list[_Pending] = []
    emitted = 0

    def flush_ready() -> Iterator[Transfer]:
        nonlocal emitted, order
        # Emit in submission order so output stays chronological.
        while emitted < len(order) and order[emitted].transfer.complete:
            yield _finalise(order[emitted])
            emitted += 1
        if emitted > 256:
            order = order[emitted:]
            emitted = 0

    for pkt in packets:
        if pkt.linktype == LINKTYPE_USBPCAP:
            _handle_usbpcap(pkt, pending, order)
        elif pkt.linktype in (LINKTYPE_USB_LINUX, LINKTYPE_USB_LINUX_MMAPPED):
            _handle_usbmon(pkt, pending, order)
        else:
            continue
        yield from flush_ready()

    for p in order[emitted:]:
        yield _finalise(p)


def _finalise(p: _Pending) -> Transfer:
    p.transfer.data = b"".join(p.chunks)
    return p.transfer


def _handle_usbpcap(pkt: RawPacket, pending: dict, order: list) -> None:
    d = pkt.data
    if len(d) < 27:
        return
    header_len, irp_id, status, _function, info, bus, device, endpoint, xfer, data_len = \
        struct.unpack("<HQIHBHHBBI", d[:27])
    stage = d[27] if xfer == XFER_CONTROL and header_len >= 28 else None
    payload = d[header_len:header_len + data_len]
    is_completion = bool(info & USBPCAP_INFO_PDO_TO_FDO)
    key = (bus << 16 | device, irp_id)

    p = pending.get(key)
    if p is None:
        p = _Pending(Transfer(pkt.ts, bus, device, endpoint, xfer))
        pending[key] = p
        order.append(p)

    t = p.transfer
    if xfer == XFER_CONTROL:
        if stage == USBPCAP_STAGE_SETUP and len(payload) >= 8:
            t.setup = Setup.parse(payload)
            # Some USBPcap versions put OUT data right after the setup bytes.
            if len(payload) > 8:
                p.chunks.append(payload[8:])
        elif stage == USBPCAP_STAGE_DATA and payload:
            p.chunks.append(payload)
        if is_completion and stage in (USBPCAP_STAGE_STATUS, USBPCAP_STAGE_COMPLETE):
            t.status = status
            t.complete = True
            del pending[key]
        return

    is_in = bool(endpoint & 0x80)
    if is_completion:
        if is_in:
            p.chunks.append(payload)
        t.status = status
        t.complete = True
        del pending[key]
    elif not is_in:
        p.chunks.append(payload)


_USBMON_HDR = struct.Struct("<QBBBBHbbqiiII8s")


def _handle_usbmon(pkt: RawPacket, pending: dict, order: list) -> None:
    hdr_len = 64 if pkt.linktype == LINKTYPE_USB_LINUX_MMAPPED else 48
    d = pkt.data
    if len(d) < hdr_len:
        return
    (urb_id, ev_type, xfer, epnum, devnum, busnum, flag_setup, _flag_data,
     ts_sec, ts_usec, status, _length, len_cap, setup_raw) = _USBMON_HDR.unpack(d[:48])
    payload = d[hdr_len:hdr_len + len_cap]
    key = (busnum << 16 | devnum, urb_id)
    ts = ts_sec + ts_usec / 1e6

    if ev_type == ord("S"):
        p = _Pending(Transfer(ts, busnum, devnum, epnum, xfer))
        pending[key] = p
        order.append(p)
        if xfer == XFER_CONTROL and flag_setup == 0:
            p.transfer.setup = Setup.parse(setup_raw)
        if not p.transfer.direction_in and payload:
            p.chunks.append(payload)
        return

    p = pending.pop(key, None)
    if p is None:
        # Completion without a submission in this capture (capture started mid-transfer).
        p = _Pending(Transfer(ts, busnum, devnum, epnum, xfer))
        order.append(p)
    t = p.transfer
    if t.direction_in and payload:
        p.chunks.append(payload)
    t.status = status
    t.complete = True
