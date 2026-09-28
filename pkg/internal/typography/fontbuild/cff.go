package fontbuild

import (
	"bytes"
	"encoding/binary"
)

// CFF describes a name-keyed CFF program whose encoding is the Standard
// encoding (the Top DICT default), or Encoding when set.
type CFF struct {
	FontName string
	// Glyphs follow the .notdef glyph, which the program always holds at
	// GID 0 with the advance of NotdefAdvance. Their Unicode and Symbol
	// fields are ignored: a CFF program reaches a glyph by name.
	Glyphs        []Glyph
	NotdefAdvance uint16
	// Encoding, when set, is a custom encoding mapping codes to glyph names,
	// written as a format 0 encoding over the leading glyphs it names and
	// supplements for the rest (Adobe Technical Note #5176, section 12).
	Encoding map[byte]string
	// Subrs draws every glyph's outline through a global subroutine calling
	// a local one, so the program carries both subroutine INDEXes.
	Subrs bool
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
		if c.Subrs {
			// Global subroutine 0, whose biased index is -107.
			cs = append(cs, dictInt(-107)...)
			cs = append(cs, 29) // callgsubr
		}
		if g.Seac != nil {
			cs = append(cs, dictInt(0)...)
			cs = append(cs, dictInt(0)...)
			cs = append(cs, dictInt(int32(g.Seac[0]))...)
			cs = append(cs, dictInt(int32(g.Seac[1]))...)
		}
		cs = append(cs, 14)
		charStrings = append(charStrings, cs)
	}
	// defaultWidthX 0, nominalWidthX 0.
	private := []byte{0x8B, 0x14, 0x8B, 0x15}
	var localSubrs, gsubrs [][]byte
	if c.Subrs {
		// The local subroutine draws two sides of a square, and the global one
		// moves to the origin and calls it.
		local := append(dictInt(500), dictInt(0)...)
		local = append(local, 5) // rlineto
		local = append(local, dictInt(0)...)
		local = append(local, dictInt(500)...)
		local = append(local, 5, 11) // rlineto, return
		localSubrs = [][]byte{local}
		global := append(dictInt(0), dictInt(0)...)
		global = append(global, 21) // rmoveto
		global = append(global, dictInt(-107)...)
		global = append(global, 10, 11) // callsubr, return
		gsubrs = [][]byte{global}
		// Subrs sits right after the Private DICT, whose length the
		// five-byte offset form fixes.
		private = append(private, dictInt32(int32(len(private)+6))...) //nolint:gosec // G115: a few bytes.
		private = append(private, 19)
	}

	var encoding []byte
	if c.Encoding != nil {
		encoding = buildEncoding(glyphs, c.Encoding)
	}

	header := []byte{1, 0, 4, 4}
	nameIndex := index([][]byte{[]byte(c.FontName)})
	stringIndex := index(strs)
	globalSubrs := index(gsubrs)
	charStringsIndex := index(charStrings)

	// Offsets use the five-byte integer form, so the Top DICT's length does
	// not depend on their values.
	encodingPlaceholder := 0
	if encoding != nil {
		encodingPlaceholder = 1
	}
	topIndexLen := len(index([][]byte{topDict(0, encodingPlaceholder, 0, 0, len(private))}))
	charsetOff := len(header) + len(nameIndex) + topIndexLen + len(stringIndex) + len(globalSubrs)
	encodingOff := charsetOff + charset.Len()
	charStringsOff := encodingOff + len(encoding)
	privateOff := charStringsOff + len(charStringsIndex)
	if encoding == nil {
		encodingOff = 0
	}
	top := topDict(charsetOff, encodingOff, charStringsOff, privateOff, len(private))

	var b bytes.Buffer
	b.Write(header)
	b.Write(nameIndex)
	b.Write(index([][]byte{top}))
	b.Write(stringIndex)
	b.Write(globalSubrs)
	b.Write(charset.Bytes())
	b.Write(encoding)
	b.Write(charStringsIndex)
	b.Write(private)
	if c.Subrs {
		b.Write(index(localSubrs))
	}
	return b.Bytes()
}

// buildEncoding returns a custom encoding giving each code of enc the glyph it
// names: a format 0 encoding over the leading glyphs enc names, one code each,
// and a supplement (code, SID) for every other code.
func buildEncoding(glyphs []Glyph, enc map[byte]string) []byte {
	sid := make(map[string]uint16, len(glyphs))
	for i, g := range glyphs[1:] {
		sid[g.Name] = uint16(391 + i) //nolint:gosec // G115: test programs hold few glyphs.
	}
	codeOf := make(map[string]int)
	for code := 255; code >= 0; code-- {
		if name, ok := enc[byte(code)]; ok {
			codeOf[name] = code
		}
	}
	var codes []byte
	used := make(map[int]bool)
	for _, g := range glyphs[1:] {
		code, ok := codeOf[g.Name]
		if !ok {
			break
		}
		codes = append(codes, byte(code)) //nolint:gosec // G115: code is a byte.
		used[code] = true
	}
	var supplements []byte
	for code := range 256 {
		name, ok := enc[byte(code)]
		if !ok || used[code] {
			continue
		}
		if s, ok := sid[name]; ok {
			supplements = append(supplements, byte(code)) //nolint:gosec // G115: code is a byte.
			supplements = binary.BigEndian.AppendUint16(supplements, s)
		}
	}
	format := byte(0)
	if len(supplements) > 0 {
		format |= 0x80
	}
	out := append([]byte{format, byte(len(codes))}, codes...) //nolint:gosec // G115: at most 255 glyphs are encoded.
	if len(supplements) > 0 {
		out = append(out, byte(len(supplements)/3)) //nolint:gosec // G115: at most 256 supplements in a test program.
		out = append(out, supplements...)
	}
	return out
}

// topDict returns a Top DICT with a FontBBox and the given charset, encoding
// (0 for the Standard encoding), CharStrings and Private DICT offsets.
func topDict(charsetOff, encodingOff, charStringsOff, privateOff, privateLen int) []byte {
	var top []byte
	for _, v := range []int32{0, 0, 1000, 800} {
		top = append(top, dictInt(v)...)
	}
	top = append(top, 5)                               // FontBBox
	top = append(top, dictInt32(int32(charsetOff))...) //nolint:gosec // G115: test programs stay small.
	top = append(top, 15)                              // charset
	if encodingOff != 0 {
		top = append(top, dictInt32(int32(encodingOff))...) //nolint:gosec // G115: test programs stay small.
		top = append(top, 16)                               // Encoding
	}
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
