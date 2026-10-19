package remediation

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// cmapSubtableRecord is one encoding record of a cmap table and the subtable
// it points at.
type cmapSubtableRecord struct {
	platform, encoding uint16
	subtable           []byte
}

// cmapTableOf builds a cmap table holding the given subtables, in order.
func cmapTableOf(records ...cmapSubtableRecord) []byte {
	header := make([]byte, 4+8*len(records))
	binary.BigEndian.PutUint16(header[2:4], uint16(len(records))) //nolint:gosec // a handful of records
	offset := len(header)
	var body []byte
	for i, r := range records {
		rec := header[4+8*i:]
		binary.BigEndian.PutUint16(rec[0:2], r.platform)
		binary.BigEndian.PutUint16(rec[2:4], r.encoding)
		binary.BigEndian.PutUint32(rec[4:8], uint32(offset+len(body))) //nolint:gosec // fixture is a few hundred bytes
		body = append(body, r.subtable...)
	}
	return append(header, body...)
}

// trueTypeWithCmapTable is [probeTrueTypeWithCmap] carrying the given cmap
// table, two glyphs, and GID 1 advancing 500 units.
func trueTypeWithCmapTable(cmapTable []byte) []byte {
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:20], 1000)
	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[34:36], 2)
	hmtx := make([]byte, 8)
	binary.BigEndian.PutUint16(hmtx[4:6], 500)
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:6], 2)
	tables := []struct {
		tag  string
		data []byte
	}{{"cmap", cmapTable}, {"head", head}, {"hhea", hhea}, {"hmtx", hmtx}, {"maxp", maxp}}
	out := make([]byte, 12+len(tables)*16)
	binary.BigEndian.PutUint32(out[0:4], 0x00010000)
	binary.BigEndian.PutUint16(out[4:6], uint16(len(tables))) //nolint:gosec // five tables
	for i, t := range tables {
		rec := 12 + i*16
		copy(out[rec:rec+4], t.tag)
		binary.BigEndian.PutUint32(out[rec+8:rec+12], uint32(len(out)))     //nolint:gosec // fixture is a few hundred bytes
		binary.BigEndian.PutUint32(out[rec+12:rec+16], uint32(len(t.data))) //nolint:gosec // fixture table is small
		out = append(out, t.data...)
	}
	return out
}

// streamObject renders data as a PDF stream object body.
func streamObject(data []byte) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(data), data)
}

// symbolicTrueTypePDF builds a page whose object 4 is a symbolic TrueType font
// stating no /Encoding, embedding program. fontExtra holds further font
// dictionary entries, and extra further object bodies, numbered from 7.
func symbolicTrueTypePDF(program []byte, fontExtra string, extra ...string) []byte {
	return assemblePDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /TrueType /BaseFont /LithoSym /FirstChar 65 /LastChar 65 /Widths [500] /FontDescriptor 5 0 R" + fontExtra + " >>",
		"<< /Type /FontDescriptor /FontName /LithoSym /Flags 4 /FontFile2 6 0 R >>",
		streamObject(program),
	}, extra...))
}

// forbiddenToUnicode is a /ToUnicode CMap stream a producer wrote: it maps
// two-byte code 0x0001 to U+0041 and 0x0002 to U+0000, the value ISO 19005-2,
// 6.2.11.7.2 forbids.
func forbiddenToUnicode() string {
	return streamObject([]byte("/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <0000> <FFFF> endcodespacerange\n" +
		"2 beginbfchar <0001> <0041> <0002> <0000> endbfchar\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"))
}

// TestFixSimpleFontToUnicode_SymbolicTrueTypeReadsItsProgram pins that a
// symbolic TrueType font stating no /Encoding gets the /ToUnicode its program
// grounds.
//
// ISO 32000-1, 9.6.6.4 takes code 0x41 to a glyph through the (3,0) subtable,
// at 0xF041, and the (3,1) subtable names that glyph U+03A3. Text extraction
// reads the code that way, and the repair writes the same reading.
func TestFixSimpleFontToUnicode_SymbolicTrueTypeReadsItsProgram(t *testing.T) {
	t.Parallel()

	program := trueTypeWithCmapTable(cmapTableOf(
		cmapSubtableRecord{3, 0, cmapFormat4Single(0xF041, 1)},
		cmapSubtableRecord{3, 1, cmapFormat4Single(0x03A3, 1)},
	))
	cmap, fixes := generatedToUnicode(t, symbolicTrueTypePDF(program, ""), codes(0x41))
	if cmap == "" {
		t.Fatalf("no /ToUnicode was generated, fixes=%v", fixes)
	}
	if !strings.Contains(cmap, "<41> <03A3>") {
		t.Errorf("code 0x41 was not mapped to U+03A3, the character the program names its glyph:\n%s", cmap)
	}
}

// TestFixSimpleFontToUnicode_SymbolicTrueTypeLeavesAnUnnamedGlyph is the
// control: when the program draws a code's glyph but names no character for
// it, nothing grounds a mapping, and a guessed one is what turned "−∞ ∞" into
// "−1 1" (see [fixSimpleFontToUnicode]).
func TestFixSimpleFontToUnicode_SymbolicTrueTypeLeavesAnUnnamedGlyph(t *testing.T) {
	t.Parallel()

	program := trueTypeWithCmapTable(cmapTableOf(
		cmapSubtableRecord{3, 0, cmapFormat4Single(0xF041, 1)},
	))
	if cmap, _ := generatedToUnicode(t, symbolicTrueTypePDF(program, ""), codes(0x41)); cmap != "" {
		t.Errorf("a glyph the program names no character for was given one:\n%s", cmap)
	}
}

// TestFixSimpleFontToUnicode_SymbolicTrueTypeKeepsTheProducersMap is the
// control for a font that carries a map holding a forbidden value: the
// producer's other readings stand, and the program's do not replace them.
//
// # Corpus evidence
//
//   - pdfjs issue20232.pdf: its Type 0 GOSTTypeA carries such a map, and
//     replacing it with the program's turned "№" into "¹" in the remediated
//     text.
func TestFixSimpleFontToUnicode_SymbolicTrueTypeKeepsTheProducersMap(t *testing.T) {
	t.Parallel()

	program := trueTypeWithCmapTable(cmapTableOf(
		cmapSubtableRecord{3, 0, cmapFormat4Single(0xF041, 1)},
		cmapSubtableRecord{3, 1, cmapFormat4Single(0x03A3, 1)},
	))
	data := symbolicTrueTypePDF(program, " /ToUnicode 7 0 R", forbiddenToUnicode())
	if cmap, _ := generatedToUnicode(t, data, codes(0x41)); cmap != "" {
		t.Errorf("the producer's /ToUnicode was replaced:\n%s", cmap)
	}
}

// type0ProgramPDF builds a page whose object 4 is a Type 0 font under
// encoding, with a CIDFontType2 descendant of the given registry and
// ordering whose /CIDToGIDMap stream sends CID 1 to GID 1, the glyph the
// program's (3,1) subtable names U+0041. extra holds further object bodies,
// numbered from 9.
func type0ProgramPDF(encoding, ordering, fontExtra string, extra ...string) []byte {
	program := trueTypeWithCmapTable(cmapTableOf(
		cmapSubtableRecord{3, 1, cmapFormat4Single(0x0041, 1)},
	))
	return assemblePDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /LithoCID /Encoding " + encoding + " /DescendantFonts [5 0 R]" + fontExtra + " >>",
		"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /LithoCID /CIDSystemInfo << /Registry (Adobe) /Ordering (" + ordering + ") /Supplement 0 >>" +
			" /FontDescriptor 6 0 R /CIDToGIDMap 8 0 R /W [1 [500]] >>",
		"<< /Type /FontDescriptor /FontName /LithoCID /Flags 4 /FontFile2 7 0 R >>",
		streamObject(program),
		streamObject([]byte{0x00, 0x00, 0x00, 0x01}),
	}, extra...))
}

// type0ToUnicode runs the Type 0 repair over data for the shown strings and
// returns the CMap it attached, or "" when it attached none.
func type0ToUnicode(t *testing.T, data []byte, shown ...string) string {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	fontRef := ast.PdfIndirectRef{ObjectNum: 4}
	fontDict, err := document.ResolveDict(ctx, doc, fontRef)
	if err != nil {
		t.Fatalf("resolving the font: %v", err)
	}
	strs := make([]ast.PdfString, len(shown))
	for i, s := range shown {
		strs[i] = ast.PdfString(s)
	}
	fixType0Font(ctx, doc, alloc, fontDict, fontRef, false, nil, strs)

	font, _ := alloc.Pending()[4].(ast.PdfDict)
	ref, ok := font["ToUnicode"].(ast.PdfIndirectRef)
	if !ok {
		return ""
	}
	stream, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("/ToUnicode points at %T, want a stream", alloc.Pending()[ref.ObjectNum])
	}
	raw, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the CMap: %v", err)
	}
	return string(raw)
}

// oneByteEncodingCMap is an embedded encoding CMap stream mapping the one-byte
// code <20> to CID 1 in a one-byte codespace.
func oneByteEncodingCMap() string {
	return streamObject([]byte("/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> def\n" +
		"/CMapName /LithoOneByte def /CMapType 1 def\n" +
		"1 begincodespacerange <00> <FF> endcodespacerange\n" +
		"1 begincidchar <20> 1 endcidchar\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"))
}

// TestFixType0Font_ToUnicodeThroughACIDToGIDMapStream pins the /ToUnicode of a
// Type 0 font whose CIDs reach glyphs through a /CIDToGIDMap stream.
//
// The identity route keys a GID table as codes, which a stream breaks
// ([identityCIDRoute]). Composed instead, code to CID by the encoding CMap
// (ISO 32000-1, 9.7.5.2 and 9.7.6.2), CID to GID by the stream (9.7.4.2) and
// GID to Unicode by the program, the codes get the characters text
// extraction reads them as. An embedded encoding CMap gives the map its
// codespace (9.10.3).
func TestFixType0Font_ToUnicodeThroughACIDToGIDMapStream(t *testing.T) {
	t.Parallel()

	t.Run("Identity-H", func(t *testing.T) {
		t.Parallel()
		cmap := type0ToUnicode(t, type0ProgramPDF("/Identity-H", "Identity", ""), "\x00\x01")
		if !strings.Contains(cmap, "<0001> <0041>") {
			t.Errorf("code <0001> was not mapped to U+0041:\n%s", cmap)
		}
	})
	t.Run("embedded CMap", func(t *testing.T) {
		t.Parallel()
		cmap := type0ToUnicode(t, type0ProgramPDF("9 0 R", "Identity", "", oneByteEncodingCMap()), "\x20")
		if !strings.Contains(cmap, "<20> <0041>") || !strings.Contains(cmap, "<00> <FF>") {
			t.Errorf("code <20> was not mapped to U+0041 in the encoding's codespace:\n%s", cmap)
		}
	})
}

// TestFixType0Font_ToUnicodeThroughACIDToGIDMapStreamRefuses is the control
// for the codes and collections the composition does not ground.
func TestFixType0Font_ToUnicodeThroughACIDToGIDMapStreamRefuses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		data  []byte
		shown string
	}{
		// CID 0 goes to GID 0, the .notdef glyph, which names no character.
		{"a code whose glyph names nothing", type0ProgramPDF("/Identity-H", "Identity", ""), "\x00\x01\x00\x00"},
		// ISO 19005-2, 6.2.11.7.2 exempts the collection: its UCS2 CMap
		// names every CID.
		{"an Adobe CJK collection", type0ProgramPDF("/Identity-H", "Japan1", ""), "\x00\x01"},
		// The CMap maps <20> alone, so <21> selects CID 0 (9.7.6.3).
		{"a code the encoding CMap does not map", type0ProgramPDF("9 0 R", "Identity", "", oneByteEncodingCMap()), "\x20\x21"},
		{"a font nothing shows", type0ProgramPDF("/Identity-H", "Identity", ""), ""},
		// A map the producer wrote stands: see
		// TestFixSimpleFontToUnicode_SymbolicTrueTypeKeepsTheProducersMap.
		{"a font with a map of its own", type0ProgramPDF("/Identity-H", "Identity", " /ToUnicode 9 0 R", forbiddenToUnicode()), "\x00\x01"},
	} {
		var shown []string
		if tc.shown != "" {
			shown = []string{tc.shown}
		}
		if cmap := type0ToUnicode(t, tc.data, shown...); cmap != "" {
			t.Errorf("%s: a /ToUnicode was attached:\n%s", tc.name, cmap)
		}
	}
}

// symbolicType1PDF builds a page whose object 4 is a symbolic Type 1 font
// stating no /Encoding, embedding a program whose built-in encoding is the
// given "dup <code> /<name> put" lines.
func symbolicType1PDF(vector string) []byte {
	program := []byte("%!PS-AdobeFont-1.0: LithoMath 001.000\n/FontName /LithoMath def\n" +
		"/Encoding 256 array\n0 1 255 {1 index exch /.notdef put} for\n" + vector + "readonly def\n" +
		"currentdict end\ncurrentfile eexec\n")
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /LithoMath /FontDescriptor 5 0 R >>",
		"<< /Type /FontDescriptor /FontName /LithoMath /Flags 4 /FontFile 6 0 R >>",
		streamObject(program),
	})
}

// TestFixSimpleFontToUnicode_SymbolicType1ReadsItsBuiltInEncoding pins that a
// symbolic Type 1 font stating no /Encoding gets the /ToUnicode its program's
// built-in encoding grounds.
//
// ISO 32000-1, 9.6.6.1 draws such a font's codes through the built-in
// encoding of its program, which names the glyph each code selects, and a
// name the Adobe Glyph List carries is the character (9.10.2). The TeX math
// fonts of verapdf 6-3-8-t01-fail-a name codes this way: read through a
// standard encoding instead, "−∞ ∞" came out "−1 1".
func TestFixSimpleFontToUnicode_SymbolicType1ReadsItsBuiltInEncoding(t *testing.T) {
	t.Parallel()

	data := symbolicType1PDF("dup 0 /minus put\ndup 49 /infinity put\n")
	cmap, fixes := generatedToUnicode(t, data, codes(0x00, 0x31))
	if cmap == "" {
		t.Fatalf("no /ToUnicode was generated, fixes=%v", fixes)
	}
	for _, want := range []string{"<00> <2212>", "<31> <221E>"} {
		if !strings.Contains(cmap, want) {
			t.Errorf("the CMap lacks %q, the character the program names the glyph:\n%s", want, cmap)
		}
	}
}

// TestFixSimpleFontToUnicode_SymbolicType1RefusesAnUngroundedCode is the
// control for codes the built-in encoding does not ground: a name no glyph
// list carries, a numeric name whose number is a guess at the producer's
// convention, and a code the encoding leaves at .notdef. Each would leave a
// shown code unmapped, so no map is written.
func TestFixSimpleFontToUnicode_SymbolicType1RefusesAnUngroundedCode(t *testing.T) {
	t.Parallel()

	vector := "dup 49 /infinity put\ndup 50 /H11021 put\ndup 51 /C51 put\n"
	for _, tc := range []struct {
		name string
		code byte
	}{
		{"a name no glyph list carries", 0x32},
		{"a numeric name", 0x33},
		{"a code the encoding leaves at .notdef", 0x34},
	} {
		if cmap, _ := generatedToUnicode(t, symbolicType1PDF(vector), codes(0x31, tc.code)); cmap != "" {
			t.Errorf("%s: a /ToUnicode was attached:\n%s", tc.name, cmap)
		}
	}
}
