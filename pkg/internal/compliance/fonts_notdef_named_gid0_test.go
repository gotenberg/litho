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

// ttfNamingGID0 builds a two-glyph TrueType program whose format 2 post table
// names GID 0 "space" and GID 1 "percent" (standard Macintosh names 3 and 8),
// so the program carries no glyph called .notdef at all.
func ttfNamingGID0() string {
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
	binary.BigEndian.PutUint16(hmtx[0:], 651)
	binary.BigEndian.PutUint16(hmtx[4:], 800)

	post := make([]byte, 32+2+2*glyphs)
	binary.BigEndian.PutUint32(post[0:], 0x00020000) // version 2.0
	binary.BigEndian.PutUint16(post[32:], glyphs)
	binary.BigEndian.PutUint16(post[34:], 3) // GID 0: standard name "space"
	binary.BigEndian.PutUint16(post[36:], 8) // GID 1: standard name "percent"

	// The directory is sorted by tag, as the sfnt format requires.
	tables := []struct {
		tag  string
		data []byte
	}{{"head", head}, {"hhea", hhea}, {"hmtx", hmtx}, {"maxp", maxp}, {"post", post}}

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

// TestNotdefGlyph_NamedGlyphAtGID0 pins that a code whose encoding names a
// real glyph is not a .notdef reference (ISO 19005-2, 6.2.11.8 t1), even when
// the program stores that glyph at GID 0.
//
// veraPDF names a simple font's glyph from its encoding (GFGlyph: the /Encoding
// name for the code, the program only when the encoding gives none) and tests
// name != ".notdef". The control keeps a code the encoding names ".notdef"
// failing the rule.
//
// # Corpus evidence
//
//   - pdfjs issue13107_reduced.pdf: embeds a DejaVuSans subset with no
//     .notdef glyph at all. Its post table names GID 0 "space", and the page
//     shows code 1, which /Differences names "space". litho reported the GID
//     0 as .notdef at PDF/A-2b, PDF/A-4 and PDF/UA-2 while veraPDF passed all
//     three.
func TestNotdefGlyph_NamedGlyphAtGID0(t *testing.T) {
	t.Parallel()

	pdf := func(name string) []byte {
		program := ttfNamingGID0()
		content := "BT /F1 12 Tf 10 50 Td <01> Tj ET"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /Type /Font /Subtype /TrueType /BaseFont /AAAAAA+DejaVuSans /FirstChar 1 /LastChar 2 "+
				"/Widths [651 800] /Encoding << /Type /Encoding /Differences [1 "+name+" /percent] >> "+
				"/FontDescriptor 6 0 R >>",
			"<< /Type /FontDescriptor /FontName /AAAAAA+DejaVuSans /Flags 32 /ItalicAngle 0 "+
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
			if v, ok := e.(Violation); ok && v.Rule.Clause == "6.2.11.8" && v.Rule.TestNumber == 1 {
				return true
			}
		}
		return false
	}

	t.Run("a named glyph stored at GID 0 is not .notdef", func(t *testing.T) {
		t.Parallel()
		if fires(t, pdf("/space")) {
			t.Error("code 1 names the glyph space, which the program stores at GID 0")
		}
	})

	t.Run("a code named .notdef still fires", func(t *testing.T) {
		t.Parallel()
		if !fires(t, pdf("/.notdef")) {
			t.Error("code 1 names .notdef")
		}
	})
}
