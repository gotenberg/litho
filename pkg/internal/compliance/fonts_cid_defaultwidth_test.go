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

// ttfWithAdvances builds a minimal TrueType program carrying just the tables a
// width check reads: head for unitsPerEm, maxp for the glyph count, hhea for the
// metric count and hmtx for the advances themselves.
func ttfWithAdvances(unitsPerEm uint16, advances []uint16) string {
	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[0:], 0x00010000)
	binary.BigEndian.PutUint32(head[12:], 0x5F0F3CF5) // magicNumber
	binary.BigEndian.PutUint16(head[18:], unitsPerEm)

	maxp := make([]byte, 32)
	binary.BigEndian.PutUint32(maxp[0:], 0x00010000)
	binary.BigEndian.PutUint16(maxp[4:], uint16(len(advances))) //nolint:gosec // fixture sizes are small

	hhea := make([]byte, 36)
	binary.BigEndian.PutUint32(hhea[0:], 0x00010000)
	binary.BigEndian.PutUint16(hhea[34:], uint16(len(advances))) //nolint:gosec // fixture sizes are small

	hmtx := make([]byte, 4*len(advances))
	for i, adv := range advances {
		binary.BigEndian.PutUint16(hmtx[4*i:], adv)
	}

	type table struct {
		tag  string
		data []byte
	}
	// The directory is sorted by tag, as the sfnt format requires.
	tables := []table{{"head", head}, {"hhea", hhea}, {"hmtx", hmtx}, {"maxp", maxp}}

	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, uint32(0x00010000))
	_ = binary.Write(&buf, binary.BigEndian, uint16(len(tables))) //nolint:gosec // fixed small count
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))           // searchRange
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))           // entrySelector
	_ = binary.Write(&buf, binary.BigEndian, uint16(0))           // rangeShift

	offset := 12 + 16*len(tables)
	var body bytes.Buffer
	for _, tb := range tables {
		buf.WriteString(tb.tag)
		_ = binary.Write(&buf, binary.BigEndian, uint32(0))                 // checksum
		_ = binary.Write(&buf, binary.BigEndian, uint32(offset+body.Len())) //nolint:gosec // fixture offsets are small
		_ = binary.Write(&buf, binary.BigEndian, uint32(len(tb.data)))      //nolint:gosec // fixture sizes are small
		body.Write(tb.data)
	}
	buf.Write(body.Bytes())
	return buf.String()
}

// TestCIDFontDefaultWidth pins that /DW is checked against the embedded
// program for every rendered CID the /W array leaves out.
//
// ISO 32000-2, 9.7.4.3 makes /DW the width of every CID /W does not cover, and
// gives it the default 1000. litho skipped the whole font when /W was absent,
// so such a CIDFont was never width-checked. pdfjs issue6612.pdf embeds a
// CIDFontType2 with /DW 595 and no /W whose glyphs advance 1229 at 2048 units
// per em, which is 600.098; veraPDF reports every shown glyph and fontTools
// confirms the advance.
func TestCIDFontDefaultWidth(t *testing.T) {
	t.Parallel()

	// pdf shows CID 1 through a Type0 font whose descendant declares dw (empty
	// for none) and embeds a program giving CID 1 the given advance.
	pdf := func(dw string, advance uint16) []byte {
		program := ttfWithAdvances(1000, []uint16{500, advance})
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			"<< /Length 44 >>\nstream\nBT /F1 12 Tf 10 50 Td <0001> Tj ET\nendstream",
			"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H "+
				"/DescendantFonts [6 0 R] >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test "+
				"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> "+
				"/CIDToGIDMap /Identity "+dw+" /FontDescriptor 7 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 4 /ItalicAngle 0 "+
				"/Ascent 700 /Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] "+
				"/FontFile2 8 0 R >>",
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

	t.Run("a declared DW contradicting the program fires", func(t *testing.T) {
		t.Parallel()
		if !fires(t, pdf("/DW 595", 600)) {
			t.Error("/DW 595 against a 600 advance is a width mismatch")
		}
	})

	t.Run("a matching DW does not", func(t *testing.T) {
		t.Parallel()
		if fires(t, pdf("/DW 600", 600)) {
			t.Error("/DW 600 matches the program advance")
		}
	})

	t.Run("an absent DW takes the 1000 default", func(t *testing.T) {
		t.Parallel()
		if !fires(t, pdf("", 600)) {
			t.Error("with no /DW the width is 1000, which a 600 advance contradicts")
		}
	})

	t.Run("an absent DW matching 1000 does not", func(t *testing.T) {
		t.Parallel()
		if fires(t, pdf("", 1000)) {
			t.Error("the 1000 default matches a 1000 advance")
		}
	})

	// A zero advance is missing information rather than a zero-wide glyph: the
	// CID may reach past the program's glyph count or land on a blank the
	// program gave no metrics for. veraPDF reports none of these. pdfjs
	// JBIG2Globals.pdf maps CID 40 into a two-glyph program and
	// complex_ttf_font.pdf CID 988 into a 143-glyph one.
	t.Run("a zero advance is not a mismatch", func(t *testing.T) {
		t.Parallel()
		if fires(t, pdf("/DW 500", 0)) {
			t.Error("a zero advance carries no width to contradict /DW")
		}
	})
}
