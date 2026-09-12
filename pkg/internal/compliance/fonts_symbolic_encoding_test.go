package compliance

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// cmapFormat4Single builds a Format 4 subtable mapping one code to gid, plus
// the 0xFFFF terminator segment.
func cmapFormat4Single(code, gid uint16) []byte {
	const segCount = 2
	sub := make([]byte, 14+segCount*8+2)
	binary.BigEndian.PutUint16(sub[0:], 4)
	binary.BigEndian.PutUint16(sub[2:], uint16(len(sub))) //nolint:gosec // fixture size is small
	binary.BigEndian.PutUint16(sub[6:], segCount*2)
	binary.BigEndian.PutUint16(sub[14:], code)     // endCode[0]
	binary.BigEndian.PutUint16(sub[16:], 0xFFFF)   // endCode[1]
	binary.BigEndian.PutUint16(sub[20:], code)     // startCode[0], after the reserved pad
	binary.BigEndian.PutUint16(sub[22:], 0xFFFF)   // startCode[1]
	binary.BigEndian.PutUint16(sub[24:], gid-code) // idDelta[0]
	binary.BigEndian.PutUint16(sub[26:], 1)        // idDelta[1]
	return sub
}

// symbolicTTFWithUnicodeCmap builds a three-glyph TrueType program with a (3,0)
// subtable sending 0xF005 to GID 1 (352 units wide) and a (3,1) subtable sending
// U+0020 to GID 2 (930 units wide), as pdfjs issue2618.pdf's program does.
func symbolicTTFWithUnicodeCmap() string {
	sym := cmapFormat4Single(0xF005, 1)
	uni := cmapFormat4Single(0x0020, 2)
	cmap := make([]byte, 4+2*8)
	binary.BigEndian.PutUint16(cmap[2:], 2)                    // numTables
	binary.BigEndian.PutUint16(cmap[4:], 3)                    // (3,0)
	binary.BigEndian.PutUint16(cmap[6:], 0)                    //
	binary.BigEndian.PutUint32(cmap[8:], 20)                   // offset of the (3,0) subtable
	binary.BigEndian.PutUint16(cmap[12:], 3)                   // (3,1)
	binary.BigEndian.PutUint16(cmap[14:], 1)                   //
	binary.BigEndian.PutUint32(cmap[16:], uint32(20+len(sym))) //nolint:gosec // fixture size is small
	cmap = append(append(cmap, sym...), uni...)

	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[0:], 0x00010000)
	binary.BigEndian.PutUint32(head[12:], 0x5F0F3CF5) // magicNumber
	binary.BigEndian.PutUint16(head[18:], 1000)       // unitsPerEm

	const glyphs = 3
	maxp := make([]byte, 32)
	binary.BigEndian.PutUint32(maxp[0:], 0x00010000)
	binary.BigEndian.PutUint16(maxp[4:], glyphs)

	hhea := make([]byte, 36)
	binary.BigEndian.PutUint32(hhea[0:], 0x00010000)
	binary.BigEndian.PutUint16(hhea[34:], glyphs)

	hmtx := make([]byte, 4*glyphs)
	binary.BigEndian.PutUint16(hmtx[0:], 500)
	binary.BigEndian.PutUint16(hmtx[4:], 352)
	binary.BigEndian.PutUint16(hmtx[8:], 930)

	// The directory is sorted by tag, as the sfnt format requires.
	tables := []struct {
		tag  string
		data []byte
	}{{"cmap", cmap}, {"head", head}, {"hhea", hhea}, {"hmtx", hmtx}, {"maxp", maxp}}

	var dir, body bytes.Buffer
	_ = binary.Write(&dir, binary.BigEndian, uint32(0x00010000))
	_ = binary.Write(&dir, binary.BigEndian, uint16(len(tables))) //nolint:gosec // fixed small count
	_ = binary.Write(&dir, binary.BigEndian, [3]uint16{})         // searchRange, entrySelector, rangeShift
	offset := 12 + 16*len(tables)
	for _, tb := range tables {
		dir.WriteString(tb.tag)
		_ = binary.Write(&dir, binary.BigEndian, uint32(0))                 // checksum
		_ = binary.Write(&dir, binary.BigEndian, uint32(offset+body.Len())) //nolint:gosec // fixture offsets are small
		_ = binary.Write(&dir, binary.BigEndian, uint32(len(tb.data)))      //nolint:gosec // fixture sizes are small
		body.Write(tb.data)
	}
	dir.Write(body.Bytes())
	return dir.String()
}

// TestSymbolicTrueTypeIgnoresEncoding pins that a symbolic TrueType font draws
// its codes through its symbol cmap even when it carries an /Encoding (ISO
// 19005-2, 6.2.11.5 compares widths against the glyph actually drawn).
//
// ISO 32000-1, 9.6.6.4 takes that route "when the font has no Encoding entry, or
// the font descriptor's Symbolic flag is set (in which case the Encoding entry
// is ignored)". pdfjs issue2618.pdf's symbolic font /R641 names code 5 "space"
// in its /Differences; litho followed the name through the program's Unicode
// cmap to a 930-unit glyph and reported a width mismatch, while the (3,0)
// subtable draws a 352-unit glyph, matching /Widths, and veraPDF passes.
func TestSymbolicTrueTypeIgnoresEncoding(t *testing.T) {
	t.Parallel()

	pdf := func(width int) []byte {
		program := symbolicTTFWithUnicodeCmap()
		content := "BT /F1 12 Tf 10 50 Td <05> Tj ET"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			fmt.Sprintf("<< /Type /Font /Subtype /TrueType /BaseFont /Test /FirstChar 5 /LastChar 5 "+
				"/Widths [%d] /Encoding << /BaseEncoding /WinAnsiEncoding /Differences [5 /space] >> "+
				"/FontDescriptor 6 0 R >>", width),
			"<< /Type /FontDescriptor /FontName /Test /Flags 4 /ItalicAngle 0 "+
				"/Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] "+
				"/FontFile2 7 0 R >>",
			fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream",
				len(program), len(program), program),
		)
	}

	fires := func(t *testing.T, data []byte) bool {
		t.Helper()
		doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == "6.2.11.5" {
				return true
			}
		}
		return false
	}

	t.Run("width of the glyph the symbol cmap draws passes", func(t *testing.T) {
		t.Parallel()
		if fires(t, pdf(352)) {
			t.Error("/Widths 352 matches the glyph 0xF005 draws")
		}
	})

	t.Run("width of the glyph the ignored encoding names fails", func(t *testing.T) {
		t.Parallel()
		if !fires(t, pdf(930)) {
			t.Error("/Widths 930 belongs to the glyph the ignored /Encoding names, not the one drawn")
		}
	})
}
