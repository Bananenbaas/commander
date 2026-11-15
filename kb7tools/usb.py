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
    # When the request was queued. ``ts`` is when it completed, which is when an
    # IN transfer's data actually arrived; for a request queued before the
    # capture started the two are equal.
    submitted: float | None = None

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
    """Pair submissions and completions into transfers, in completion order.

    A transfer is timestamped when it completes. Host controllers queue
    interrupt IN requests long before the device has anything to send, so the
    submission time says nothing about when the data arrived; ordering and
    timing by it put each report next to the previous one. Transfers that never
    complete within the capture come last, ordered by submission.
    """
    pending: dict[tuple[int, int], _Pending] = {}

    for pkt in packets:
        if pkt.linktype == LINKTYPE_USBPCAP:
            done = _handle_usbpcap(pkt, pending)
        elif pkt.linktype in (LINKTYPE_USB_LINUX, LINKTYPE_USB_LINUX_MMAPPED):
            done = _handle_usbmon(pkt, pending)
        else:
            continue
        if done is not None:
            yield _finalise(done)

    for p in sorted(pending.values(), key=lambda p: p.transfer.ts):
        yield _finalise(p)


def _finalise(p: _Pending) -> Transfer:
    p.transfer.data = b"".join(p.chunks)
    return p.transfer


def _complete(p: _Pending, ts: float, status: int) -> _Pending:
    t = p.transfer
    t.ts = ts
    t.status = status
    t.complete = True
    return p


def _handle_usbpcap(pkt: RawPacket, pending: dict) -> _Pending | None:
    d = pkt.data
    if len(d) < 27:
        return None
    header_len, irp_id, status, _function, info, bus, device, endpoint, xfer, data_len = \
        struct.unpack("<HQIHBHHBBI", d[:27])
    stage = d[27] if xfer == XFER_CONTROL and header_len >= 28 else None
    payload = d[header_len:header_len + data_len]
    is_completion = bool(info & USBPCAP_INFO_PDO_TO_FDO)
    key = (bus << 16 | device, irp_id)

    p = pending.get(key)
    if p is None:
        p = _Pending(Transfer(pkt.ts, bus, device, endpoint, xfer, submitted=pkt.ts))
        pending[key] = p

    t = p.transfer
    if xfer == XFER_CONTROL:
        if stage == USBPCAP_STAGE_SETUP and len(payload) >= 8:
            t.setup = Setup.parse(payload)
            # Some USBPcap versions put OUT data right after the setup bytes.
            if len(payload) > 8:
                p.chunks.append(payload[8:])
        elif stage == USBPCAP_STAGE_DATA and payload:
            p.chunks.append(payload)
        elif stage == USBPCAP_STAGE_COMPLETE and payload and not p.chunks:
            # USBPcap 1.5.4 emits no DATA stage for a control transfer: the data
            # is attached to the COMPLETE stage instead. Only take it when no
            # earlier stage supplied any, so a capture that has both layouts
            # does not count the same bytes twice.
            p.chunks.append(payload)
        if is_completion and stage in (USBPCAP_STAGE_STATUS, USBPCAP_STAGE_COMPLETE):
            del pending[key]
            return _complete(p, pkt.ts, status)
        return None

    is_in = bool(endpoint & 0x80)
    if is_completion:
        if is_in:
            p.chunks.append(payload)
        del pending[key]
        return _complete(p, pkt.ts, status)
    if not is_in:
        p.chunks.append(payload)
    return None


_USBMON_HDR = struct.Struct("<QBBBBHbbqiiII8s")


def _handle_usbmon(pkt: RawPacket, pending: dict) -> _Pending | None:
    hdr_len = 64 if pkt.linktype == LINKTYPE_USB_LINUX_MMAPPED else 48
    d = pkt.data
    if len(d) < hdr_len:
        return None
    (urb_id, ev_type, xfer, epnum, devnum, busnum, flag_setup, _flag_data,
     ts_sec, ts_usec, status, _length, len_cap, setup_raw) = _USBMON_HDR.unpack(d[:48])
    payload = d[hdr_len:hdr_len + len_cap]
    key = (busnum << 16 | devnum, urb_id)
    ts = ts_sec + ts_usec / 1e6

    if ev_type == ord("S"):
        p = _Pending(Transfer(ts, busnum, devnum, epnum, xfer, submitted=ts))
        pending[key] = p
        if xfer == XFER_CONTROL and flag_setup == 0:
            p.transfer.setup = Setup.parse(setup_raw)
        if not p.transfer.direction_in and payload:
            p.chunks.append(payload)
        return None

    p = pending.pop(key, None)
    if p is None:
        # Completion without a submission in this capture (capture started mid-transfer).
        p = _Pending(Transfer(ts, busnum, devnum, epnum, xfer, submitted=ts))
    if p.transfer.direction_in and payload:
        p.chunks.append(payload)
    return _complete(p, ts, status)
