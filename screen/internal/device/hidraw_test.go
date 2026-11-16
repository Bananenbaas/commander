package device

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParseHIDID(t *testing.T) {
	uevent := []byte("DRIVER=hid-generic\nHID_ID=0003:000010F5:00005038\nHID_NAME=Keyboard\n")
	bus, vendor, product, ok := parseHIDID(uevent)
	if !ok || bus != 0x0003 || vendor != 0x10f5 || product != 0x5038 {
		t.Fatalf("parseHIDID = %#x %#x %#x %v", bus, vendor, product, ok)
	}
	for _, bad := range []string{
		"DRIVER=hid-generic\n",
		"HID_ID=0003:000010F5\n",
		"HID_ID=0003:000010F5:0000503G\n",
		"",
	} {
		if _, _, _, ok := parseHIDID([]byte(bad)); ok {
			t.Errorf("parseHIDID(%q) accepted", bad)
		}
	}
}

func TestInterfaceFromName(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		ok   bool
	}{
		{"1-2:1.2", 2, true},
		{"3-1.4.2:1.0", 0, true},
		{"1-10:1.1", 1, true},
		{"0003:10F5:5038.0007", 0, false},
		{"1-2", 0, false},
		{"usb1", 0, false},
		{"hidraw3", 0, false},
	} {
		n, ok := interfaceFromName(tc.name)
		if n != tc.n || ok != tc.ok {
			t.Errorf("interfaceFromName(%q) = %d, %v; want %d, %v", tc.name, n, ok, tc.n, tc.ok)
		}
	}
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The kernel names USB interface directories like "1-2:1.2" and HID
// device directories like "0003:10F5:5038.0007". Colons cannot appear in
// file names on Windows, so the trees below use stand-in names and carry a
// bInterfaceNumber file, except in the test of the name fallback.

func TestUSBInterface(t *testing.T) {
	root := t.TempDir()

	// The kernel writes bInterfaceNumber in hex.
	intf := mkdir(t, root, "usb1", "1-2", "intf")
	writeFile(t, filepath.Join(intf, "bInterfaceNumber"), "0a\n")
	if n, err := usbInterface(mkdir(t, intf, "hid")); err != nil || n != 10 {
		t.Fatalf("with bInterfaceNumber: %d, %v; want 10", n, err)
	}

	bad := mkdir(t, root, "usb3", "3-1", "intf")
	writeFile(t, filepath.Join(bad, "bInterfaceNumber"), "zz\n")
	if _, err := usbInterface(mkdir(t, bad, "hid")); err == nil {
		t.Fatal("bad bInterfaceNumber accepted")
	}

	if _, err := usbInterface(mkdir(t, root, "virtual", "hid")); err == nil {
		t.Fatal("found an interface where there is none")
	}
}

func TestUSBInterfaceByName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("USB interface names contain colons, which Windows file names cannot")
	}
	root := t.TempDir()
	// The bInterfaceNumber file wins over the name.
	intf := mkdir(t, root, "usb1", "1-2", "1-2:1.3")
	writeFile(t, filepath.Join(intf, "bInterfaceNumber"), "01\n")
	if n, err := usbInterface(mkdir(t, intf, "0003:10F5:5038.0007")); err != nil || n != 1 {
		t.Fatalf("with bInterfaceNumber: %d, %v; want 1", n, err)
	}
	byName := mkdir(t, root, "usb2", "2-1", "2-1:1.2", "0003:10F5:5038.0008")
	if n, err := usbInterface(byName); err != nil || n != 2 {
		t.Fatalf("by name: %d, %v; want 2", n, err)
	}
}

// sysfs builds a hidraw class directory and the device tree its "device"
// links point to, and returns the class directory and a resolver for the
// links, so the test needs no symlinks.
func sysfs(t *testing.T) (string, func(string) (string, error)) {
	t.Helper()
	root := t.TempDir()
	class := mkdir(t, root, "class", "hidraw")
	links := map[string]string{}
	node := func(name, port string, iface int, hidID string) {
		intf := mkdir(t, root, "devices", "usb1", port, fmt.Sprintf("intf%d", iface))
		writeFile(t, filepath.Join(intf, "bInterfaceNumber"), fmt.Sprintf("%02x\n", iface))
		dev := mkdir(t, intf, "hid")
		writeFile(t, filepath.Join(dev, "uevent"), "DRIVER=hid-generic\nHID_ID="+hidID+"\n")
		mkdir(t, class, name)
		links[filepath.Join(class, name, "device")] = dev
	}
	node("hidraw0", "1-2", 0, "0003:000010F5:00005038")
	node("hidraw1", "1-2", 1, "0003:000010F5:00005038")
	node("hidraw2", "1-2", 2, "0003:000010F5:00005038")
	node("hidraw3", "1-2", 3, "0003:000010F5:00005038")
	node("hidraw4", "1-3", 0, "0003:00001234:00005678") // another device
	node("hidraw5", "1-4", 1, "0005:000010F5:00005038") // not USB
	mkdir(t, class, "hidraw6")                          // no device link
	resolve := func(p string) (string, error) {
		if d, ok := links[p]; ok {
			return d, nil
		}
		return "", os.ErrNotExist
	}
	return class, resolve
}

func TestFindHidraw(t *testing.T) {
	class, resolve := sysfs(t)
	nodes, err := findHidraw(class, resolve)
	if err != nil {
		t.Fatal(err)
	}
	want := []hidrawNode{{"hidraw0", 0}, {"hidraw1", 1}, {"hidraw2", 2}, {"hidraw3", 3}}
	if len(nodes) != len(want) {
		t.Fatalf("nodes = %v, want %v", nodes, want)
	}
	for i := range want {
		if nodes[i] != want[i] {
			t.Fatalf("nodes = %v, want %v", nodes, want)
		}
	}
	stream, control, err := pickNodes(nodes)
	if err != nil || stream != "/dev/hidraw1" || control != "/dev/hidraw2" {
		t.Fatalf("pickNodes = %q, %q, %v", stream, control, err)
	}
}

func TestFindHidrawNoClass(t *testing.T) {
	nodes, err := findHidraw(filepath.Join(t.TempDir(), "missing"), filepath.EvalSymlinks)
	if err != nil || nodes != nil {
		t.Fatalf("findHidraw on a missing class = %v, %v", nodes, err)
	}
}

func TestPickNodes(t *testing.T) {
	if _, _, err := pickNodes(nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("none: %v, want ErrNotFound", err)
	}
	if _, _, err := pickNodes([]hidrawNode{{"hidraw1", 1}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no control node: %v, want ErrNotFound", err)
	}
	if _, _, err := pickNodes([]hidrawNode{{"hidraw2", 2}, {"hidraw0", 0}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no stream node: %v, want ErrNotFound", err)
	}
	two := []hidrawNode{{"hidraw1", 1}, {"hidraw2", 2}, {"hidraw5", 1}, {"hidraw6", 2}}
	if _, _, err := pickNodes(two); !errors.Is(err, ErrNotFound) || !errors.Is(err, errMultiple) {
		t.Fatalf("two keyboards: %v, want ErrNotFound for more than one KB7", err)
	}
}

func TestHidioc(t *testing.T) {
	// _IOC(_IOC_WRITE|_IOC_READ, 'H', nr, len), worked out by hand.
	for _, tc := range []struct {
		nr   byte
		n    int
		want uint32
	}{
		{hidiocSFeature, 5, 0xC0054806},    // 0e 05 01 00 00
		{hidiocSFeature, 12, 0xC00C4806},   // 0x35
		{hidiocSFeature, 584, 0xC2484806},  // 0x29
		{hidiocGFeature, 4, 0xC0044807},    // 0x05
		{hidiocGFeature, 584, 0xC2484807},  // 0x29
		{hidiocGFeature, 1044, 0xC4144807}, // the whole collection
		{hidiocGFeature, maxIoctlLen, 0xDFFF4807},
	} {
		if got := hidioc(tc.nr, tc.n); got != tc.want {
			t.Errorf("hidioc(%#x, %d) = %#x, want %#x", tc.nr, tc.n, got, tc.want)
		}
	}
}
