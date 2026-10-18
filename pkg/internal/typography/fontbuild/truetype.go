package fontbuild

import (
	"bytes"
	"encoding/binary"
	"slices"
	"unicode/utf16"
)

// Glyph is one glyph of a built program.
type Glyph struct {
	// Name is the PostScript glyph name the program's post table (TrueType)
	// or charset (CFF) gives the glyph.
	Name string
	// Unicode is the code point the TrueType cmap maps to the glyph, 0 for
	// none.
	Unicode rune
	// Symbol, when non-zero, is the code the TrueType (3,0) symbol cmap maps
	// to the glyph, already offset into the 0xF000 range.
	Symbol uint16
	// Mac, when non-zero, is the code the TrueType (1,0) Macintosh cmap maps
	// to the glyph.
	Mac byte
	// Advance is the glyph's advance width in font units.
	Advance uint16
	// Empty leaves the glyph without an outline, as a space is.
	Empty bool
	// Components, when set, makes the glyph a TrueType composite glyph
	// drawing each listed GID at the origin (OpenType specification, glyf
	// table, composite glyph description). CFF programs ignore it.
	Components []uint16
	// Seac, when set, makes a CFF glyph an accented character built from
	// the two glyphs the Standard encoding codes name, base then accent
	// (Type 2 charstring endchar with four arguments). TrueType programs
	// ignore it.
	Seac *[2]byte
}

// TrueType describes a TrueType program with glyf outlines.
type TrueType struct {
	PostScriptName string
	Family         string
	Subfamily      string
	// UnitsPerEm defaults to 1000.
	UnitsPerEm uint16
	// FSType is the OS/2 embedding permission field.
	FSType uint16
	// NoOS2 leaves the OS/2 table out.
	NoOS2 bool
	// Glyphs follow the .notdef glyph, which the program always holds at
	// GID 0 with the advance of NotdefAdvance.
	Glyphs        []Glyph
	NotdefAdvance uint16
}

// Build returns the program's bytes.
func (t TrueType) Build() []byte {
	upem := t.UnitsPerEm
	if upem == 0 {
		upem = 1000
	}
	glyphs := append([]Glyph{{Name: ".notdef", Advance: t.NotdefAdvance}}, t.Glyphs...)
	n := len(glyphs)

	var glyf bytes.Buffer
	loca := make([]uint32, 0, n+1)
	for _, g := range glyphs {
		loca = append(loca, uint32(glyf.Len())) //nolint:gosec // G115: test programs stay far below 4 GiB.
		switch {
		case len(g.Components) > 0:
			glyf.Write(compositeGlyph(g.Components))
		case !g.Empty:
			glyf.Write(rectangleGlyph(int16(g.Advance))) //nolint:gosec // G115: test advances stay below 32768.
		}
	}
	loca = append(loca, uint32(glyf.Len())) //nolint:gosec // G115: test programs stay far below 4 GiB.
	var locaTable bytes.Buffer
	for _, off := range loca {
		be32(&locaTable, off)
	}

	var hmtx bytes.Buffer
	for _, g := range glyphs {
		be16(&hmtx, g.Advance)
		be16(&hmtx, 0)
	}

	var head bytes.Buffer
	be32(&head, 0x00010000) // version
	be32(&head, 0x00010000) // fontRevision
	be32(&head, 0)          // checkSumAdjustment
	be32(&head, 0x5F0F3CF5) // magicNumber
	be16(&head, 0x000B)     // flags
	be16(&head, upem)
	head.Write(make([]byte, 16)) // created, modified
	be16(&head, 0)               // xMin
	be16(&head, 0)               // yMin
	be16(&head, upem)            // xMax
	be16(&head, upem*7/10)       // yMax
	be16(&head, 0)               // macStyle
	be16(&head, 8)               // lowestRecPPEM
	be16(&head, 2)               // fontDirectionHint
	be16(&head, 1)               // indexToLocFormat: long
	be16(&head, 0)               // glyphDataFormat

	var hhea bytes.Buffer
	be32(&hhea, 0x00010000)
	be16(&hhea, upem*8/10)              // ascender
	be16(&hhea, uint16(-int16(upem/5))) //nolint:gosec // G115: descender is a negative int16 stored in two bytes.
	be16(&hhea, 0)                      // lineGap
	be16(&hhea, maxAdvance(glyphs))
	hhea.Write(make([]byte, 22))
	be16(&hhea, uint16(n)) //nolint:gosec // G115: test programs hold few glyphs.

	var maxp bytes.Buffer
	be32(&maxp, 0x00010000)
	be16(&maxp, uint16(n)) //nolint:gosec // G115: test programs hold few glyphs.
	be16(&maxp, 4)         // maxPoints
	be16(&maxp, 1)         // maxContours
	maxp.Write(make([]byte, 22))

	tables := map[string][]byte{
		"cmap": t.cmap(glyphs),
		"glyf": glyf.Bytes(),
		"head": head.Bytes(),
		"hhea": hhea.Bytes(),
		"hmtx": hmtx.Bytes(),
		"loca": locaTable.Bytes(),
		"maxp": maxp.Bytes(),
		"name": nameTable(t.Family, t.Subfamily, t.PostScriptName),
		"post": postTable(glyphs),
	}
	if !t.NoOS2 {
		tables["OS/2"] = os2Table(t.FSType, upem)
	}
	return sfnt(0x00010000, tables)
}

// rectangleGlyph returns a simple glyph drawing one rectangle as wide as the
// advance.
func rectangleGlyph(width int16) []byte {
	var b bytes.Buffer
	be16(&b, 1)                             // numberOfContours
	be16(&b, 0)                             // xMin
	be16(&b, 0)                             // yMin
	be16(&b, uint16(max(width, 1)))         //nolint:gosec // G115: width is positive here.
	be16(&b, 500)                           // yMax
	be16(&b, 3)                             // endPtsOfContours[0]
	be16(&b, 0)                             // instructionLength
	b.Write([]byte{0x01, 0x01, 0x01, 0x01}) // on-curve flags, 16-bit deltas
	for _, dx := range []int16{0, max(width, 1), 0, -max(width, 1)} {
		be16(&b, uint16(dx)) //nolint:gosec // G115: a two's-complement delta.
	}
	for _, dy := range []int16{0, 0, 500, 0} {
		be16(&b, uint16(dy)) //nolint:gosec // G115: a two's-complement delta.
	}
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
	return b.Bytes()
}

// compositeGlyph returns a composite glyph drawing each of gids at the
// origin, with word arguments and no transform.
func compositeGlyph(gids []uint16) []byte {
	var b bytes.Buffer
	be16(&b, 0xFFFF) // numberOfContours -1
	b.Write(make([]byte, 8))
	for i, gid := range gids {
		flags := uint16(0x0001 | 0x0002) // ARG_1_AND_2_ARE_WORDS, ARGS_ARE_XY_VALUES
		if i < len(gids)-1 {
			flags |= 0x0020 // MORE_COMPONENTS
		}
		be16(&b, flags)
		be16(&b, gid)
		be32(&b, 0)
	}
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
	return b.Bytes()
}

// cmap returns a cmap table with a (3,1) format 4 subtable over the glyphs'
// code points, when any glyph has one a (3,0) format 4 subtable over their
// symbol codes, and when any glyph has one a (1,0) format 0 subtable over their
// Macintosh codes.
func (TrueType) cmap(glyphs []Glyph) []byte {
	uni := make(map[uint16]uint16)
	sym := make(map[uint16]uint16)
	var mac []byte
	for gid, g := range glyphs {
		if g.Mac != 0 {
			if mac == nil {
				mac = make([]byte, 256)
			}
			mac[g.Mac] = byte(gid) //nolint:gosec // G115: test programs hold few glyphs.
		}
		if g.Unicode > 0 && g.Unicode <= 0xFFFF {
			uni[uint16(g.Unicode)] = uint16(gid) //nolint:gosec // G115: bounded to the BMP and to few glyphs.
		}
		if g.Symbol != 0 {
			sym[g.Symbol] = uint16(gid) //nolint:gosec // G115: test programs hold few glyphs.
		}
	}
	type sub struct {
		platform uint16
		encoding uint16
		data     []byte
	}
	var subs []sub
	if mac != nil {
		subs = append(subs, sub{1, 0, format0(mac)})
	}
	if len(sym) > 0 {
		subs = append(subs, sub{3, 0, format4(sym)})
	}
	if len(uni) > 0 || len(sym) == 0 {
		subs = append(subs, sub{3, 1, format4(uni)})
	}
	var b bytes.Buffer
	be16(&b, 0)
	be16(&b, uint16(len(subs))) //nolint:gosec // G115: at most three subtables.
	offset := 4 + 8*len(subs)
	for _, s := range subs {
		be16(&b, s.platform)
		be16(&b, s.encoding)
		be32(&b, uint32(offset)) //nolint:gosec // G115: test tables stay small.
		offset += len(s.data)
	}
	for _, s := range subs {
		b.Write(s.data)
	}
	return b.Bytes()
}

// format0 returns a format 0 cmap subtable over the 256 glyph IDs of gids.
func format0(gids []byte) []byte {
	var b bytes.Buffer
	be16(&b, 0)
	be16(&b, uint16(6+len(gids))) //nolint:gosec // G115: 262 bytes.
	be16(&b, 0)
	b.Write(gids)
	return b.Bytes()
}

// format4 returns a format 4 cmap subtable with one segment per code.
func format4(m map[uint16]uint16) []byte {
	codes := make([]uint16, 0, len(m)+1)
	for c := range m {
		if c != 0xFFFF {
			codes = append(codes, c)
		}
	}
	slices.Sort(codes)
	codes = append(codes, 0xFFFF)
	seg := len(codes)
	var b bytes.Buffer
	be16(&b, 4)
	be16(&b, uint16(16+8*seg)) //nolint:gosec // G115: test tables stay small.
	be16(&b, 0)
	be16(&b, uint16(2*seg)) //nolint:gosec // G115: test tables stay small.
	searchRange, selector := 1, 0
	for searchRange*2 <= seg {
		searchRange *= 2
		selector++
	}
	be16(&b, uint16(2*searchRange))       //nolint:gosec // G115: test tables stay small.
	be16(&b, uint16(selector))            //nolint:gosec // G115: test tables stay small.
	be16(&b, uint16(2*seg-2*searchRange)) //nolint:gosec // G115: test tables stay small.
	for _, c := range codes {
		be16(&b, c)
	}
	be16(&b, 0)
	for _, c := range codes {
		be16(&b, c)
	}
	for _, c := range codes {
		if c == 0xFFFF {
			be16(&b, 1)
			continue
		}
		be16(&b, m[c]-c)
	}
	for range codes {
		be16(&b, 0)
	}
	return b.Bytes()
}

// nameTable returns a name table holding the family, subfamily, full and
// PostScript names as Windows Unicode English records.
func nameTable(family, subfamily, psName string) []byte {
	records := []struct {
		id uint16
		s  string
	}{{1, family}, {2, subfamily}, {4, family + " " + subfamily}, {6, psName}}
	var storage bytes.Buffer
	var b bytes.Buffer
	be16(&b, 0)
	be16(&b, uint16(len(records)))      //nolint:gosec // G115: four records.
	be16(&b, uint16(6+12*len(records))) //nolint:gosec // G115: four records.
	for _, r := range records {
		units := utf16.Encode([]rune(r.s))
		be16(&b, 3)
		be16(&b, 1)
		be16(&b, 0x0409)
		be16(&b, r.id)
		be16(&b, uint16(2*len(units)))  //nolint:gosec // G115: test names are short.
		be16(&b, uint16(storage.Len())) //nolint:gosec // G115: test names are short.
		for _, u := range units {
			be16(&storage, u)
		}
	}
	b.Write(storage.Bytes())
	return b.Bytes()
}

// postTable returns a format 2 post table naming every glyph.
func postTable(glyphs []Glyph) []byte {
	var b bytes.Buffer
	be32(&b, 0x00020000)
	b.Write(make([]byte, 28))
	be16(&b, uint16(len(glyphs))) //nolint:gosec // G115: test programs hold few glyphs.
	var names bytes.Buffer
	for i, g := range glyphs {
		if i == 0 {
			be16(&b, 0) // .notdef is the first standard Macintosh name
			continue
		}
		be16(&b, uint16(258+i-1))          //nolint:gosec // G115: test programs hold few glyphs.
		names.WriteByte(byte(len(g.Name))) //nolint:gosec // G115: a post table glyph name is a Pascal string of at most 255 bytes, and test names are short.
		names.WriteString(g.Name)
	}
	b.Write(names.Bytes())
	return b.Bytes()
}

// os2Table returns a version 4 OS/2 table.
func os2Table(fsType, upem uint16) []byte {
	b := make([]byte, 96)
	binary.BigEndian.PutUint16(b[0:], 4)
	binary.BigEndian.PutUint16(b[2:], upem/2) // xAvgCharWidth
	binary.BigEndian.PutUint16(b[4:], 400)    // usWeightClass
	binary.BigEndian.PutUint16(b[6:], 5)      // usWidthClass
	binary.BigEndian.PutUint16(b[8:], fsType)
	copy(b[58:62], "TEST")                                     // achVendID
	binary.BigEndian.PutUint16(b[62:], 0x0040)                 // fsSelection: regular
	binary.BigEndian.PutUint16(b[68:], upem*8/10)              // sTypoAscender
	binary.BigEndian.PutUint16(b[70:], uint16(-int16(upem/5))) //nolint:gosec // G115: a negative int16 stored in two bytes.
	binary.BigEndian.PutUint16(b[88:], upem*7/10)              // sCapHeight
	return b
}

// sfnt assembles tables under an sfnt header, in tag order.
func sfnt(version uint32, tables map[string][]byte) []byte {
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	var b bytes.Buffer
	be32(&b, version)
	be16(&b, uint16(len(tags))) //nolint:gosec // G115: few tables.
	b.Write(make([]byte, 6))
	offset := 12 + 16*len(tags)
	var body bytes.Buffer
	for _, tag := range tags {
		data := tables[tag]
		b.WriteString(tag)
		be32(&b, checksum(data))
		be32(&b, uint32(offset+body.Len())) //nolint:gosec // G115: test programs stay small.
		be32(&b, uint32(len(data)))         //nolint:gosec // G115: test programs stay small.
		body.Write(data)
		for body.Len()%4 != 0 {
			body.WriteByte(0)
		}
	}
	b.Write(body.Bytes())
	return b.Bytes()
}

// checksum is the sfnt table checksum (OpenType specification, table
// directory).
func checksum(data []byte) uint32 {
	var sum uint32
	for i := 0; i < len(data); i += 4 {
		var word [4]byte
		copy(word[:], data[i:])
		sum += binary.BigEndian.Uint32(word[:])
	}
	return sum
}

func maxAdvance(glyphs []Glyph) uint16 {
	var m uint16
	for _, g := range glyphs {
		m = max(m, g.Advance)
	}
	return m
}

func be16(b *bytes.Buffer, v uint16) {
	b.Write(binary.BigEndian.AppendUint16(nil, v))
}

func be32(b *bytes.Buffer, v uint32) {
	b.Write(binary.BigEndian.AppendUint32(nil, v))
}
