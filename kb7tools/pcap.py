# SPDX-License-Identifier: GPL-2.0-or-later
"""Minimal pcap / pcapng reader (stdlib only).

Yields (timestamp_seconds, linktype, packet_bytes) for every captured packet.
Only the block types needed for USB captures are interpreted; everything else
is skipped.
"""

from __future__ import annotations

import struct
from dataclasses import dataclass
from typing import BinaryIO, Iterator

PCAPNG_SHB = 0x0A0D0D0A
PCAPNG_IDB = 0x00000001
PCAPNG_SPB = 0x00000003
PCAPNG_EPB = 0x00000006
PCAPNG_BOM = 0x1A2B3C4D

PCAP_MAGIC_US = 0xA1B2C3D4
PCAP_MAGIC_NS = 0xA1B23C4D

OPT_ENDOFOPT = 0
OPT_IF_TSRESOL = 9


class CaptureFormatError(ValueError):
    pass


@dataclass(frozen=True)
class RawPacket:
    ts: float
    linktype: int
    data: bytes


@dataclass
class _Interface:
    linktype: int
    ts_divisor: float


def read_packets(path: str) -> Iterator[RawPacket]:
    with open(path, "rb") as fh:
        head = fh.read(4)
        fh.seek(0)
        if len(head) < 4:
            raise CaptureFormatError(f"{path}: file too short")
        magic_le = struct.unpack("<I", head)[0]
        magic_be = struct.unpack(">I", head)[0]
        if magic_le == PCAPNG_SHB:
            yield from _read_pcapng(fh)
        elif PCAP_MAGIC_US in (magic_le, magic_be) or PCAP_MAGIC_NS in (magic_le, magic_be):
            yield from _read_pcap(fh)
        else:
            raise CaptureFormatError(f"{path}: not a pcap or pcapng file")


def _read_pcap(fh: BinaryIO) -> Iterator[RawPacket]:
    hdr = fh.read(24)
    if len(hdr) < 24:
        raise CaptureFormatError("truncated pcap header")
    magic_le = struct.unpack("<I", hdr[:4])[0]
    if magic_le in (PCAP_MAGIC_US, PCAP_MAGIC_NS):
        endian = "<"
        magic = magic_le
    else:
        endian = ">"
        magic = struct.unpack(">I", hdr[:4])[0]
    divisor = 1e9 if magic == PCAP_MAGIC_NS else 1e6
    linktype = struct.unpack(endian + "I", hdr[20:24])[0] & 0x0FFFFFFF
    rec = struct.Struct(endian + "IIII")
    while True:
        rh = fh.read(16)
        if len(rh) < 16:
            return
        ts_sec, ts_frac, incl_len, _orig_len = rec.unpack(rh)
        data = fh.read(incl_len)
        if len(data) < incl_len:
            return
        yield RawPacket(ts_sec + ts_frac / divisor, linktype, data)


def _read_pcapng(fh: BinaryIO) -> Iterator[RawPacket]:
    endian = "<"
    interfaces: list[_Interface] = []
    while True:
        bh = fh.read(8)
        if len(bh) < 8:
            return
        block_type = struct.unpack("<I", bh[:4])[0]
        if block_type == PCAPNG_SHB:
            # Byte order is defined by the BOM inside each section header.
            bom_raw = fh.read(4)
            if struct.unpack("<I", bom_raw)[0] == PCAPNG_BOM:
                endian = "<"
            elif struct.unpack(">I", bom_raw)[0] == PCAPNG_BOM:
                endian = ">"
            else:
                raise CaptureFormatError("bad pcapng byte-order magic")
            total_len = struct.unpack(endian + "I", bh[4:8])[0]
            fh.read(total_len - 12)
            interfaces = []
            continue

        total_len = struct.unpack(endian + "I", bh[4:8])[0]
        if total_len < 12:
            raise CaptureFormatError("corrupt pcapng block length")
        body = fh.read(total_len - 8)
        if len(body) < total_len - 8:
            return
        body = body[:-4]  # trailing block-length copy

        if block_type == PCAPNG_IDB:
            linktype = struct.unpack(endian + "H", body[0:2])[0]
            interfaces.append(_Interface(linktype, _parse_tsresol(body[8:], endian)))
        elif block_type == PCAPNG_EPB:
            if_id, ts_hi, ts_lo, cap_len, _orig = struct.unpack(endian + "IIIII", body[:20])
            if if_id >= len(interfaces):
                raise CaptureFormatError("packet references unknown interface")
            iface = interfaces[if_id]
            ts = ((ts_hi << 32) | ts_lo) / iface.ts_divisor
            yield RawPacket(ts, iface.linktype, bytes(body[20:20 + cap_len]))
        elif block_type == PCAPNG_SPB:
            if not interfaces:
                raise CaptureFormatError("simple packet without interface")
            orig_len = struct.unpack(endian + "I", body[:4])[0]
            yield RawPacket(0.0, interfaces[0].linktype, bytes(body[4:4 + orig_len]))


def _parse_tsresol(options: bytes, endian: str) -> float:
    pos = 0
    while pos + 4 <= len(options):
        code, length = struct.unpack(endian + "HH", options[pos:pos + 4])
        if code == OPT_ENDOFOPT:
            break
        value = options[pos + 4:pos + 4 + length]
        if code == OPT_IF_TSRESOL and length >= 1:
            res = value[0]
            return float(2 ** (res & 0x7F)) if res & 0x80 else float(10 ** res)
        pos += 4 + length + ((4 - length % 4) % 4)
    return 1e6
