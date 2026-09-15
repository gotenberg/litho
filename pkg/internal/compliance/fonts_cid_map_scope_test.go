package compliance

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// withNumberOfHMetrics returns a copy of an sfnt program whose hhea
// numberOfHMetrics reads n.
func withNumberOfHMetrics(t *testing.T, data []byte, n uint16) []byte {
	t.Helper()
	out := append([]byte(nil), data...)
	records, err := truetype.ParseTableDirectory(out)
	if err != nil {
		t.Fatalf("parse table directory: %v", err)
	}
	for _, r := range records {
		if r.Tag == "hhea" {
			binary.BigEndian.PutUint16(out[r.Offset+34:], n)
			return out
		}
	}
	t.Fatal("the program has no hhea table")
	return nil
}

// TestCIDSetCompletenessAbsentMap pins ISO 19005-2, 6.2.11.4.2 t2 on a
// CIDFontType2 subset with no /CIDToGIDMap: the map is then Identity (ISO
// 32000-1, 9.7.4.2, Table 117), so the /CIDSet shall list the glyphs of the
// program, as it shall under /CIDToGIDMap /Identity. veraPDF reads the absent
// entry that way on pdfjs issue5874.pdf. It requires the glyphs below
// hhea.numberOfHMetrics only, as on pdfjs issue5202.pdf, and fails a bit for a
// CID the program does not hold. Under a /CIDToGIDMap stream every CID the map
// covers whose glyph exists counts, GID 0 included, and a trailing odd byte is
// one more entry, as on pdfjs issue11768_reduced.pdf. Every case was measured
// against veraPDF.
func TestCIDSetCompletenessAbsentMap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf") // 38 glyphs, numberOfHMetrics 38
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	full := []byte{0x7F, 0xFF, 0xFF, 0xFF, 0xFC}          // CIDs 1 to 37
	missing := []byte{0x3F, 0xFF, 0xFF, 0xFF, 0xFC}       // CID 1 left out
	pastEnd := []byte{0x7F, 0xFF, 0xFF, 0xFF, 0xFC, 0x80} // CIDs 1 to 37 and 40

	const stream = " /CIDToGIDMap 6 0 R"
	cid1 := []byte{0x40}   // CID 1
	cids12 := []byte{0x60} // CIDs 1 and 2
	tests := []struct {
		name     string
		mapEntry string
		mapData  string // the /CIDToGIDMap stream's bytes
		cidSet   []byte
		metrics  uint16 // hhea numberOfHMetrics to write, 0 to keep the program's
		fires    bool
	}{
		{"no map, every glyph listed", "", "", full, 0, false},
		{"no map, a glyph left out", "", "", missing, 0, true},
		{"no map, a glyph past numberOfHMetrics left out", "", "", missing, 1, false},
		{"no map, a bit past the last glyph", "", "", pastEnd, 0, true},
		{"an Identity map, a glyph left out", " /CIDToGIDMap /Identity", "", missing, 0, true},
		{"a stream map, every mapped CID listed", stream, "\x00\x00\x00\x01", cid1, 0, false},
		{"a stream map, a mapped CID left out", stream, "\x00\x00\x00\x01", missing, 0, true},
		{"a stream map, a CID mapped to GID 0 left out", stream, "\x00\x00\x00\x01\x00\x00", cid1, 0, true},
		{"a stream map, a bit past the map", stream, "\x00\x00\x00\x01", cids12, 0, true},
		{"a stream map with an odd byte, its CID left out", stream, "\x00\x00\x00\x01\x00", cid1, 0, true},
		{"a stream map with an odd byte, its CID listed", stream, "\x00\x00\x00\x01\x00", cids12, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			program := ttf
			if tt.metrics != 0 {
				program = withNumberOfHMetrics(t, ttf, tt.metrics)
			}
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog >>",
				"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /ABCDEF+Test"+
					" /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 3 0 R"+tt.mapEntry+" >>",
				"<< /Type /FontDescriptor /FontName /ABCDEF+Test /Flags 4 /ItalicAngle 0 /Ascent 700 /Descent -200"+
					" /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] /FontFile2 4 0 R /CIDSet 5 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), program),
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.cidSet), tt.cidSet),
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.mapData), tt.mapData),
			)
			cidFontDict, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve CIDFont: %v", err)
			}
			fd, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 3})
			if err != nil {
				t.Fatalf("resolve font descriptor: %v", err)
			}
			got := checkCIDSetCompletenessTrueType(ctx, doc, cidFontDict, fd, fd["CIDSet"], "F1", 1, Rule_19005_2_6_2_11_4_2_t02)
			if fired := len(got) != 0; fired != tt.fires {
				t.Errorf("fired = %v (%v), want %v", fired, got, tt.fires)
			}
		})
	}
}

// TestCIDFontType0IgnoresCIDToGIDMap pins ISO 19005-2, 6.2.11.5 t1 on a
// CIDFontType0 that carries a /CIDToGIDMap: ISO 32000-1, 9.7.4.2, Table 117
// gives the entry to Type 2 CIDFonts only, so the glyph whose width /W states is
// the CID's own. veraPDF ignores the map the same way on pdfjs issue7901.pdf. A
// CIDFontType2 still follows it.
func TestCIDFontType0IgnoresCIDToGIDMap(t *testing.T) {
	t.Parallel()
	ctx := testCtx()

	binaryWidths := map[uint16]float64{0: 1000, 2: 700, 15: 600}
	gidMap := make([]byte, 32)
	gidMap[31] = 0x02 // CID 15 → GID 2
	mapStream := ast.NewStream(ast.PdfDict{}, gidMap)
	wArr := ast.PdfArray{ast.PdfInt(15), ast.PdfArray{ast.PdfReal(700.0)}} // GID 2's width

	for _, tt := range []struct {
		subtype string
		fires   bool
	}{
		{"CIDFontType0", true},
		{"CIDFontType2", false},
	} {
		t.Run(tt.subtype, func(t *testing.T) {
			t.Parallel()
			dict := ast.PdfDict{"Subtype": ast.PdfName(tt.subtype), "W": wArr, "CIDToGIDMap": mapStream}
			got := checkCIDFontWidths(ctx, nopDoc(), dict, binaryWidths, 1000, "TestCID", 1, Rule_19005_2_6_2_11_5_t01, nil)
			if fired := len(got) != 0; fired != tt.fires {
				t.Errorf("fired = %v (%v), want %v", fired, got, tt.fires)
			}
		})
	}
}
