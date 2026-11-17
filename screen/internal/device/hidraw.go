// SPDX-License-Identifier: GPL-2.0-or-later

package device

// Helpers for the Linux hidraw implementation that need no Linux kernel:
// finding the KB7's hidraw nodes in sysfs and building ioctl request
// numbers. They build on every system so their tests run everywhere.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// USB interfaces of the KB7 that this package opens on Linux.
const (
	streamInterface  = 1 // the stream
	controlInterface = 2 // feature, notification and touch reports
)

const busUSB = 0x0003

// hidrawNode is a hidraw node of a KB7 and the USB interface it belongs to.
type hidrawNode struct {
	Name      string // e.g. "hidraw3"; the device file is /dev/<Name>
	Interface int
}

// findHidraw lists the hidraw nodes in classDir (normally
// /sys/class/hidraw) that belong to a KB7. resolve follows a sysfs symlink
// (filepath.EvalSymlinks); each node's "device" link leads to its HID
// device directory, whose uevent names the vendor and product and whose
// ancestors include the USB interface.
func findHidraw(classDir string, resolve func(string) (string, error)) ([]hidrawNode, error) {
	entries, err := os.ReadDir(classDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("device: %w", err)
	}
	var nodes []hidrawNode
	for _, e := range entries {
		dev, err := resolve(filepath.Join(classDir, e.Name(), "device"))
		if err != nil {
			continue
		}
		uevent, err := os.ReadFile(filepath.Join(dev, "uevent"))
		if err != nil {
			continue
		}
		bus, vendor, product, ok := parseHIDID(uevent)
		if !ok || bus != busUSB || vendor != VendorID || product != ProductID {
			continue
		}
		iface, err := usbInterface(dev)
		if err != nil {
			return nil, fmt.Errorf("device: %s: %w", e.Name(), err)
		}
		nodes = append(nodes, hidrawNode{Name: e.Name(), Interface: iface})
	}
	return nodes, nil
}

// pickNodes returns the device files of the stream node (interface 1) and
// the control node (interface 2).
func pickNodes(nodes []hidrawNode) (stream, control string, err error) {
	for _, n := range nodes {
		var p *string
		switch n.Interface {
		case streamInterface:
			p = &stream
		case controlInterface:
			p = &control
		default:
			continue
		}
		if *p != "" {
			return "", "", errMultiple
		}
		*p = "/dev/" + n.Name
	}
	switch {
	case stream == "" && control == "":
		return "", "", ErrNotFound
	case stream == "":
		return "", "", fmt.Errorf("%w: no hidraw node for interface %d", ErrNotFound, streamInterface)
	case control == "":
		return "", "", fmt.Errorf("%w: no hidraw node for interface %d", ErrNotFound, controlInterface)
	}
	return stream, control, nil
}

// parseHIDID reads the HID_ID line of a HID device's uevent file, for
// example "HID_ID=0003:000010F5:00005038": bus, vendor and product in hex.
func parseHIDID(uevent []byte) (bus, vendor, product uint32, ok bool) {
	for line := range bytes.Lines(uevent) {
		v, found := strings.CutPrefix(strings.TrimSpace(string(line)), "HID_ID=")
		if !found {
			continue
		}
		parts := strings.Split(v, ":")
		if len(parts) != 3 {
			return 0, 0, 0, false
		}
		var n [3]uint32
		for i, p := range parts {
			x, err := strconv.ParseUint(p, 16, 32)
			if err != nil {
				return 0, 0, 0, false
			}
			n[i] = uint32(x)
		}
		return n[0], n[1], n[2], true
	}
	return 0, 0, 0, false
}

// usbInterface returns the number of the USB interface a HID device
// directory belongs to: the bInterfaceNumber of the nearest ancestor that
// has one (written in hex by the kernel), or else the interface part of the
// nearest ancestor named like a USB interface ("1-2:1.2").
func usbInterface(dir string) (int, error) {
	for d := dir; ; {
		if b, err := os.ReadFile(filepath.Join(d, "bInterfaceNumber")); err == nil {
			n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 16, 8)
			if err != nil {
				return 0, fmt.Errorf("bad bInterfaceNumber %q in %s", bytes.TrimSpace(b), d)
			}
			return int(n), nil
		}
		if n, ok := interfaceFromName(filepath.Base(d)); ok {
			return n, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			return 0, fmt.Errorf("no USB interface above %s", dir)
		}
		d = parent
	}
}

// usbInterfaceName matches the kernel's name for a USB interface,
// "<bus>-<port path>:<configuration>.<interface>", for example "1-2:1.2" or
// "3-1.4:1.0".
var usbInterfaceName = regexp.MustCompile(`^\d+-\d+(?:\.\d+)*:\d+\.(\d+)$`)

// interfaceFromName returns the interface number in a USB interface's
// sysfs name.
func interfaceFromName(name string) (int, bool) {
	m := usbInterfaceName.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n > 255 {
		return 0, false
	}
	return n, true
}

// Numbers of the hidraw feature report ioctls.
const (
	hidiocSFeature = 0x06 // HIDIOCSFEATURE(len): send a feature report
	hidiocGFeature = 0x07 // HIDIOCGFEATURE(len): get a feature report
)

// maxIoctlLen is the largest length an ioctl request number can carry on
// every Linux architecture: 13 bits on mips, powerpc and sparc, 14 bits on
// the others.
const maxIoctlLen = 1<<13 - 1

// hidioc returns the request number of HIDIOCSFEATURE(n) or
// HIDIOCGFEATURE(n): _IOC(_IOC_WRITE|_IOC_READ, 'H', nr, n). The direction
// bits come out as 0xC0000000 on every architecture (bits 30-31 set where
// _IOC_READ=2, _IOC_WRITE=1 and the direction starts at bit 30; bits 30-31
// set where _IOC_READ=2, _IOC_WRITE=4 and it starts at bit 29), and the
// length starts at bit 16 everywhere. n must be at most maxIoctlLen.
func hidioc(nr byte, n int) uint32 {
	return 0xC0000000 | uint32(n)<<16 | uint32('H')<<8 | uint32(nr)
}
