package compliance

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// nonSymbolicTTF builds a two-glyph TrueType program whose (3,1) subtable
// sends U+0041 to GID 1.
func nonSymbolicTTF() string {
	uni := cmapFormat4Single(0x0041, 1)
	cmap := make([]byte, 4+8)
	binary.BigEndian.PutUint16(cmap[2:], 1)  // numTables
	binary.BigEndian.PutUint16(cmap[4:], 3)  // (3,1)
	binary.BigEndian.PutUint16(cmap[6:], 1)  //
	binary.BigEndian.PutUint32(cmap[8:], 12) // offset of the subtable
	cmap = append(cmap, uni...)

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
	binary.BigEndian.PutUint16(hmtx[4:], 722)

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

// TestTrueTypeUnnamedCodeGlyphPresence pins that a non-symbolic TrueType font
// draws no glyph for a code its predefined encoding leaves unnamed (ISO 14289-2
// §8.4.5.5.1 t2).
//
// ISO 32000-1, 9.6.6.4 reaches a non-symbolic TrueType glyph through the name
// the encoding gives the code, and WinAnsiEncoding names nothing below 32.
// veraPDF (TrueTypeFontProgram.containsCode) reads such a code as .notdef and
// reports the glyph absent: govdocs thread_300_300708.pdf shows code 31 through
// an embedded TimesNewRomanPSMT. A Differences-only dictionary stays unjudged,
// since litho takes an implicit base encoding there and veraPDF takes none.
func TestTrueTypeUnnamedCodeGlyphPresence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		encoding string
		code     byte
		fires    bool
	}{
		{"a code WinAnsiEncoding leaves unnamed", "/WinAnsiEncoding", 31, true},
		{"a code WinAnsiEncoding names", "/WinAnsiEncoding", 65, false},
		{"an unnamed code over a base encoding", "<< /BaseEncoding /WinAnsiEncoding /Differences [66 /A] >>", 31, true},
		{"a Differences-only dictionary", "<< /Differences [65 /A] >>", 31, false},
		{"code 0, which the .notdef rule judges", "/WinAnsiEncoding", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			program := nonSymbolicTTF()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /TrueType /BaseFont /Test /FirstChar 0 /LastChar 255 /Encoding "+tt.encoding+
					" /FontDescriptor 3 0 R >>",
				"<< /Type /FontDescriptor /FontName /Test /Flags 32 /ItalicAngle 0 /Ascent 700 /Descent -200 "+
					"/CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] /FontFile2 4 0 R >>",
				fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(program), len(program), program),
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{tt.code: true}}
			got := checkSimpleGlyphPresence(ctx, doc, fontDict, "F1", 1, "PDF/UA-2", usage)
			if fired := len(got) != 0; fired != tt.fires {
				t.Errorf("fired = %v (%v), want %v", fired, got, tt.fires)
			}
		})
	}
}
