package remediation

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// cmapFormat0 builds a format 0 cmap subtable, the byte encoding table a
// (1,0) Macintosh subtable is usually written as, mapping each code of gids
// to its glyph.
func cmapFormat0(gids map[byte]byte) []byte {
	sub := make([]byte, 6+256)
	binary.BigEndian.PutUint16(sub[2:4], uint16(len(sub))) //nolint:gosec // 262 bytes
	for code, gid := range gids {
		sub[6+int(code)] = gid
	}
	return sub
}

// trueTypeWithNamedGlyphs builds a TrueType program carrying cmapTable and a
// version 2.0 post table naming its glyphs names, one glyph per name.
func trueTypeWithNamedGlyphs(cmapTable []byte, names ...string) []byte {
	n := len(names)
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:20], 1000)
	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[34:36], uint16(n)) //nolint:gosec // a handful of glyphs
	hmtx := make([]byte, 4*n)
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:6], uint16(n)) //nolint:gosec // a handful of glyphs
	var post bytes.Buffer
	post.Write([]byte{0x00, 0x02, 0x00, 0x00})
	post.Write(make([]byte, 28))
	index := make([]byte, 2+2*n)
	binary.BigEndian.PutUint16(index, uint16(n)) //nolint:gosec // a handful of glyphs
	for i := range n {
		binary.BigEndian.PutUint16(index[2+2*i:], uint16(258+i)) //nolint:gosec // a handful of glyphs past the 258 standard names
	}
	post.Write(index)
	for _, name := range names {
		post.WriteByte(byte(len(name)))
		post.WriteString(name)
	}
	tables := []struct {
		tag  string
		data []byte
	}{{"cmap", cmapTable}, {"head", head}, {"hhea", hhea}, {"hmtx", hmtx}, {"maxp", maxp}, {"post", post.Bytes()}}
	out := make([]byte, 12+len(tables)*16)
	binary.BigEndian.PutUint32(out[0:4], 0x00010000)
	binary.BigEndian.PutUint16(out[4:6], uint16(len(tables))) //nolint:gosec // six tables
	for i, t := range tables {
		rec := 12 + i*16
		copy(out[rec:rec+4], t.tag)
		binary.BigEndian.PutUint32(out[rec+8:rec+12], uint32(len(out)))     //nolint:gosec // fixture is a few hundred bytes
		binary.BigEndian.PutUint32(out[rec+12:rec+16], uint32(len(t.data))) //nolint:gosec // fixture table is small
		out = append(out, t.data...)
		for len(out)%4 != 0 {
			out = append(out, 0)
		}
	}
	return out
}

// TestFixSimpleFontToUnicode_SymbolicMacRomanReadsItsGlyphNames pins that a
// symbolic TrueType font stating no /Encoding, whose only route to its glyphs
// is a genuine (1,0) Macintosh subtable, gets the /ToUnicode its post table
// names when every name agrees with the Mac OS Roman reading of its code.
//
// ISO 32000-1, 9.6.6.4 takes code 0x80 to glyph 1 through the (1,0) subtable,
// whose codes are Mac OS Roman characters, and the post table names that glyph
// Adieresis, which the Adobe Glyph List reads U+00C4 (9.10.2), the character
// Mac OS Roman gives 0x80. Text extraction reads the code that way.
func TestFixSimpleFontToUnicode_SymbolicMacRomanReadsItsGlyphNames(t *testing.T) {
	t.Parallel()

	program := trueTypeWithNamedGlyphs(cmapTableOf(
		cmapSubtableRecord{1, 0, cmapFormat0(map[byte]byte{0x80: 1, 0x8A: 2})},
		cmapSubtableRecord{3, 1, cmapFormat4Single(0x00C4, 1)},
	), ".notdef", "Adieresis", "adieresis")
	cmap, fixes := generatedToUnicode(t, symbolicTrueTypePDF(program, ""), codes(0x80, 0x8A))
	if cmap == "" {
		t.Fatalf("no /ToUnicode was generated, fixes=%v", fixes)
	}
	for _, want := range []string{"<80> <00C4>", "<8A> <00E4>"} {
		if !strings.Contains(cmap, want) {
			t.Errorf("the CMap lacks %s, the character the program names the glyph:\n%s", want, cmap)
		}
	}
}

// TestFixSimpleFontToUnicode_SymbolicMacRomanLeavesAContradictedFont is the
// control: when the post name and the (3,1) subtable agree on a character
// Mac OS Roman does not read, the program describes its glyphs in another
// encoding than the one it declares, and no code of the font is mapped, the
// shown one included.
//
// # Corpus evidence
//
//   - pdfjs bug894572.pdf: TSCInaimathi, a legacy Tamil font, names its glyphs
//     as Windows Latin characters behind a (1,0) subtable and maps them so in
//     (3,1): code 0xE8 selects a glyph named egrave and mapped to U+00E8, where
//     Mac OS Roman reads U+00CB.
func TestFixSimpleFontToUnicode_SymbolicMacRomanLeavesAContradictedFont(t *testing.T) {
	t.Parallel()

	program := trueTypeWithNamedGlyphs(cmapTableOf(
		// Code 0x8F selects a glyph (3,1) does not map U+008F to, so the
		// (1,0) subtable is no copy of (3,1), and 0x8F reads as Mac OS Roman
		// "è", which the glyph's name confirms.
		cmapSubtableRecord{1, 0, cmapFormat0(map[byte]byte{0x41: 1, 0x8F: 3, 0xE8: 2})},
		cmapSubtableRecord{3, 1, cmapFormat4Single(0x00E8, 2)},
	), ".notdef", "A", "egrave", "egrave")
	if cmap, _ := generatedToUnicode(t, symbolicTrueTypePDF(program, ""), codes(0x41)); cmap != "" {
		t.Errorf("a font whose names contradict its Mac OS Roman subtable was mapped:\n%s", cmap)
	}
}

// TestFixSimpleFontToUnicode_SymbolicMacRomanLeavesAnUnnamedGlyph is the
// control for a glyph the post table leaves unnamed: nothing but the
// subtable's declaration says what it shows, and a code's standard-encoding
// reading is the guess that turned "−∞ ∞" into "−1 1" (see
// [fixSimpleFontToUnicode]).
func TestFixSimpleFontToUnicode_SymbolicMacRomanLeavesAnUnnamedGlyph(t *testing.T) {
	t.Parallel()

	program := trueTypeWithCmapTable(cmapTableOf(
		cmapSubtableRecord{1, 0, cmapFormat0(map[byte]byte{0x41: 1})},
	))
	if cmap, _ := generatedToUnicode(t, symbolicTrueTypePDF(program, ""), codes(0x41)); cmap != "" {
		t.Errorf("a glyph the program names no character for was given one:\n%s", cmap)
	}
}

// TestFixSimpleFontToUnicode_SymbolicMacRomanSkipsAnUncorroboratedCode pins
// that a code whose glyph name alone disagrees with Mac OS Roman gets no entry
// while the font's other codes keep theirs, and that a name canonically
// equivalent to the Mac OS Roman reading confirms it.
//
// # Corpus evidence
//
//   - govdocs thread_000_000763.pdf: its TimesNewRoman subsets name the glyph
//     at 0xB9 pi1, U+03D6, where Mac OS Roman reads U+03C0, and the one at
//     0xBD Ohm, U+2126, canonically U+03A9.
func TestFixSimpleFontToUnicode_SymbolicMacRomanSkipsAnUncorroboratedCode(t *testing.T) {
	t.Parallel()

	program := trueTypeWithNamedGlyphs(cmapTableOf(
		cmapSubtableRecord{1, 0, cmapFormat0(map[byte]byte{0x41: 1, 0xB9: 2, 0xBD: 3})},
	), ".notdef", "A", "pi1", "Ohm")
	pdf := symbolicTrueTypePDF(program, "")

	cmap, fixes := generatedToUnicode(t, pdf, codes(0x41, 0xBD))
	if cmap == "" {
		t.Fatalf("no /ToUnicode was generated, fixes=%v", fixes)
	}
	for _, want := range []string{"<41> <0041>", "<BD> <03A9>"} {
		if !strings.Contains(cmap, want) {
			t.Errorf("the CMap lacks %s:\n%s", want, cmap)
		}
	}
	if strings.Contains(cmap, "<B9>") {
		t.Errorf("code 0xB9, whose name contradicts Mac OS Roman, was mapped:\n%s", cmap)
	}
	if cmap, _ := generatedToUnicode(t, pdf, codes(0x41, 0xB9)); cmap != "" {
		t.Errorf("a map was written although shown code 0xB9 has no confirmed reading:\n%s", cmap)
	}
}
