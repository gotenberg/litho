package fontbuild

import (
	"bytes"
	"encoding/binary"
)

// CFF describes a name-keyed CFF program whose encoding is the Standard
// encoding (the Top DICT default).
type CFF struct {
	FontName string
	// Glyphs follow the .notdef glyph, which the program always holds at
	// GID 0 with the advance of NotdefAdvance. Their Unicode and Symbol
	// fields are ignored: a CFF program reaches a glyph by name.
	Glyphs        []Glyph
	NotdefAdvance uint16
}

// Build returns the program's bytes, laid out as header, Name INDEX, Top DICT
// INDEX, String INDEX, empty Global Subr INDEX, charset, CharStrings INDEX and
// Private DICT (Adobe Technical Note #5176). Every glyph name is a custom
// string, which a reader resolves the same way as a standard one.
func (c CFF) Build() []byte {
	glyphs := append([]Glyph{{Name: ".notdef", Advance: c.NotdefAdvance}}, c.Glyphs...)

	strs := make([][]byte, 0, len(glyphs)-1)
	for _, g := range glyphs[1:] {
		strs = append(strs, []byte(g.Name))
	}
	var charset bytes.Buffer
	charset.WriteByte(0) // format 0
	for i := range glyphs[1:] {
		be16(&charset, uint16(391+i)) //nolint:gosec // G115: test programs hold few glyphs.
	}
	charStrings := make([][]byte, 0, len(glyphs))
	for _, g := range glyphs {
		// The advance is the charstring's first operand, measured from a
		// nominalWidthX of 0, and endchar (14) closes the glyph.
		cs := dictInt(int32(g.Advance))
		cs = append(cs, 14)
		charStrings = append(charStrings, cs)
	}
	private := []byte{0x8B, 0x14, 0x8B, 0x15} // defaultWidthX 0, nominalWidthX 0

	header := []byte{1, 0, 4, 4}
	nameIndex := index([][]byte{[]byte(c.FontName)})
	stringIndex := index(strs)
	globalSubrs := index(nil)
	charStringsIndex := index(charStrings)

	// Offsets use the five-byte integer form, so the Top DICT's length does
	// not depend on their values.
	topIndexLen := len(index([][]byte{topDict(0, 0, 0, len(private))}))
	charsetOff := len(header) + len(nameIndex) + topIndexLen + len(stringIndex) + len(globalSubrs)
	charStringsOff := charsetOff + charset.Len()
	privateOff := charStringsOff + len(charStringsIndex)
	top := topDict(charsetOff, charStringsOff, privateOff, len(private))

	var b bytes.Buffer
	b.Write(header)
	b.Write(nameIndex)
	b.Write(index([][]byte{top}))
	b.Write(stringIndex)
	b.Write(globalSubrs)
	b.Write(charset.Bytes())
	b.Write(charStringsIndex)
	b.Write(private)
	return b.Bytes()
}

// topDict returns a Top DICT with a FontBBox and the given charset,
// CharStrings and Private DICT offsets.
func topDict(charsetOff, charStringsOff, privateOff, privateLen int) []byte {
	var top []byte
	for _, v := range []int32{0, 0, 1000, 800} {
		top = append(top, dictInt(v)...)
	}
	top = append(top, 5)                                   // FontBBox
	top = append(top, dictInt32(int32(charsetOff))...)     //nolint:gosec // G115: test programs stay small.
	top = append(top, 15)                                  // charset
	top = append(top, dictInt32(int32(charStringsOff))...) //nolint:gosec // G115: test programs stay small.
	top = append(top, 17)                                  // CharStrings
	top = append(top, dictInt32(int32(privateLen))...)     //nolint:gosec // G115: test programs stay small.
	top = append(top, dictInt32(int32(privateOff))...)     //nolint:gosec // G115: test programs stay small.
	return append(top, 18)                                 // Private
}

// index returns a CFF INDEX with four-byte offsets.
func index(entries [][]byte) []byte {
	var b bytes.Buffer
	be16(&b, uint16(len(entries))) //nolint:gosec // G115: test programs hold few entries.
	if len(entries) == 0 {
		return b.Bytes()
	}
	b.WriteByte(4)
	off := uint32(1)
	be32(&b, off)
	for _, e := range entries {
		off += uint32(len(e)) //nolint:gosec // G115: test entries stay small.
		be32(&b, off)
	}
	for _, e := range entries {
		b.Write(e)
	}
	return b.Bytes()
}

// dictInt encodes v in the shortest DICT integer form that holds it, which is
// also the Type 2 charstring form for values in the int16 range.
func dictInt(v int32) []byte {
	switch {
	case v >= -107 && v <= 107:
		return []byte{byte(v + 139)}
	case v >= 108 && v <= 1131:
		v -= 108
		return []byte{byte(v/256 + 247), byte(v % 256)}
	case v >= -1131 && v <= -108:
		v = -v - 108
		return []byte{byte(v/256 + 251), byte(v % 256)}
	case v >= -32768 && v <= 32767:
		return append([]byte{28}, binary.BigEndian.AppendUint16(nil, uint16(v))...) //nolint:gosec // G115: a two's-complement int16.
	}
	return dictInt32(v)
}

// dictInt32 encodes v in the five-byte DICT integer form.
func dictInt32(v int32) []byte {
	return append([]byte{29}, binary.BigEndian.AppendUint32(nil, uint32(v))...) //nolint:gosec // G115: a two's-complement int32.
}
