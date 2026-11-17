// SPDX-License-Identifier: GPL-2.0-or-later

package icon

import (
	"encoding/binary"
	"errors"
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	langUS      = 0x0409
	subdirFlag  = 0x80000000
)

var le = binary.LittleEndian

type iconEntry struct {
	width, height, colours, reserved byte
	planes, bits                     uint16
	data                             []byte
}

func parseICO(ico []byte) ([]iconEntry, error) {
	if len(ico) < 6 || le.Uint16(ico[0:]) != 0 || le.Uint16(ico[2:]) != 1 {
		return nil, errors.New("icon: not an ICO file")
	}
	n := int(le.Uint16(ico[4:]))
	if n == 0 || len(ico) < 6+16*n {
		return nil, errors.New("icon: truncated ICO directory")
	}
	var out []iconEntry
	for i := 0; i < n; i++ {
		e := ico[6+16*i:]
		size, off := int(le.Uint32(e[8:])), int(le.Uint32(e[12:]))
		if off < 0 || size < 0 || off+size > len(ico) {
			return nil, errors.New("icon: ICO entry outside the file")
		}
		out = append(out, iconEntry{e[0], e[1], e[2], e[3], le.Uint16(e[4:]), le.Uint16(e[6:]), ico[off : off+size]})
	}
	return out, nil
}

// Syso converts an ICO file to a COFF object for amd64 whose .rsrc section
// holds one RT_GROUP_ICON (ID 1, language 0x0409) and the RT_ICON images
// (IDs 1..n). The Go linker includes such a .syso file in a Windows build.
func Syso(ico []byte) ([]byte, error) {
	icons, err := parseICO(ico)
	if err != nil {
		return nil, err
	}
	n := len(icons)

	// GRPICONDIR
	grp := make([]byte, 6+14*n)
	le.PutUint16(grp[2:], 1)
	le.PutUint16(grp[4:], uint16(n))
	for i, ic := range icons {
		e := grp[6+14*i:]
		e[0], e[1], e[2], e[3] = ic.width, ic.height, ic.colours, ic.reserved
		le.PutUint16(e[4:], ic.planes)
		le.PutUint16(e[6:], ic.bits)
		le.PutUint32(e[8:], uint32(len(ic.data)))
		le.PutUint16(e[12:], uint16(i+1))
	}

	// Layout: root, the RT_ICON directory, the RT_GROUP_ICON directory, one
	// language directory per resource, the data entries, then the data.
	dirSize := func(entries int) int { return 16 + 8*entries }
	iconDirOff := dirSize(2)
	grpDirOff := iconDirOff + dirSize(n)
	langOff := grpDirOff + dirSize(1)
	dataEntOff := langOff + (n+1)*dirSize(1)
	dataOff := dataEntOff + (n+1)*16

	var sec []byte
	put32 := func(v uint32) { sec = le.AppendUint32(sec, v) }
	put16 := func(v uint16) { sec = le.AppendUint16(sec, v) }
	dirHeader := func(ids int) {
		put32(0) // characteristics
		put32(0) // time stamp
		put16(0) // major version
		put16(0) // minor version
		put16(0) // named entries
		put16(uint16(ids))
	}
	entry := func(id, target uint32) { put32(id); put32(target) }

	dirHeader(2)
	entry(rtIcon, subdirFlag|uint32(iconDirOff))
	entry(rtGroupIcon, subdirFlag|uint32(grpDirOff))
	dirHeader(n)
	for i := 0; i < n; i++ {
		entry(uint32(i+1), subdirFlag|uint32(langOff+i*dirSize(1)))
	}
	dirHeader(1)
	entry(1, subdirFlag|uint32(langOff+n*dirSize(1)))
	for i := 0; i <= n; i++ {
		dirHeader(1)
		entry(langUS, uint32(dataEntOff+i*16))
	}

	// Data entries; their RVA fields need relocations.
	var relocs []uint32
	datas := make([][]byte, 0, n+1)
	for _, ic := range icons {
		datas = append(datas, ic.data)
	}
	datas = append(datas, grp)
	off := dataOff
	for _, d := range datas {
		relocs = append(relocs, uint32(len(sec)))
		put32(uint32(off))
		put32(uint32(len(d)))
		put32(0) // code page
		put32(0) // reserved
		off += (len(d) + 3) &^ 3
	}
	for _, d := range datas {
		sec = append(sec, d...)
		for len(sec)%4 != 0 {
			sec = append(sec, 0)
		}
	}

	const (
		fileHdr   = 20
		secHdr    = 40
		relocSize = 10
	)
	rawOff := fileHdr + secHdr
	relOff := rawOff + len(sec)
	symOff := relOff + relocSize*len(relocs)
	name := []byte{'.', 'r', 's', 'r', 'c', 0, 0, 0}

	var out []byte
	a16 := func(v uint16) { out = le.AppendUint16(out, v) }
	a32 := func(v uint32) { out = le.AppendUint32(out, v) }
	// COFF file header
	a16(0x8664)
	a16(1)
	a32(0)
	a32(uint32(symOff))
	a32(2) // the section symbol and its auxiliary record
	a16(0)
	a16(0)
	// section header
	out = append(out, name...)
	a32(0) // virtual size
	a32(0) // virtual address
	a32(uint32(len(sec)))
	a32(uint32(rawOff))
	a32(uint32(relOff))
	a32(0)
	a16(uint16(len(relocs)))
	a16(0)
	a32(0xC0300040) // initialized data, align 4, read, write
	out = append(out, sec...)
	for _, r := range relocs {
		a32(r)
		a32(0) // symbol index of .rsrc
		a16(3) // IMAGE_REL_AMD64_ADDR32NB
	}
	// section symbol with its auxiliary record
	out = append(out, name...)
	a32(0)
	a16(1)
	a16(0)
	out = append(out, 3, 1) // static, one auxiliary record
	a32(uint32(len(sec)))
	a16(uint16(len(relocs)))
	a16(0)
	a32(0)
	a16(0)
	out = append(out, 0, 0, 0, 0) // selection and padding
	a32(4)                        // empty string table
	return out, nil
}
