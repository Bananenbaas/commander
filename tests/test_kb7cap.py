# SPDX-License-Identifier: GPL-2.0-or-later
import contextlib
import io
import os
import tempfile
import unittest

from kb7tools.analysis import Capture, diff_lines, summary_lines
from kb7tools.cli import main
from kb7tools.descriptors import report_descriptor_complete, summarise_hid_report_descriptor
from kb7tools.pcap import CaptureFormatError, read_packets
from kb7tools.usb import XFER_CONTROL, XFER_INTERRUPT

from tests import synth

FEATURE = bytes([0x05]) + bytes(range(1, 64))
KEYSTROKE = bytes([0, 0, 0x04, 0, 0, 0, 0, 0])  # 'a' pressed


def build_session(colour: int, *, split_control: bool = False,
                  data_on_complete: bool = False) -> synth.UsbPcapBuilder:
    b = synth.UsbPcapBuilder()
    b.enumerate(data_on_complete=data_on_complete)
    b.control_out(synth.setup_bytes(0x21, 0x09, 0x0305, 1, len(FEATURE)), FEATURE,
                  data_in_setup_packet=not split_control)
    b.interrupt_in(0x81, KEYSTROKE)
    b.interrupt_out(0x03, bytes([0x06, 0x10, colour, 0x00, 0xFF]) + bytes(27))
    b.interrupt_in(0x82, bytes([0x06, 0x00, 0x01]))
    b.interrupt_in(0x82, synth.VENDOR_KEYSTROKE)
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

    def test_device_inventory_both_control_in_layouts(self) -> None:
        # data_on_complete=True is the layout real USBPcap 1.5.4 produces: the
        # descriptor rides on the COMPLETE stage with no DATA-stage packet.
        for on_complete in (False, True):
            with self.subTest(data_on_complete=on_complete):
                cap = self.capture(data_on_complete=on_complete)
                dev = cap.devices["1.5"]
                self.assertEqual(dev.descriptor.vid_pid, "1234:5678")
                self.assertEqual(dev.name, "1234:5678 Example Corp Test Keyboard")
                self.assertEqual([i.number for i in dev.interfaces], [0, 1])
                self.assertEqual(dev.keyboard_endpoints(), {0x81})
                self.assertEqual(dev.hid_reports[1], synth.HID_REPORT_DESC)

    def test_control_in_data_is_not_lost_on_complete_stage(self) -> None:
        cap = self.capture(data_on_complete=True)
        standard = [t for t in cap.select(None, include_standard=True)
                    if t.xfer_type == XFER_CONTROL and t.descriptor_type == 1]
        self.assertEqual(len(standard), 1)
        self.assertEqual(standard[0].data, synth.DEVICE_DESC)

    def test_complete_stage_payload_is_not_counted_twice(self) -> None:
        # A capture that has both a DATA stage and a COMPLETE stage carrying the
        # same bytes must still yield the payload exactly once.
        b = synth.UsbPcapBuilder()
        setup = synth.setup_bytes(0x80, 6, 0x0100, 0, 18)
        irp = b._next_irp()
        b._pkt(irp, 0, 0x80, 2, setup, stage=0)
        b._pkt(irp, 1, 0x80, 2, synth.DEVICE_DESC, stage=1)
        b._pkt(irp, 1, 0x80, 2, synth.DEVICE_DESC, stage=3)
        synth.write_pcapng(self.path("dup.pcapng"), 249, b.packets)
        cap = Capture(self.path("dup.pcapng"))
        (t,) = cap.transfers
        self.assertEqual(t.data, synth.DEVICE_DESC)

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
                         [(0x81, True), (0x03, False), (0x82, True), (0x82, True)])
        self.assertEqual(intr[2].data, bytes([0x06, 0x00, 0x01]))
        self.assertEqual(intr[3].data, synth.VENDOR_KEYSTROKE)
        self.assertTrue(all(t.complete and t.status == 0 for t in cap.transfers))

    def test_keyboard_report_on_a_vendor_interface_counts_as_a_keystroke(self) -> None:
        # Interface 1 is not a boot keyboard, but its report descriptor declares
        # a keyboard report (id 0x04). That report may not leak by default,
        # while the other reports on the same endpoint must stay visible.
        cap = self.capture()
        vendor_in = [t for t in cap.transfers
                     if t.endpoint == 0x82 and t.xfer_type == XFER_INTERRUPT and t.data]
        by_id = {t.data[0]: t for t in vendor_in}
        self.assertIn(0x04, by_id, "synthetic session should contain the keyboard report")
        self.assertIn(0x06, by_id, "synthetic session should contain a vendor report")
        self.assertTrue(cap.is_keystroke(by_id[0x04]))
        self.assertFalse(cap.is_keystroke(by_id[0x06]))

    def test_vendor_keystroke_is_not_printed_without_the_flag(self) -> None:
        synth.write_pcapng(self.path("k.pcapng"), 249, build_session(0x11).packets)
        code, out = self.run_cli("dump", self.path("k.pcapng"), "-d", "1234:5678")
        self.assertEqual(code, 0)
        self.assertNotIn("04 00 00 04", out)
        code, shown = self.run_cli("dump", self.path("k.pcapng"), "-d", "1234:5678",
                                   "--show-keystrokes")
        self.assertEqual(code, 0)
        self.assertIn("04 00 00 04", shown)

    def _session_without_report_descriptor(self, descriptor: bytes | None) -> Capture:
        b = synth.UsbPcapBuilder()
        b.enumerate(report_descriptor=False)
        if descriptor is not None:
            b.control_in(synth.setup_bytes(0x81, 6, 0x2200, 1, len(synth.HID_REPORT_DESC)), descriptor)
        b.interrupt_in(0x82, bytes([0x06, 0x00, 0x01]))
        synth.write_pcapng(self.path("nd.pcapng"), 249, b.packets)
        return Capture(self.path("nd.pcapng"))

    def test_hid_interface_without_report_descriptor_fails_closed(self) -> None:
        # The interface is known to be HID, but the capture never saw its report
        # descriptor, so nothing rules out a keyboard report: hide it.
        cap = self._session_without_report_descriptor(None)
        (t,) = [t for t in cap.transfers if t.endpoint == 0x82 and t.data]
        self.assertTrue(cap.is_keystroke(t))

    def test_truncated_report_descriptor_fails_closed(self) -> None:
        # Cut inside the first item: the usage page lacks its second data byte.
        cap = self._session_without_report_descriptor(synth.HID_REPORT_DESC[:2])
        (t,) = [t for t in cap.transfers if t.endpoint == 0x82 and t.data]
        self.assertTrue(cap.is_keystroke(t))

    def test_fail_closed_output_says_why(self) -> None:
        self._session_without_report_descriptor(None)
        code, out = self.run_cli("dump", self.path("nd.pcapng"), "-d", "1234:5678", "-e", "0x82")
        self.assertEqual(code, 0)
        self.assertNotIn("06 00 01", out)
        self.assertIn("no HID report descriptor", out)
        code, shown = self.run_cli("dump", self.path("nd.pcapng"), "-d", "1234:5678", "-e", "0x82",
                                   "--show-keystrokes")
        self.assertIn("06 00 01", shown)

    def test_standard_requests_hidden_by_default(self) -> None:
        cap = self.capture()
        self.assertFalse(any(t.setup is not None and t.setup.is_standard for t in cap.select(None)))
        self.assertTrue(any(t.descriptor_type == 1 for t in cap.select(None, include_standard=True)))

    def test_vid_pid_selector(self) -> None:
        cap = self.capture()
        self.assertEqual(cap.resolve_device("1234:5678"), {"1.5"})
        with self.assertRaises(LookupError):
            cap.resolve_device("dead:beef")


class TimingTests(TempDirTest):
    """Interrupt IN requests are queued long before the device has data.

    The data reaches the host when the request completes, so that is the time a
    transfer must carry. Timestamping at submission puts every report next to
    the previous one, which made two taps three seconds apart look like a pair
    of duplicates in a real capture.
    """

    def test_usbpcap_in_transfer_carries_its_completion_time(self) -> None:
        b = synth.UsbPcapBuilder()
        b.enumerate()
        irp = b._next_irp()
        b._pkt(irp, 0, 0x82, 1, b"")              # read queued now ...
        queued_at = b.t
        b.t += 3.0
        b.interrupt_out(0x03, bytes(32))          # ... something else happens ...
        b._pkt(irp, 1, 0x82, 1, bytes([0x06, 0x00, 0x01]))  # ... data arrives 3 s later
        arrived_at = b.t
        synth.write_pcapng(self.path("t.pcapng"), 249, b.packets)
        cap = Capture(self.path("t.pcapng"))
        intr = [t for t in cap.transfers if t.xfer_type == XFER_INTERRUPT]
        self.assertEqual([t.endpoint for t in intr], [0x03, 0x82])
        self.assertAlmostEqual(intr[1].ts, arrived_at, places=4)
        self.assertAlmostEqual(intr[1].submitted, queued_at, places=4)
        self.assertEqual([t.ts for t in cap.transfers], sorted(t.ts for t in cap.transfers))

    def test_usbmon_in_transfer_carries_its_completion_time(self) -> None:
        setup = synth.setup_bytes(0x21, 0x09, 0x0305, 1, len(FEATURE))
        packets = [
            (1.0, synth.usbmon_packet(7, "S", 1, 0x82, 7, 3, 1.0, None, b"")),
            (2.0, synth.usbmon_packet(8, "S", 2, 0x00, 7, 3, 2.0, setup, FEATURE)),
            (2.1, synth.usbmon_packet(8, "C", 2, 0x00, 7, 3, 2.1, None, b"")),
            (4.0, synth.usbmon_packet(7, "C", 1, 0x82, 7, 3, 4.0, None, bytes([0x06, 0x00, 0x01]))),
        ]
        synth.write_pcapng(self.path("m.pcapng"), 220, packets)
        cap = Capture(self.path("m.pcapng"))
        ctrl, intr = cap.transfers
        self.assertEqual((ctrl.xfer_type, intr.xfer_type), (XFER_CONTROL, XFER_INTERRUPT))
        self.assertAlmostEqual(intr.ts, 4.0, places=4)
        self.assertAlmostEqual(intr.submitted, 1.0, places=4)


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
    def test_report_with_mixed_pages_records_every_page(self) -> None:
        # Report 7 starts on a vendor page and only later switches to the
        # keyboard page. Recording just the first page would let it leak.
        desc = bytes([
            0x06, 0x00, 0xFF, 0x09, 0x01, 0xA1, 0x01,
            0x85, 0x07, 0x75, 0x08, 0x95, 0x02, 0x09, 0x02, 0x81, 0x02,   # vendor input
            0x05, 0x07, 0x19, 0x00, 0x29, 0xFF, 0x95, 0x06, 0x81, 0x00,   # keyboard input
            0xC0,
        ])
        (r,) = summarise_hid_report_descriptor(desc)
        self.assertEqual(r.report_id, 7)
        self.assertEqual(r.usage_pages, {0xFF00, 0x07})

    def test_extended_usage_carries_its_own_page(self) -> None:
        # Four-byte Usage Minimum/Maximum put the page in the high 16 bits, so
        # the keyboard page appears without any Usage Page item for it.
        desc = bytes([
            0x06, 0x00, 0xFF, 0x09, 0x01, 0xA1, 0x01,
            0x85, 0x08, 0x75, 0x08, 0x95, 0x06,
            0x1B, 0x00, 0x00, 0x07, 0x00,      # Usage Minimum 0x0007:0000
            0x2B, 0xFF, 0x00, 0x07, 0x00,      # Usage Maximum 0x0007:00FF
            0x81, 0x00,
            0xC0,
        ])
        (r,) = summarise_hid_report_descriptor(desc)
        self.assertIn(0x07, r.usage_pages)

    def test_report_descriptor_completeness(self) -> None:
        self.assertTrue(report_descriptor_complete(synth.HID_REPORT_DESC))
        self.assertFalse(report_descriptor_complete(synth.HID_REPORT_DESC[:2]))

    def test_hid_report_summary(self) -> None:
        reports = summarise_hid_report_descriptor(synth.HID_REPORT_DESC)
        self.assertEqual([(r.report_id, r.usage_page, r.feature_bits, r.output_bits) for r in reports],
                         [(4, 0x07, 0, 0), (5, 0xFF00, 512, 0), (6, 0xFF00, 0, 256)])


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
