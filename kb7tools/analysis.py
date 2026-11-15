# SPDX-License-Identifier: GPL-2.0-or-later
"""Capture-level analysis: device inventory, filtering, formatting, diffing."""

from __future__ import annotations

import difflib
from collections import Counter
from dataclasses import dataclass, field

from .descriptors import (HID_USAGE_PAGE_KEYBOARD, DeviceDescriptor, Interface, ReportInfo,
                          parse_configuration, parse_string, summarise_hid_report_descriptor)
from .pcap import read_packets
from .usb import (DESC_CONFIGURATION, DESC_DEVICE, DESC_HID_REPORT, DESC_STRING, XFER_CONTROL,
                  XFER_NAMES, Transfer, iter_transfers)


@dataclass
class DeviceInfo:
    address: str
    descriptor: DeviceDescriptor | None = None
    strings: dict[int, str] = field(default_factory=dict)
    interfaces: list[Interface] = field(default_factory=list)
    hid_reports: dict[int, bytes] = field(default_factory=dict)  # interface -> raw descriptor

    @property
    def name(self) -> str:
        d = self.descriptor
        if d is None:
            return "(no device descriptor in capture)"
        parts = [self.strings.get(d.i_manufacturer, ""), self.strings.get(d.i_product, "")]
        label = " ".join(p for p in parts if p).strip()
        return f"{d.vid_pid} {label}".strip()

    def keyboard_endpoints(self) -> set[int]:
        """Endpoints of boot-keyboard interfaces: every report on them is typing."""
        return {ep.address for i in self.interfaces if i.is_boot_keyboard for ep in i.endpoints}

    def keystroke_report_ids(self, iface_no: int) -> set[int]:
        """Report IDs on one interface that carry keyboard usages.

        A keyboard report is not confined to boot-keyboard interfaces. The KB7
        exposes one on an otherwise vendor-defined interface, mixed in with
        unrelated reports on the same endpoint, so hiding whole endpoints would
        hide far too much. Match on the report ID instead.
        """
        raw = self.hid_reports.get(iface_no)
        if not raw:
            return set()
        return {r.report_id for r in summarise_hid_report_descriptor(raw)
                if r.usage_page == HID_USAGE_PAGE_KEYBOARD}

    def interface_for_endpoint(self, ep_address: int) -> Interface | None:
        for iface in self.interfaces:
            if any(ep.address == ep_address for ep in iface.endpoints):
                return iface
        return None


class Capture:
    def __init__(self, path: str):
        self.path = path
        self.transfers: list[Transfer] = list(iter_transfers(read_packets(path)))
        self.devices: dict[str, DeviceInfo] = {}
        self._index_descriptors()

    def _index_descriptors(self) -> None:
        for t in self.transfers:
            dev = self.devices.setdefault(t.address, DeviceInfo(t.address))
            dtype = t.descriptor_type
            if dtype is None or not t.data or t.setup is None:
                continue
            if dtype == DESC_DEVICE:
                parsed = DeviceDescriptor.parse(t.data)
                if parsed is not None:
                    dev.descriptor = parsed
            elif dtype == DESC_CONFIGURATION:
                # The first, 9-byte read only carries the total length.
                ifaces = parse_configuration(t.data)
                if ifaces:
                    dev.interfaces = ifaces
            elif dtype == DESC_STRING:
                text = parse_string(t.data)
                index = t.setup.w_value & 0xFF
                if text is not None and index != 0:
                    dev.strings[index] = text
            elif dtype == DESC_HID_REPORT:
                prev = dev.hid_reports.get(t.setup.w_index, b"")
                if len(t.data) >= len(prev):
                    dev.hid_reports[t.setup.w_index] = t.data

    def resolve_device(self, selector: str | None) -> set[str] | None:
        """Selector is 'bus.dev' or 'vid:pid'. None means all devices."""
        if not selector:
            return None
        if ":" in selector:
            want = selector.lower()
            found = {a for a, d in self.devices.items()
                     if d.descriptor is not None and d.descriptor.vid_pid == want}
            if not found:
                raise LookupError(
                    f"no device with VID:PID {selector} in this capture "
                    "(were device descriptors captured? see docs/CAPTURE-GUIDE.md)")
            return found
        if selector not in self.devices:
            raise LookupError(f"no device at address {selector} in this capture")
        return {selector}

    def is_keystroke(self, t: Transfer) -> bool:
        dev = self.devices.get(t.address)
        if dev is None or not t.direction_in or t.xfer_type == XFER_CONTROL:
            return False
        if t.endpoint in dev.keyboard_endpoints():
            return True
        iface = dev.interface_for_endpoint(t.endpoint)
        if iface is None:
            return False
        ids = dev.keystroke_report_ids(iface.number)
        if not ids:
            return False
        # Report ID 0 means the reports on this interface carry no ID byte, so
        # nothing on it can be told apart: hide the lot.
        if 0 in ids:
            return True
        return bool(t.data) and t.data[0] in ids

    def select(self, addresses: set[str] | None, *, include_standard: bool = False,
               include_empty: bool = False, direction: str | None = None,
               endpoint: int | None = None) -> list[Transfer]:
        out = []
        for t in self.transfers:
            if addresses is not None and t.address not in addresses:
                continue
            if not include_standard and t.setup is not None and t.setup.is_standard:
                continue
            if not include_empty and not t.data and t.setup is None:
                continue
            if direction == "out" and t.direction_in:
                continue
            if direction == "in" and not t.direction_in:
                continue
            if endpoint is not None and t.endpoint != endpoint:
                continue
            out.append(t)
        return out


def hexdump(data: bytes, width: int = 16, limit: int | None = None, indent: str = "      ") -> list[str]:
    shown = data if limit is None else data[:limit]
    lines = []
    for off in range(0, len(shown), width):
        chunk = shown[off:off + width]
        hexpart = " ".join(f"{b:02x}" for b in chunk)
        asc = "".join(chr(b) if 32 <= b < 127 else "." for b in chunk)
        lines.append(f"{indent}{off:04x}  {hexpart:<{width * 3 - 1}}  {asc}")
    if limit is not None and len(data) > limit:
        lines.append(f"{indent}... {len(data) - limit} more bytes")
    return lines


def format_transfer(t: Transfer, t0: float) -> str:
    status = "" if t.status in (0, None) else f" status=0x{t.status & 0xFFFFFFFF:08x}"
    if not t.complete:
        status += " (no completion)"
    setup = f"  {t.setup.describe()}" if t.setup is not None else ""
    return (f"{t.ts - t0:11.6f}  {t.address:>6}  ep 0x{t.endpoint:02x} {t.direction} "
            f"{XFER_NAMES.get(t.xfer_type, '?'):<4} len={len(t.data):<5}{setup}{status}").rstrip()


def summary_lines(cap: Capture) -> list[str]:
    lines = [f"{cap.path}: {len(cap.transfers)} transfers"]
    if cap.transfers:
        lines.append(f"duration: {cap.transfers[-1].ts - cap.transfers[0].ts:.3f}s")
    per_dev: dict[str, Counter] = {}
    for t in cap.transfers:
        key = f"ep 0x{t.endpoint:02x} {t.direction} {XFER_NAMES.get(t.xfer_type, '?')}"
        per_dev.setdefault(t.address, Counter())[key] += 1
    for addr in sorted(per_dev, key=_addr_sort):
        dev = cap.devices[addr]
        lines.append("")
        lines.append(f"device {addr}: {dev.name}")
        for iface in dev.interfaces:
            eps = ", ".join(f"0x{e.address:02x} {e.type_name} mps={e.max_packet}" for e in iface.endpoints)
            lines.append(f"  {iface.describe()}  [{eps}]")
        for iface_no, raw in sorted(dev.hid_reports.items()):
            lines.append(f"  hid report descriptor (iface {iface_no}, {len(raw)} bytes):")
            for r in summarise_hid_report_descriptor(raw):
                lines.append("    " + _report_line(r))
        for key, n in sorted(per_dev[addr].items()):
            lines.append(f"  {key:<22} {n:>7} transfers")
    return lines


def _report_line(r: ReportInfo) -> str:
    page = f"0x{r.usage_page:04x}" + (" (vendor)" if r.usage_page >= 0xFF00 else "")
    sizes = []
    for name, bits in (("in", r.input_bits), ("out", r.output_bits), ("feature", r.feature_bits)):
        if bits:
            sizes.append(f"{name}={bits // 8}B" if bits % 8 == 0 else f"{name}={bits}bit")
    return f"report id 0x{r.report_id:02x}  usage page {page}  {' '.join(sizes)}"


def _addr_sort(addr: str) -> tuple[int, ...]:
    return tuple(int(x) for x in addr.split("."))


def _shape(t: Transfer) -> tuple:
    return (t.endpoint, t.setup.describe() if t.setup is not None else "", len(t.data))


def diff_lines(a: list[Transfer], b: list[Transfer], *, show_equal: bool = False,
               limit: int | None = None) -> list[str]:
    """Align two transfer sequences by shape, then show byte differences."""
    sm = difflib.SequenceMatcher(a=[_shape(t) for t in a], b=[_shape(t) for t in b], autojunk=False)
    lines: list[str] = []
    changed = 0
    for op, a1, a2, b1, b2 in sm.get_opcodes():
        if op == "equal" or (op == "replace" and a2 - a1 == b2 - b1):
            for i, j in zip(range(a1, a2), range(b1, b2)):
                ta, tb = a[i], b[j]
                if ta.data == tb.data and _shape(ta) == _shape(tb):
                    if show_equal:
                        lines.append(f"= A#{i} B#{j}  {_brief(ta)}")
                    continue
                changed += 1
                lines.append(f"~ A#{i} B#{j}  {_brief(ta)}  |  {_brief(tb)}")
                lines.extend(_byte_diff(ta.data, tb.data, limit))
        else:
            for i in range(a1, a2):
                changed += 1
                lines.append(f"- A#{i}  {_brief(a[i])}")
                lines.extend(hexdump(a[i].data, limit=limit))
            for j in range(b1, b2):
                changed += 1
                lines.append(f"+ B#{j}  {_brief(b[j])}")
                lines.extend(hexdump(b[j].data, limit=limit))
    lines.insert(0, f"A: {len(a)} transfers, B: {len(b)} transfers, {changed} differ")
    return lines


def _brief(t: Transfer) -> str:
    setup = f" {t.setup.describe()}" if t.setup is not None else ""
    return f"ep 0x{t.endpoint:02x} {t.direction.strip()} len={len(t.data)}{setup}"


def _byte_diff(da: bytes, db: bytes, limit: int | None, width: int = 16) -> list[str]:
    n = max(len(da), len(db))
    if limit is not None:
        n = min(n, limit)
    lines = []
    for off in range(0, n, width):
        ca, cb = da[off:off + width], db[off:off + width]
        if ca == cb:
            continue
        cols = max(len(ca), len(cb))
        ha = " ".join(f"{ca[k]:02x}" if k < len(ca) else "--" for k in range(cols))
        hb = " ".join(f"{cb[k]:02x}" if k < len(cb) else "--" for k in range(cols))
        marks = " ".join("^^" if (ca[k:k + 1] != cb[k:k + 1]) else "  " for k in range(cols))
        lines.append(f"      {off:04x} A {ha}")
        lines.append(f"      {off:04x} B {hb}")
        lines.append(f"             {marks.rstrip()}")
    return lines
