package build

// Port of cff.go.
//
// A minimal bare CFF font program, the Type1C form of `FontFile3`.
//
// ISO 19005-1, 6.3.5 test 2 and ISO 19005-2, 6.2.11.4.2 test 1 both compare a
// font descriptor's `/CharSet` against the glyph names the embedded program
// defines, so reaching them needs a program a validator can actually parse a
// charset out of. truetype.go does the same job for `FontFile2`; the corpus
// builders take no dependency beyond pikepdf, so this module emits the bytes
// directly.
//
// The program holds three glyphs, `.notdef` plus `A` and `B`, drawn as plain
// rectangles, which is enough to render and to resolve every code a fixture
// shows. `A` and `B` are standard strings (SID 34 and 35 per ISO 32000-1,
// Annex D), so the String INDEX stays empty and the charset names them by
// number.
//
// Every offset in the Top DICT is written in the 5-byte `29` form (ISO
// 32000-1, Table 100), whose width does not depend on the value. The layout is
// therefore fixed before the offsets are known and the file is assembled in
// one pass.

import (
	"bytes"
	"encoding/binary"
)

// cffFontName mirrors FONT_NAME.
const cffFontName = "LithoCFF"

// cffAdvanceWidth mirrors ADVANCE_WIDTH.
const cffAdvanceWidth = 600

// Standard string identifiers of "A" and "B", ISO 32000-1, Annex D.
const (
	cffSIDA = 34
	cffSIDB = 35
)

// cffU8 appends v as struct's ">B": its low 8 bits.
func cffU8(b []byte, v int) []byte {
	return append(b, byte(v)) //nolint:gosec // struct packing keeps the field width; fixture values fit.
}

// cffU16 appends v as struct's ">H": its low 16 bits, big-endian.
func cffU16(b []byte, v int) []byte {
	return binary.BigEndian.AppendUint16(b, uint16(v)) //nolint:gosec // struct packing keeps the field width; fixture values fit.
}

// cffU32 appends v as struct's ">I" or ">i": its low 32 bits, big-endian.
func cffU32(b []byte, v int) []byte {
	return binary.BigEndian.AppendUint32(b, uint32(v)) //nolint:gosec // struct packing keeps the field width; fixture values fit.
}

// cffCat concatenates parts into a new slice.
func cffCat(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

// cffIndex is _index: one CFF INDEX. A count of zero is two bytes and nothing
// else.
func cffIndex(entries [][]byte) []byte {
	if len(entries) == 0 {
		return cffU16(nil, 0)
	}
	offsets := []int{1}
	for _, e := range entries {
		offsets = append(offsets, offsets[len(offsets)-1]+len(e))
	}
	width := 2
	if offsets[len(offsets)-1] < 256 {
		width = 1
	}
	out := cffU16(nil, len(entries))
	out = cffU8(out, width)
	for _, off := range offsets {
		if width == 1 {
			out = cffU8(out, off)
		} else {
			out = cffU16(out, off)
		}
	}
	for _, e := range entries {
		out = append(out, e...)
	}
	return out
}

// cffDictInt is _dict_int: a DICT operand in the fixed-width 32-bit form.
func cffDictInt(value int) []byte {
	return cffU32([]byte{0x1d}, value)
}

// cffT2Num is _t2_num: a Type 2 charstring integer, ISO 32000-1 does not
// restate the encoding. It panics outside -1131..1131, where Python raises
// ValueError.
func cffT2Num(value int) []byte {
	switch {
	case -107 <= value && value <= 107:
		return cffU8(nil, value+139)
	case 108 <= value && value <= 1131:
		v := value - 108
		return cffU8(cffU8(nil, (v>>8)+247), v&0xFF)
	case -1131 <= value && value <= -108:
		v := -value - 108
		return cffU8(cffU8(nil, (v>>8)+251), v&0xFF)
	}
	panic("charstring integer out of the range this module writes")
}

// cffRectCharstring is _rect_charstring: one glyph, a rectangle from (50, 0)
// to (450, height), then endchar.
func cffRectCharstring(height int) []byte {
	out := cffCat(cffT2Num(50), cffT2Num(0), []byte{0x15}) // rmoveto
	for _, d := range [][2]int{{400, 0}, {0, height}, {-400, 0}} {
		out = append(out, cffT2Num(d[0])...)
		out = append(out, cffT2Num(d[1])...)
	}
	return append(out, 0x05, 0x0e) // rlineto, endchar
}

// cffPrivate is the Private DICT every name-keyed and CID-keyed program here
// carries: defaultWidthX, then nominalWidthX 0.
func cffPrivate(defaultWidth int) []byte {
	return cffCat(cffDictInt(defaultWidth), []byte{0x14}, cffDictInt(0), []byte{0x15})
}

// cffHeader is the 4-byte CFF header: major 1, minor 0, hdrSize 4, offSize 1.
func cffHeader() []byte { return []byte{0x01, 0x00, 0x04, 0x01} }

// cffBuild is build: return the bytes of a bare CFF holding .notdef, A and B.
func cffBuild() []byte {
	charstrings := [][]byte{{0x0e}, cffRectCharstring(400), cffRectCharstring(500)}

	private := cffPrivate(cffAdvanceWidth)

	// A Top DICT of fixed width: charset (15), CharStrings (17), Private (18).
	topLen := len(cffDictInt(0))*4 + 3
	header := cffHeader()
	nameIndex := cffIndex([][]byte{[]byte(cffFontName)})
	topIndexLen := len(cffIndex([][]byte{make([]byte, topLen)}))
	stringIndex := cffIndex(nil)
	gsubrIndex := cffIndex(nil)

	charsetOff := len(header) + len(nameIndex) + topIndexLen + len(stringIndex) + len(gsubrIndex)
	charset := cffU16(cffU16([]byte{0x00}, cffSIDA), cffSIDB)
	charstringsOff := charsetOff + len(charset)
	charstringsIndex := cffIndex(charstrings)
	privateOff := charstringsOff + len(charstringsIndex)

	top := cffCat(
		cffDictInt(charsetOff), []byte{0x0f},
		cffDictInt(charstringsOff), []byte{0x11},
		cffDictInt(len(private)), cffDictInt(privateOff), []byte{0x12},
	)
	if len(top) != topLen {
		panic("Top DICT width changed, which would move every offset")
	}

	return cffCat(header, nameIndex, cffIndex([][]byte{top}), stringIndex, gsubrIndex,
		charset, charstringsIndex, private)
}

// cffCIDTop is the make_top closure of build_cid and build_cid_charstrings.
// ROS (12 30) naming standard string 0 twice with supplement 0, then charset
// (15), CharStrings (17), FDArray (12 36) and FDSelect (12 37).
func cffCIDTop(charsetOff, charstringsOff, fdarrayOff, fdselectOff int) []byte {
	return cffCat(
		cffDictInt(0), cffDictInt(0), cffDictInt(0), []byte{0x0c, 0x1e},
		cffDictInt(charsetOff), []byte{0x0f},
		cffDictInt(charstringsOff), []byte{0x11},
		cffDictInt(fdarrayOff), []byte{0x0c, 0x24},
		cffDictInt(fdselectOff), []byte{0x0c, 0x25},
	)
}

// cffFDSelect3 is an FDSelect in format 3: one range putting nGlyphs glyphs in
// font DICT 0.
func cffFDSelect3(nGlyphs int) []byte {
	out := cffU8(nil, 3)
	out = cffU16(out, 1)
	out = cffU16(out, 0)
	out = cffU8(out, 0)
	return cffU16(out, nGlyphs)
}

// cffBuildCID is build_cid: return the bytes of a CID-keyed CFF holding
// .notdef and one glyph per CID.
//
// The charset maps glyph i+1 to cids[i], so a CID the list leaves out has no
// glyph in the program. Every glyph advances ADVANCE_WIDTH: the one font
// DICT's Private DICT sets defaultWidthX, and no charstring states a width of
// its own. A CID-keyed font carries its Private DICT in an FDArray, chosen per
// glyph through FDSelect (Adobe Technical Note #5176, 19), and the ROS
// operator in the Top DICT is what marks it CID-keyed.
func cffBuildCID(cids []int) []byte {
	charstrings := make([][]byte, 1, 1+len(cids))
	charstrings[0] = []byte{0x0e}
	for range cids {
		charstrings = append(charstrings, cffRectCharstring(400))
	}
	private := cffPrivate(cffAdvanceWidth)

	makeFontDict := func(privateOff int) []byte {
		return cffCat(cffDictInt(len(private)), cffDictInt(privateOff), []byte{0x12})
	}

	header := cffHeader()
	nameIndex := cffIndex([][]byte{[]byte(cffFontName)})
	topIndexLen := len(cffIndex([][]byte{cffCIDTop(0, 0, 0, 0)}))
	stringIndex := cffIndex(nil)
	gsubrIndex := cffIndex(nil)

	charsetOff := len(header) + len(nameIndex) + topIndexLen + len(stringIndex) + len(gsubrIndex)
	charset := []byte{0x00}
	for _, c := range cids {
		charset = cffU16(charset, c)
	}
	fdselectOff := charsetOff + len(charset)
	// FDSelect format 3: one range putting every glyph in font DICT 0.
	fdselect := cffFDSelect3(len(charstrings))
	charstringsOff := fdselectOff + len(fdselect)
	charstringsIndex := cffIndex(charstrings)
	fdarrayOff := charstringsOff + len(charstringsIndex)
	privateOff := fdarrayOff + len(cffIndex([][]byte{makeFontDict(0)}))

	top := cffCIDTop(charsetOff, charstringsOff, fdarrayOff, fdselectOff)
	return cffCat(header, nameIndex, cffIndex([][]byte{top}), stringIndex, gsubrIndex,
		charset, fdselect, charstringsIndex, cffIndex([][]byte{makeFontDict(privateOff)}), private)
}

// cffBuildCharstrings is build_charstrings: build's name-keyed program with
// the CharStrings INDEX given.
//
// The charset still names A and B, so a program given fewer than three
// charstrings, or none, keeps names for glyphs it holds no program for.
// defaultWidth is the Private DICT's defaultWidthX; Python defaults it to
// ADVANCE_WIDTH.
func cffBuildCharstrings(charstrings [][]byte, defaultWidth int) []byte {
	private := cffPrivate(defaultWidth)
	topLen := len(cffDictInt(0))*4 + 3
	header := cffHeader()
	nameIndex := cffIndex([][]byte{[]byte(cffFontName)})
	charsetOff := len(header) + len(nameIndex) + len(cffIndex([][]byte{make([]byte, topLen)})) + 4
	charset := cffU16(cffU16([]byte{0x00}, cffSIDA), cffSIDB)
	charstringsOff := charsetOff + len(charset)
	charstringsIndex := cffIndex(charstrings)
	privateOff := charstringsOff + len(charstringsIndex)
	top := cffCat(
		cffDictInt(charsetOff), []byte{0x0f},
		cffDictInt(charstringsOff), []byte{0x11},
		cffDictInt(len(private)), cffDictInt(privateOff), []byte{0x12},
	)
	if len(top) != topLen {
		panic("Top DICT width changed, which would move every offset")
	}
	return cffCat(header, nameIndex, cffIndex([][]byte{top}), cffIndex(nil), cffIndex(nil),
		charset, charstringsIndex, private)
}

// cffBuildCIDCharstrings is build_cid_charstrings: a CID-keyed program whose
// charset maps glyphs 1 and 2 to CIDs 1 and 2, holding only the first count
// charstrings (.notdef, then one per CID). defaultWidth is the one font DICT's
// defaultWidthX; Python defaults it to ADVANCE_WIDTH.
func cffBuildCIDCharstrings(count, defaultWidth int) []byte {
	cids := []int{1, 2}
	all := make([][]byte, 1, 1+len(cids))
	all[0] = []byte{0x0e}
	for range cids {
		all = append(all, cffRectCharstring(400))
	}
	// Python's [:count], negative counts included.
	n := count
	if n < 0 {
		n = max(0, len(all)+n)
	}
	charstrings := all[:min(n, len(all))]
	private := cffPrivate(defaultWidth)

	header := cffHeader()
	nameIndex := cffIndex([][]byte{[]byte(cffFontName)})
	charsetOff := len(header) + len(nameIndex) + len(cffIndex([][]byte{cffCIDTop(0, 0, 0, 0)})) + 4
	charset := []byte{0x00}
	for _, c := range cids {
		charset = cffU16(charset, c)
	}
	fdselectOff := charsetOff + len(charset)
	fdselect := cffFDSelect3(max(count, 1))
	charstringsOff := fdselectOff + len(fdselect)
	charstringsIndex := cffIndex(charstrings)
	fdarrayOff := charstringsOff + len(charstringsIndex)
	fontDictLen := len(cffDictInt(0))*2 + 1
	privateOff := fdarrayOff + len(cffIndex([][]byte{make([]byte, fontDictLen)}))
	fontDict := cffCat(cffDictInt(len(private)), cffDictInt(privateOff), []byte{0x12})
	top := cffCIDTop(charsetOff, charstringsOff, fdarrayOff, fdselectOff)
	return cffCat(header, nameIndex, cffIndex([][]byte{top}), cffIndex(nil), cffIndex(nil),
		charset, fdselect, charstringsIndex, cffIndex([][]byte{fontDict}), private)
}

// cffCFF2Index is _cff2_index: a CFF2 INDEX, whose count is a Card32
// (OpenType CFF2 table, INDEX Data).
func cffCFF2Index(entries [][]byte) []byte {
	if len(entries) == 0 {
		return cffU32(nil, 0)
	}
	offsets := []int{1}
	for _, e := range entries {
		offsets = append(offsets, offsets[len(offsets)-1]+len(e))
	}
	out := cffU32(nil, len(entries))
	out = cffU8(out, 2)
	for _, off := range offsets {
		out = cffU16(out, off)
	}
	for _, e := range entries {
		out = append(out, e...)
	}
	return out
}

// cffCFF2Rect is _cff2_rect: a CFF2 charstring, the rectangle of
// cffRectCharstring with no width operand and no endchar, neither of which
// CFF2 has.
func cffCFF2Rect(height int) []byte {
	out := cffCat(cffT2Num(50), cffT2Num(0), []byte{0x15})
	for _, d := range [][2]int{{400, 0}, {0, height}, {-400, 0}} {
		out = append(out, cffT2Num(d[0])...)
		out = append(out, cffT2Num(d[1])...)
	}
	return append(out, 0x05)
}

// cffBuildCFF2 is build_cff2: return a 'CFF2' table holding .notdef, A and B
// (OpenType, CFF2 table).
//
// CFF2 drops the Name, String and charset structures and the Top DICT INDEX:
// a 5-byte header gives the length of a bare Top DICT, which names the
// CharStrings INDEX and the FDArray holding the one Private DICT. Advance
// widths live in 'hmtx' alone.
func cffBuildCFF2() []byte {
	charstrings := [][]byte{cffCFF2Rect(100), cffCFF2Rect(700), cffCFF2Rect(500)}
	private := cffCat(cffDictInt(0), []byte{0x0a}) // StdHW 0, so the Private DICT is not empty
	topLen := len(cffDictInt(0))*2 + 1 + 2
	gsubrs := cffCFF2Index(nil)
	charstringsOff := 5 + topLen + len(gsubrs)
	charstringsIndex := cffCFF2Index(charstrings)
	fdarrayOff := charstringsOff + len(charstringsIndex)
	fontDictLen := len(cffDictInt(0))*2 + 1
	privateOff := fdarrayOff + len(cffCFF2Index([][]byte{make([]byte, fontDictLen)}))
	fontDict := cffCat(cffDictInt(len(private)), cffDictInt(privateOff), []byte{0x12})
	top := cffCat(cffDictInt(charstringsOff), []byte{0x11}, cffDictInt(fdarrayOff), []byte{0x0c, 0x24})
	if len(top) != topLen {
		panic("Top DICT width changed, which would move every offset")
	}
	head := cffU16([]byte{2, 0, 5}, topLen)
	return cffCat(head, top, gsubrs, charstringsIndex, cffCFF2Index([][]byte{fontDict}), private)
}
