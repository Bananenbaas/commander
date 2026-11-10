# SPDX-License-Identifier: GPL-2.0-or-later
import contextlib
import io
import os
import tempfile
import unittest

from kb7tools.analysis import Capture, diff_lines, summary_lines
from kb7tools.cli import main
from kb7tools.descriptors import summarise_hid_report_descriptor
from kb7tools.pcap import CaptureFormatError, read_packets
from kb7tools.usb import XFER_CONTROL, XFER_INTERRUPT

from tests import synth

FEATURE = bytes([0x05]) + bytes(range(1, 64))
KEYSTROKE = bytes([0, 0, 0x04, 0, 0, 0, 0, 0])  # 'a' pressed


def build_session(colour: int, *, split_control: bool = False) -> synth.UsbPcapBuilder:
    b = synth.UsbPcapBuilder()
    b.enumerate()
    b.control_out(synth.setup_bytes(0x21, 0x09, 0x0305, 1, len(FEATURE)), FEATURE,
                  data_in_setup_packet=not split_control)
    b.interrupt_in(0x81, KEYSTROKE)
    b.interrupt_out(0x03, bytes([0x06, 0x10, colour, 0x00, 0xFF]) + bytes(27))
    b.interrupt_in(0x82, bytes([0x06, 0x00, 0x01]))
    return b


class TempDirTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = self._tmp.name

    def tearDown(self) -> None:
        self._tmp.cleanup()

    def path(self, name: str) -> str:
        return os.path.join(self.dir, name)

    def run_cli(self, *argv: str) -> tuple[int, str]:
        buf = io.StringIO()
        with contextlib.redirect_stdout(buf), contextlib.redirect_stderr(io.StringIO()):
            code = main(list(argv))
        return code, buf.getvalue()


class PcapReaderTests(TempDirTest):
    def test_pcapng_and_pcap_yield_same_packets(self) -> None:
        packets = build_session(0x11).packets
        synth.write_pcapng(self.path("a.pcapng"), 249, packets)
        synth.write_pcap(self.path("a.pcap"), 249, packets)
        ng = list(read_packets(self.path("a.pcapng")))
        classic = list(read_packets(self.path("a.pcap")))
        self.assertEqual([p.data for p in ng], [d for _, d in packets])
        self.assertEqual([p.data for p in classic], [d for _, d in packets])
        self.assertAlmostEqual(ng[-1].ts, packets[-1][0], places=6)

    def test_nanosecond_resolution_option(self) -> None:
        packets = [(1.000000123, b"\x00" * 30)]
        synth.write_pcapng(self.path("ns.pcapng"), 249, packets, nanosecond=True)
        (pkt,) = list(read_packets(self.path("ns.pcapng")))
        self.assertAlmostEqual(pkt.ts, 1.000000123, places=9)

    def test_rejects_non_capture(self) -> None:
        with open(self.path("junk.bin"), "wb") as fh:
            fh.write(b"not a capture file")
        with self.assertRaises(CaptureFormatError):
            list(read_packets(self.path("junk.bin")))


class UsbPcapTransferTests(TempDirTest):
    def capture(self, **kw) -> Capture:
        synth.write_pcapng(self.path("s.pcapng"), 249, build_session(0x11, **kw).packets)
        return Capture(self.path("s.pcapng"))

    def test_device_inventory(self) -> None:
        cap = self.capture()
        dev = cap.devices["1.5"]
        self.assertEqual(dev.descriptor.vid_pid, "1234:5678")
        self.assertEqual(dev.name, "1234:5678 Example Corp Test Keyboard")
        self.assertEqual([i.number for i in dev.interfaces], [0, 1])
        self.assertEqual(dev.keyboard_endpoints(), {0x81})
        self.assertEqual(dev.hid_reports[1], synth.HID_REPORT_DESC)

    def test_set_report_payload_both_usbpcap_layouts(self) -> None:
        for split in (False, True):
            with self.subTest(split_control=split):
                cap = self.capture(split_control=split)
                sel = cap.select({"1.5"}, direction="out")
                ctrl = [t for t in sel if t.xfer_type == XFER_CONTROL]
                self.assertEqual(len(ctrl), 1)
                self.assertEqual(ctrl[0].data, FEATURE)
                self.assertIn("HID SET_REPORT feature id=0x05 iface=1", ctrl[0].setup.describe())

    def test_interrupt_directions(self) -> None:
        cap = self.capture()
        intr = [t for t in cap.transfers if t.xfer_type == XFER_INTERRUPT]
        self.assertEqual([(t.endpoint, t.direction_in) for t in intr],
                         [(0x81, True), (0x03, False), (0x82, True)])
        self.assertEqual(intr[2].data, bytes([0x06, 0x00, 0x01]))
        self.assertTrue(all(t.complete and t.status == 0 for t in cap.transfers))

    def test_standard_requests_hidden_by_default(self) -> None:
        cap = self.capture()
        self.assertFalse(any(t.setup is not None and t.setup.is_standard for t in cap.select(None)))
        self.assertTrue(any(t.descriptor_type == 1 for t in cap.select(None, include_standard=True)))

    def test_vid_pid_selector(self) -> None:
        cap = self.capture()
        self.assertEqual(cap.resolve_device("1234:5678"), {"1.5"})
        with self.assertRaises(LookupError):
            cap.resolve_device("dead:beef")


class UsbmonTests(TempDirTest):
    def test_usbmon_mmapped_and_plain(self) -> None:
        setup = synth.setup_bytes(0x21, 0x09, 0x0305, 1, len(FEATURE))
        for mmapped, linktype in ((True, 220), (False, 189)):
            with self.subTest(linktype=linktype):
                pk = [
                    (0.1, synth.usbmon_packet(1, "S", 2, 0x00, 7, 3, 0.1, setup, FEATURE, mmapped)),
                    (0.2, synth.usbmon_packet(2, "S", 1, 0x82, 7, 3, 0.2, None, b"", mmapped)),
                    (0.3, synth.usbmon_packet(1, "C", 2, 0x00, 7, 3, 0.3, None, b"", mmapped)),
                    (0.4, synth.usbmon_packet(2, "C", 1, 0x82, 7, 3, 0.4, None, b"\x06\x01", mmapped)),
                ]
                synth.write_pcapng(self.path("m.pcapng"), linktype, pk)
                cap = Capture(self.path("m.pcapng"))
                self.assertEqual(len(cap.transfers), 2)
                ctrl, intr = cap.transfers
                self.assertEqual((ctrl.address, ctrl.data, ctrl.direction_in), ("3.7", FEATURE, False))
                self.assertEqual((intr.data, intr.direction_in), (b"\x06\x01", True))


class DescriptorTests(unittest.TestCase):
    def test_hid_report_summary(self) -> None:
        reports = summarise_hid_report_descriptor(synth.HID_REPORT_DESC)
        self.assertEqual([(r.report_id, r.usage_page, r.feature_bits, r.output_bits) for r in reports],
                         [(5, 0xFF00, 512, 0), (6, 0xFF00, 0, 256)])


class DiffTests(TempDirTest):
    def test_single_byte_difference(self) -> None:
        synth.write_pcapng(self.path("red.pcapng"), 249, build_session(0x11).packets)
        synth.write_pcapng(self.path("blue.pcapng"), 249, build_session(0x22).packets)
        a, b = Capture(self.path("red.pcapng")), Capture(self.path("blue.pcapng"))
        lines = diff_lines(a.select(None, direction="out"), b.select(None, direction="out"))
        self.assertEqual(lines[0], "A: 2 transfers, B: 2 transfers, 1 differ")
        self.assertIn("0000 A 06 10 11 00 ff", "\n".join(lines))
        self.assertIn("0000 B 06 10 22 00 ff", "\n".join(lines))
        marker = next(l for l in lines if "^^" in l)
        self.assertEqual(marker.strip(), "^^")

    def test_identical_captures(self) -> None:
        synth.write_pcapng(self.path("x.pcapng"), 249, build_session(0x11).packets)
        code, out = self.run_cli("diff", self.path("x.pcapng"), self.path("x.pcapng"))
        self.assertEqual(code, 0)
        self.assertTrue(out.startswith("A: 3 transfers, B: 3 transfers, 0 differ"))


class CliTests(TempDirTest):
    def setUp(self) -> None:
        super().setUp()
        synth.write_pcapng(self.path("s.pcapng"), 249, build_session(0x11).packets)

    def test_summary(self) -> None:
        code, out = self.run_cli("summary", self.path("s.pcapng"))
        self.assertEqual(code, 0)
        self.assertIn("device 1.5: 1234:5678 Example Corp Test Keyboard", out)
        self.assertIn("iface 0 alt 0: HID boot-keyboard", out)
        self.assertIn("report id 0x05  usage page 0xff00 (vendor)  feature=64B", out)

    def test_keystrokes_hidden_unless_requested(self) -> None:
        _, out = self.run_cli("dump", self.path("s.pcapng"), "-e", "0x81")
        self.assertIn("keystroke report hidden", out)
        self.assertNotIn("00 00 04 00", out)
        _, out = self.run_cli("dump", self.path("s.pcapng"), "-e", "0x81", "--show-keystrokes")
        self.assertIn("00 00 04 00", out)

    def test_diff_excludes_keystrokes(self) -> None:
        _, out = self.run_cli("diff", self.path("s.pcapng"), self.path("s.pcapng"), "--equal")
        self.assertNotIn("ep 0x81", out)

    def test_errors_return_code_2(self) -> None:
        code, _ = self.run_cli("summary", self.path("missing.pcapng"))
        self.assertEqual(code, 2)
        code, _ = self.run_cli("dump", self.path("s.pcapng"), "-d", "9.9")
        self.assertEqual(code, 2)


if __name__ == "__main__":
    unittest.main()
