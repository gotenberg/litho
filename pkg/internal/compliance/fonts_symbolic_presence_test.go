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

// symbolicTTF builds a two-glyph TrueType program whose only cmap subtable is
// a (3,0) Microsoft Symbol one mapping code to GID 1, with the head, hhea, hmtx
// and maxp tables the glyph checks read.
func symbolicTTF(code uint16) string {
	// Format 4: one segment for code, then the 0xFFFF terminator segment.
	const segCount = 2
	sub := make([]byte, 14+segCount*8+2)
	binary.BigEndian.PutUint16(sub[0:], 4)
	binary.BigEndian.PutUint16(sub[2:], uint16(len(sub))) //nolint:gosec // fixture size is small
	binary.BigEndian.PutUint16(sub[6:], segCount*2)
	binary.BigEndian.PutUint16(sub[14:], code)   // endCode[0]
	binary.BigEndian.PutUint16(sub[16:], 0xFFFF) // endCode[1]
	binary.BigEndian.PutUint16(sub[20:], code)   // startCode[0], after the reserved pad
	binary.BigEndian.PutUint16(sub[22:], 0xFFFF) // startCode[1]
	binary.BigEndian.PutUint16(sub[24:], 1-code) // idDelta[0]: code maps to GID 1
	binary.BigEndian.PutUint16(sub[26:], 1)      // idDelta[1]

	cmap := make([]byte, 12)
	binary.BigEndian.PutUint16(cmap[2:], 1)  // numTables
	binary.BigEndian.PutUint16(cmap[4:], 3)  // platformID 3
	binary.BigEndian.PutUint16(cmap[6:], 0)  // encodingID 0, Symbol
	binary.BigEndian.PutUint32(cmap[8:], 12) // subtable offset
	cmap = append(cmap, sub...)

	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[0:], 0x00010000)
	binary.BigEndian.PutUint32(head[12:], 0x5F0F3CF5) // magicNumber
	binary.BigEndian.PutUint16(head[18:], 1000)       // unitsPerEm

	const glyphs = 2
	maxp := make([]byte, 32)
	binary.BigEndian.PutUint32(maxp[0:], 0x00010000)
	binary.BigEndian.PutUint16(maxp[4:], glyphs)

	hhea := make([]byte, 36)
	binary.BigEndian.PutUint32(hhea[0:], 0x00010000)
	binary.BigEndian.PutUint16(hhea[34:], glyphs)

	hmtx := make([]byte, 4*glyphs)
	binary.BigEndian.PutUint16(hmtx[0:], 500)
	binary.BigEndian.PutUint16(hmtx[4:], 500)

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

// TestSymbolicTrueTypeGlyphPresence pins that a code a symbolic TrueType font's
// symbol cmap subtable does not hold is reported as a glyph not present (ISO
// 19005-2, 6.2.11.4.1 t2).
//
// ISO 32000-1, 9.6.6.4 draws such a code through the (3,0) subtable, so a code
// it lacks selects no glyph. litho treated the code as unresolvable and skipped
// it. pdfjs issue8234.pdf shows code 32 through an Arial subset whose (3,0)
// subtable holds only 0xF021 to 0xF027; veraPDF reports it and fontTools
// confirms no space glyph.
func TestSymbolicTrueTypeGlyphPresence(t *testing.T) {
	t.Parallel()

	// pdf shows shown through a symbolic TrueType font with no /Encoding whose
	// (3,0) subtable maps cmapCode to its one real glyph.
	pdf := func(cmapCode uint16, shown byte) []byte {
		program := symbolicTTF(cmapCode)
		content := fmt.Sprintf("BT /F1 12 Tf 10 50 Td <%02X> Tj ET", shown)
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /Type /Font /Subtype /TrueType /BaseFont /Test /FirstChar 32 /LastChar 33 "+
				"/Widths [500 500] /FontDescriptor 6 0 R >>",
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
			if v, ok := e.(Violation); ok && v.Rule.Clause == "6.2.11.4.1" && v.Rule.TestNumber == 2 {
				return true
			}
		}
		return false
	}

	t.Run("a code the subtable lacks fires", func(t *testing.T) {
		t.Parallel()
		if !fires(t, pdf(0xF021, 0x20)) {
			t.Error("code 32 has no glyph in a (3,0) subtable holding only 0xF021")
		}
	})

	t.Run("a code the subtable holds does not", func(t *testing.T) {
		t.Parallel()
		if fires(t, pdf(0xF021, 0x21)) {
			t.Error("code 33 selects the glyph 0xF021 maps to")
		}
	})

	t.Run("a code held in the 0xF100 range does not", func(t *testing.T) {
		t.Parallel()
		if fires(t, pdf(0xF121, 0x21)) {
			t.Error("code 33 selects the glyph 0xF121 maps to")
		}
	})
}
