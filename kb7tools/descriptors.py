# SPDX-License-Identifier: GPL-2.0-or-later
"""Parsers for the USB / HID descriptors that show up in a capture."""

from __future__ import annotations

import struct
from dataclasses import dataclass, field

USB_CLASS_HID = 0x03
HID_PROTOCOL_KEYBOARD = 0x01
HID_USAGE_PAGE_KEYBOARD = 0x07  # a report on this page carries what was typed


@dataclass(frozen=True)
class DeviceDescriptor:
    usb_version: int
    device_class: int
    max_packet0: int
    vendor_id: int
    product_id: int
    device_version: int
    i_manufacturer: int
    i_product: int
    i_serial: int

    @classmethod
    def parse(cls, raw: bytes) -> "DeviceDescriptor | None":
        if len(raw) < 18 or raw[0] < 18 or raw[1] != 0x01:
            return None
        (_bl, _bt, bcd_usb, dev_class, _sub, _proto, mps0, vid, pid, bcd_dev,
         i_mfr, i_prod, i_ser, _ncfg) = struct.unpack("<BBHBBBBHHHBBBB", raw[:18])
        return cls(bcd_usb, dev_class, mps0, vid, pid, bcd_dev, i_mfr, i_prod, i_ser)

    @property
    def vid_pid(self) -> str:
        return f"{self.vendor_id:04x}:{self.product_id:04x}"


@dataclass(frozen=True)
class Endpoint:
    address: int
    attributes: int
    max_packet: int
    interval: int

    @property
    def type_name(self) -> str:
        return ("ctrl", "iso", "bulk", "intr")[self.attributes & 0x03]


@dataclass
class Interface:
    number: int
    alt: int
    iface_class: int
    subclass: int
    protocol: int
    endpoints: list[Endpoint] = field(default_factory=list)

    @property
    def is_boot_keyboard(self) -> bool:
        return self.iface_class == USB_CLASS_HID and self.protocol == HID_PROTOCOL_KEYBOARD

    def describe(self) -> str:
        cls = {0x03: "HID", 0x01: "audio", 0x02: "CDC", 0x0A: "CDC-data", 0xFF: "vendor"}.get(
            self.iface_class, f"class 0x{self.iface_class:02x}")
        extra = ""
        if self.iface_class == USB_CLASS_HID:
            extra = {0: "", 1: " boot-keyboard", 2: " boot-mouse"}.get(self.protocol, f" proto={self.protocol}")
        return f"iface {self.number} alt {self.alt}: {cls}{extra}"


def parse_configuration(raw: bytes) -> list[Interface]:
    interfaces: list[Interface] = []
    pos = 0
    while pos + 2 <= len(raw):
        length, dtype = raw[pos], raw[pos + 1]
        if length < 2:
            break
        body = raw[pos:pos + length]
        if dtype == 0x04 and len(body) >= 9:
            interfaces.append(Interface(body[2], body[3], body[5], body[6], body[7]))
        elif dtype == 0x05 and len(body) >= 7 and interfaces:
            mps = struct.unpack("<H", body[4:6])[0]
            interfaces[-1].endpoints.append(Endpoint(body[2], body[3], mps, body[6]))
        pos += length
    return interfaces


def parse_string(raw: bytes) -> str | None:
    if len(raw) < 2 or raw[1] != 0x03:
        return None
    return raw[2:raw[0]].decode("utf-16-le", errors="replace")


@dataclass
class ReportInfo:
    report_id: int
    usage_page: int
    input_bits: int = 0
    output_bits: int = 0
    feature_bits: int = 0


def summarise_hid_report_descriptor(raw: bytes) -> list[ReportInfo]:
    """Per report ID: usage page and payload size per report type.

    Deliberately a summary, not a full HID parser: it answers "which report IDs
    exist, how big are they, and are they vendor-defined".
    """
    reports: dict[int, ReportInfo] = {}
    usage_page = 0
    report_id = 0
    report_size = 0
    report_count = 0
    global_stack: list[tuple[int, int, int, int]] = []
    pos = 0
    while pos < len(raw):
        prefix = raw[pos]
        if prefix == 0xFE:  # long item
            if pos + 1 >= len(raw):
                break
            pos += 3 + raw[pos + 1]
            continue
        size = (0, 1, 2, 4)[prefix & 0x03]
        item_type = (prefix >> 2) & 0x03
        tag = prefix >> 4
        value = int.from_bytes(raw[pos + 1:pos + 1 + size], "little")
        pos += 1 + size

        if item_type == 1:  # global
            if tag == 0x0:
                usage_page = value
            elif tag == 0x7:
                report_size = value
            elif tag == 0x8:
                report_id = value
            elif tag == 0x9:
                report_count = value
            elif tag == 0xA:
                global_stack.append((usage_page, report_id, report_size, report_count))
            elif tag == 0xB and global_stack:
                usage_page, report_id, report_size, report_count = global_stack.pop()
        elif item_type == 0 and tag in (0x8, 0x9, 0xB):  # input / output / feature
            info = reports.setdefault(report_id, ReportInfo(report_id, usage_page))
            bits = report_size * report_count
            if tag == 0x8:
                info.input_bits += bits
            elif tag == 0x9:
                info.output_bits += bits
            else:
                info.feature_bits += bits
    return [reports[k] for k in sorted(reports)]
