# SPDX-License-Identifier: GPL-2.0-or-later
"""kb7cap — inspect and compare USB captures of the KB7."""

from __future__ import annotations

import argparse
import sys

from . import __version__
from .analysis import Capture, diff_lines, format_transfer, hexdump, summary_lines
from .pcap import CaptureFormatError

REDACTED = "      [keystroke report hidden — use --show-keystrokes to display]"


def _endpoint(value: str) -> int:
    return int(value, 0)


def _add_filters(p: argparse.ArgumentParser) -> None:
    p.add_argument("-d", "--device", help="device to show: 'bus.dev' or 'vid:pid' (hex)")
    p.add_argument("-e", "--endpoint", type=_endpoint, help="endpoint address incl. direction bit, e.g. 0x02 or 0x83")
    p.add_argument("--dir", choices=("in", "out"), help="only one direction")
    p.add_argument("--standard", action="store_true", help="include standard control requests (descriptors etc.)")
    p.add_argument("--max-bytes", type=int, default=256, help="bytes of payload to print per transfer (0 = all)")
    p.add_argument("--show-keystrokes", action="store_true",
                   help="do not hide keyboard reports (may reveal typed text)")


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="kb7cap", description=__doc__)
    parser.add_argument("--version", action="version", version=f"%(prog)s {__version__}")
    sub = parser.add_subparsers(dest="command", required=True)

    p_sum = sub.add_parser("summary", help="devices, interfaces, HID reports and traffic per endpoint")
    p_sum.add_argument("capture")

    p_dump = sub.add_parser("dump", help="list transfers with hex payload")
    p_dump.add_argument("capture")
    _add_filters(p_dump)
    p_dump.add_argument("--no-hex", action="store_true", help="one line per transfer, no payload")
    p_dump.add_argument("--empty", action="store_true", help="include transfers without payload")

    p_diff = sub.add_parser("diff", help="align two captures and show which bytes differ")
    p_diff.add_argument("capture_a")
    p_diff.add_argument("capture_b")
    _add_filters(p_diff)
    p_diff.add_argument("--equal", action="store_true", help="also list identical transfers")
    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        if args.command == "summary":
            print("\n".join(summary_lines(Capture(args.capture))))
        elif args.command == "dump":
            _dump(args)
        else:
            _diff(args)
    except (OSError, CaptureFormatError, LookupError) as exc:
        print(f"kb7cap: {exc}", file=sys.stderr)
        return 2
    except BrokenPipeError:
        return 0
    return 0


def _limit(args: argparse.Namespace) -> int | None:
    return None if args.max_bytes == 0 else args.max_bytes


def _dump(args: argparse.Namespace) -> None:
    cap = Capture(args.capture)
    transfers = cap.select(cap.resolve_device(args.device), include_standard=args.standard,
                           include_empty=args.empty, direction=args.dir, endpoint=args.endpoint)
    t0 = cap.transfers[0].ts if cap.transfers else 0.0
    for t in transfers:
        print(format_transfer(t, t0))
        if args.no_hex or not t.data:
            continue
        if cap.is_keystroke(t) and not args.show_keystrokes:
            print(REDACTED)
        else:
            print("\n".join(hexdump(t.data, limit=_limit(args))))


def _diff(args: argparse.Namespace) -> None:
    sides = []
    for path in (args.capture_a, args.capture_b):
        cap = Capture(path)
        sel = cap.select(cap.resolve_device(args.device), include_standard=args.standard,
                         direction=args.dir, endpoint=args.endpoint)
        if not args.show_keystrokes:
            sel = [t for t in sel if not cap.is_keystroke(t)]
        sides.append(sel)
    print("\n".join(diff_lines(sides[0], sides[1], show_equal=args.equal, limit=_limit(args))))
