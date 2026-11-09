package build

// Port of type1.go.
//
// A minimal Type 1 font program, the form `/FontFile` holds.
//
// ISO 32000-1, 9.9.1 and Table 126 make a `/FontFile` stream a Type 1 program
// in the form the Adobe Type 1 Font Format defines: a clear-text portion, then
// the eexec-encrypted portion holding the Private dictionary and the
// CharStrings, then 512 zeros and `cleartomark`. `/Length1`, `/Length2` and
// `/Length3` count the three. The corpus builders take no dependency beyond
// pikepdf, so this module writes the bytes and the two encryptions (Type 1
// Font Format, 7.1) itself.
//
// Every glyph is a rectangle with an advance of ADVANCE_WIDTH. The four
// plaintext bytes each encryption starts with are zeros, so the program is the
// same on every build. The first ciphertext byte is then 0xD9, which is not a
// hexadecimal digit, so a reader takes the encrypted portion as binary.

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// t1AdvanceWidth mirrors ADVANCE_WIDTH.
const t1AdvanceWidth = 600

// Mirrors EEXEC_KEY, CHARSTRING_KEY, C1, C2 and LEN_IV.
const (
	t1EexecKey      = 55665
	t1CharstringKey = 4330
	t1C1            = 52845
	t1C2            = 22719
	t1LenIV         = 4
)

// t1GlyphHeights mirrors GLYPH_HEIGHTS. The nth glyph is 400 + 100 * (n %
// GLYPH_HEIGHTS) high, so a program of any size keeps every height a
// charstring integer this module writes.
const t1GlyphHeights = 8

// t1Encrypt is _encrypt: the Type 1 encryption of plain under key, Type 1
// Font Format, 7.1.
func t1Encrypt(plain []byte, key int) []byte {
	r := key
	out := make([]byte, 0, len(plain))
	for _, p := range plain {
		c := int(p) ^ (r >> 8)
		out = append(out, byte(c)) //nolint:gosec // r < 2^16, so c < 2^8.
		r = ((c+r)*t1C1 + t1C2) & 0xFFFF
	}
	return out
}

// t1Num is _num: a Type 1 charstring integer, Type 1 Font Format, 6.2. It
// panics outside -1131..1131, where Python raises ValueError.
func t1Num(value int) []byte {
	switch {
	case -107 <= value && value <= 107:
		return []byte{byte(value + 139)} //nolint:gosec // 32..246.
	case 108 <= value && value <= 1131:
		v := value - 108
		return []byte{byte((v >> 8) + 247), byte(v & 0xFF)} //nolint:gosec // v < 1024.
	case -1131 <= value && value <= -108:
		v := -value - 108
		return []byte{byte((v >> 8) + 251), byte(v & 0xFF)} //nolint:gosec // v < 1024.
	}
	panic("charstring integer out of the range this module writes")
}

// t1Charstring is _charstring: hsbw, then a rectangle from (50, 0) of 400 by
// height, then endchar.
func t1Charstring(height int) []byte {
	out := t1Cat(t1Num(0), t1Num(t1AdvanceWidth), []byte{0x0d}) // hsbw
	if height != 0 {
		out = t1Cat(out, t1Num(50), t1Num(0), []byte{0x15})                                                // rmoveto
		out = t1Cat(out, t1Num(400), []byte{0x06}, t1Num(height), []byte{0x07}, t1Num(-400), []byte{0x06}) // hlineto vlineto hlineto
		out = append(out, 0x09)                                                                            // closepath
	}
	return append(out, 0x0e) // endchar
}

// t1Cat concatenates parts into a new slice.
func t1Cat(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

// t1ASCII mirrors str.encode("ascii"), which raises on anything else.
func t1ASCII(s string) []byte {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			panic(fmt.Sprintf("type1: %q is not ASCII", s))
		}
	}
	return []byte(s)
}

// t1EncodingArray is encoding_array: the clear-text /Encoding of a program
// naming codes, a {code: name} map.
//
// fillNotdef writes the customary loop that names every code .notdef before
// the entries, so a code codes leaves out names .notdef. Without it the array
// keeps the null objects `array` creates. Python defaults fill_notdef to
// True.
func t1EncodingArray(codes map[int]string, fillNotdef bool) []byte {
	out := []byte("/Encoding 256 array\n")
	if fillNotdef {
		out = append(out, "0 1 255 {1 index exch /.notdef put} for\n"...)
	}
	keys := make([]int, 0, len(codes))
	for c := range codes {
		keys = append(keys, c)
	}
	sort.Ints(keys)
	for _, code := range keys {
		out = fmt.Appendf(out, "dup %d /%s put\n", code, t1ASCII(codes[code]))
	}
	return append(out, "readonly def\n"...)
}

// t1Options carries build's keyword arguments. The zero value is Python's
// defaults.
type t1Options struct {
	// Name is the FontName; empty means "LithoT1".
	Name string
	// Glyphs names the CharStrings beside .notdef; nil means A and B. A
	// non-nil empty slice means no glyph but .notdef.
	Glyphs []string
	// Encoding is the clear-text /Encoding entry; nil means
	// StandardEncoding.
	Encoding []byte
	// OmitCharStrings is Python's charstrings=False.
	OmitCharStrings bool
}

// t1Build is build: return (program, length1, length2, length3) for a Type 1
// font.
//
// glyphs names the CharStrings beside .notdef. encoding is the clear-text
// /Encoding entry, StandardEncoding when None. charstrings=False leaves the
// Private dictionary without a CharStrings dictionary, which ends the program
// with no glyph defined.
func t1Build(o t1Options) (program []byte, length1, length2, length3 int) {
	name := []byte(o.Name)
	if o.Name == "" {
		name = []byte("LithoT1")
	}
	glyphs := o.Glyphs
	if glyphs == nil {
		glyphs = []string{"A", "B"}
	}
	encoding := o.Encoding
	if encoding == nil {
		encoding = []byte("/Encoding StandardEncoding def\n")
	}
	clear := t1Cat(
		[]byte("%!PS-AdobeFont-1.0: "), name, []byte(" 001.000\n"+
			"11 dict begin\n"+
			"/FontName /"), name, []byte(" def\n"),
		encoding,
		[]byte("/PaintType 0 def\n"+
			"/FontType 1 def\n"+
			"/FontMatrix [0.001 0 0 0.001 0 0] readonly def\n"+
			"/FontBBox {0 -200 550 700} readonly def\n"+
			"currentdict end\n"+
			"currentfile eexec\n"),
	)
	private := fmt.Appendf(nil,
		"dup /Private 8 dict dup begin\n"+
			"/RD{string currentfile exch readstring pop}executeonly def\n"+
			"/ND{noaccess def}executeonly def\n"+
			"/NP{noaccess put}executeonly def\n"+
			"/BlueValues [] def\n"+
			"/MinFeature {16 16} def\n"+
			"/password 5839 def\n"+
			"/lenIV %d def\n", t1LenIV)
	// One subroutine, a bare return, since readers expect a /Subrs array.
	subr := t1Encrypt(append(make([]byte, t1LenIV), 0x0b), t1CharstringKey)
	private = fmt.Appendf(private, "/Subrs 1 array\ndup 0 %d RD ", len(subr))
	private = append(private, subr...)
	private = append(private, " NP\nND\n"...)
	if !o.OmitCharStrings {
		names := append([]string{".notdef"}, glyphs...)
		private = fmt.Appendf(private, "2 index /CharStrings %d dict dup begin\n", len(names))
		for i, glyph := range names {
			height := 0
			if i != 0 {
				height = 400 + 100*(i%t1GlyphHeights)
			}
			cs := t1Encrypt(append(make([]byte, t1LenIV), t1Charstring(height)...), t1CharstringKey)
			private = fmt.Appendf(private, "/%s %d RD ", t1ASCII(glyph), len(cs))
			private = append(private, cs...)
			private = append(private, " ND\n"...)
		}
		private = append(private, "end\n"...)
	}
	private = append(private, "end\nreadonly put\nnoaccess put\ndup /FontName get exch definefont pop\nmark currentfile closefile\n"...)
	encrypted := t1Encrypt(append([]byte{0, 0, 0, 0}, private...), t1EexecKey)
	trailer := []byte(strings.Repeat(strings.Repeat("0", 64)+"\n", 8) + "cleartomark\n")
	return t1Cat(clear, encrypted, trailer), len(clear), len(encrypted), len(trailer)
}
