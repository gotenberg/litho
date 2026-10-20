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

// singleSubtableTTF builds a two-glyph TrueType program whose one cmap
// subtable, (platformID, encodingID), sends code 0x41 to GID 1.
func singleSubtableTTF(platformID, encodingID uint16) string {
	sub := cmapFormat4Single(0x0041, 1)
	cmap := make([]byte, 4+8)
	binary.BigEndian.PutUint16(cmap[2:], 1)
	binary.BigEndian.PutUint16(cmap[4:], platformID)
	binary.BigEndian.PutUint16(cmap[6:], encodingID)
	binary.BigEndian.PutUint32(cmap[8:], 12)
	cmap = append(cmap, sub...)

	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[0:], 0x00010000)
	binary.BigEndian.PutUint32(head[12:], 0x5F0F3CF5)
	binary.BigEndian.PutUint16(head[18:], 1000)

	const glyphs = 2
	maxp := make([]byte, 32)
	binary.BigEndian.PutUint32(maxp[0:], 0x00010000)
	binary.BigEndian.PutUint16(maxp[4:], glyphs)

	hhea := make([]byte, 36)
	binary.BigEndian.PutUint32(hhea[0:], 0x00010000)
	binary.BigEndian.PutUint16(hhea[34:], glyphs)

	hmtx := make([]byte, 4*glyphs)
	binary.BigEndian.PutUint16(hmtx[0:], 500)
	binary.BigEndian.PutUint16(hmtx[4:], 600)

	tables := []struct {
		tag  string
		data []byte
	}{{"cmap", cmap}, {"head", head}, {"hhea", hhea}, {"hmtx", hmtx}, {"maxp", maxp}}

	var dir, body bytes.Buffer
	_ = binary.Write(&dir, binary.BigEndian, uint32(0x00010000))
	_ = binary.Write(&dir, binary.BigEndian, uint16(len(tables))) //nolint:gosec // fixed small count
	_ = binary.Write(&dir, binary.BigEndian, [3]uint16{})
	offset := 12 + 16*len(tables)
	for _, tb := range tables {
		dir.WriteString(tb.tag)
		_ = binary.Write(&dir, binary.BigEndian, uint32(0))
		_ = binary.Write(&dir, binary.BigEndian, uint32(offset+body.Len())) //nolint:gosec // fixture offsets are small
		_ = binary.Write(&dir, binary.BigEndian, uint32(len(tb.data)))      //nolint:gosec // fixture sizes are small
		body.Write(tb.data)
	}
	dir.Write(body.Bytes())
	return dir.String()
}

// TestTrueTypeSymbolRouteGlyphPresence pins glyph presence for an embedded
// TrueType font that ISO 32000-1, 9.6.6.4 reads through its symbol subtables:
// one with no /Encoding, or one whose Symbolic flag is set. Only a (3,0) or a
// (1,0) subtable reaches a glyph, so a program carrying a (3,1) alone selects
// none, as veraPDF 1.30.2 measures on litho ttnoenc_2b.pdf.
func TestTrueTypeSymbolRouteGlyphPresence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flags    int
		encoding string
		platform uint16
		encID    uint16
		fires    bool
	}{
		{"non-symbolic, no /Encoding, (3,1) only", 32, "", 3, 1, true},
		{"symbolic, no /Encoding, (3,1) only", 4, "", 3, 1, true},
		{"symbolic with /Encoding, (3,1) only", 4, "/Encoding /WinAnsiEncoding ", 3, 1, true},
		{"non-symbolic, no /Encoding, (1,0)", 32, "", 1, 0, false},
		{"non-symbolic, no /Encoding, (3,0)", 32, "", 3, 0, false},
		{"symbolic, no /Encoding, (3,0)", 4, "", 3, 0, false},
		{"non-symbolic with /WinAnsiEncoding, (3,1) only", 32, "/Encoding /WinAnsiEncoding ", 3, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			program := singleSubtableTTF(tt.platform, tt.encID)
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /TrueType /BaseFont /Test /FirstChar 65 /LastChar 65 /Widths [600] "+tt.encoding+
					"/FontDescriptor 3 0 R >>",
				fmt.Sprintf("<< /Type /FontDescriptor /FontName /Test /Flags %d /ItalicAngle 0 /Ascent 700 /Descent -200 "+
					"/CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] /FontFile2 4 0 R >>", tt.flags),
				fmt.Sprintf("<< /Length %d /Length1 %d >>\nstream\n%s\nendstream", len(program), len(program), program),
			)
			fontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve font: %v", err)
			}
			usage := &fontUsageInfo{visible: true, charCodes: map[byte]bool{0x41: true}}
			got := checkSimpleGlyphPresence(ctx, doc, fontDict, "F1", 1, "PDF/A-2b", usage)
			if fired := len(got) != 0; fired != tt.fires {
				t.Errorf("fired = %v (%v), want %v", fired, got, tt.fires)
			}
			// The width the check weighs keeps its any-subtable reading.
			widths := checkGlyphWidths(ctx, doc, fontDict, "F1", 1, "PDF/A-2b", usage)
			if len(widths) != 0 {
				t.Errorf("width check fired: %v", widths)
			}
		})
	}
}
