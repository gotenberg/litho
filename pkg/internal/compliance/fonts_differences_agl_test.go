package compliance

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/models"
)

// withCmapRecord returns a copy of an sfnt program whose cmap encoding record
// for the platform and encoding pair from reads to instead.
func withCmapRecord(t *testing.T, data []byte, from, to [2]uint16) []byte {
	t.Helper()
	out := append([]byte(nil), data...)
	records, err := truetype.ParseTableDirectory(out)
	if err != nil {
		t.Fatalf("parse table directory: %v", err)
	}
	for _, r := range records {
		if r.Tag != "cmap" {
			continue
		}
		base := int(r.Offset)
		for i := range int(binary.BigEndian.Uint16(out[base+2:])) {
			rec := out[base+4+8*i:]
			if binary.BigEndian.Uint16(rec) == from[0] && binary.BigEndian.Uint16(rec[2:]) == from[1] {
				binary.BigEndian.PutUint16(rec, to[0])
				binary.BigEndian.PutUint16(rec[2:], to[1])
			}
		}
		return out
	}
	t.Fatal("the program has no cmap table")
	return nil
}

// TestNonSymbolicTrueTypeDifferencesListed pins ISO 19005-2, 6.2.11.6 t2 and ISO
// 19005-4, 6.2.10.6 t2 on a non-symbolic TrueType font whose encoding carries
// /Differences: every name shall be listed in the Adobe Glyph List, which a
// uniXXXX spelling is not, and the embedded program shall carry a Microsoft
// Unicode (3,1) cmap subtable. A font with no embedded program is left to the
// embedding rule. Every case was measured against veraPDF.
func TestNonSymbolicTrueTypeDifferencesListed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	ttf, err := os.ReadFile("testdata/cidfont_notdef_min.ttf") // cmap (0,3) and (3,1)
	if err != nil {
		t.Fatalf("read TTF fixture: %v", err)
	}
	macOnly := withCmapRecord(t, ttf, [2]uint16{3, 1}, [2]uint16{1, 0})
	noCmap := stripCmapTable(t, ttf)

	tests := []struct {
		name    string
		glyph   string
		program []byte
		want    int
	}{
		{"a listed name", "/A", ttf, 0},
		{"a uniXXXX name", "/uni0041", ttf, 1},
		{"a listed legacy name", "/afii10051", ttf, 0},
		{"a .notdef entry", "/.notdef", ttf, 0},
		{"a program with no (3,1) subtable", "/A", macOnly, 1},
		{"a program with no cmap table", "/A", noCmap, 1},
		{"no embedded program", "/uni0041", nil, 0},
	}
	for _, tt := range tests {
		for _, p := range []struct {
			profile models.PDFAProfile
			rule    Rule
		}{{models.PDFA2b, Rule_19005_2_6_2_11_6_t02}, {models.PDFA4, Rule_19005_4_6_2_10_6_t02}} {
			t.Run(fmt.Sprintf("%s, %s", tt.name, p.profile), func(t *testing.T) {
				t.Parallel()
				const body = "BT /F1 12 Tf 10 50 Td (A) Tj ET"
				fontFile, program := "", "null"
				if tt.program != nil {
					fontFile = " /FontFile2 7 0 R"
					program = fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.program), tt.program)
				}
				doc, _ := objsDocPDF(t,
					"<< /Type /Catalog /Pages 2 0 R >>",
					"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
					"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
					fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
					"<< /Type /Font /Subtype /TrueType /BaseFont /Test /FirstChar 65 /LastChar 65 /Widths [722]"+
						" /Encoding << /BaseEncoding /WinAnsiEncoding /Differences [65 "+tt.glyph+"] >> /FontDescriptor 6 0 R >>",
					"<< /Type /FontDescriptor /FontName /Test /Flags 32 /Ascent 700 /Descent -200 /CapHeight 700 /StemV 80"+
						" /FontBBox [0 0 500 700] /ItalicAngle 0"+fontFile+" >>",
					program,
				)
				viols, err := ValidatePDFA(ctx, doc, p.profile)
				if err != nil {
					t.Fatalf("ValidatePDFA: %v", err)
				}
				if got := countRuleErrs(t, viols, p.rule); got != tt.want {
					t.Errorf("%s t%d fired %d times, want %d", p.rule.Clause, p.rule.TestNumber, got, tt.want)
				}
			})
		}
	}
}
