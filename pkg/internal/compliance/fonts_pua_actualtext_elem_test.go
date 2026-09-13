package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestPUAActualText_MCIDElementOnly pins that a structure element covers the
// PUA glyphs of its marked content only through its own /ActualText.
//
// veraPDF's actualTextPresent looks the key up on the structure element the
// glyph's MCID names and nowhere above it (MarkedContentHelper.containsStringKey
// reads that one element), under ISO 14289-2, 8.4.3 and ISO 19005-2,
// 6.2.11.7.3 alike. litho climbed the element's /P chain and let a parent's
// /ActualText cover a child that has none. Measured against veraPDF: a P
// carrying /ActualText over a Span without it fails, the Span carrying it
// passes.
func TestPUAActualText_MCIDElementOnly(t *testing.T) {
	t.Parallel()

	const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <0000> <ffff> endcodespacerange\n" +
		"1 beginbfchar <0001> <E12D> endbfchar\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"
	const body = "/Span << /MCID 0 >> BDC BT /F1 12 Tf 10 50 Td <0001> Tj ET EMC"

	// doc tags the PUA glyph with MCID 0, owned by a Span (object 10) under a
	// P (object 11); spanExtra and pExtra are appended to each element.
	doc := func(spanExtra, pExtra string) []byte {
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 7 0 R /MarkInfo << /Marked true >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /StructParents 0 "+
				"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H "+
				"/DescendantFonts [8 0 R] /ToUnicode 6 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
			"<< /Type /StructTreeRoot /K 11 0 R /ParentTree << /Nums [0 [10 0 R]] >> >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /DW 1000 "+
				"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 9 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 4 /ItalicAngle 0 /Ascent 700 "+
				"/Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
			"<< /Type /StructElem /S /Span /P 11 0 R /Pg 3 0 R /K 0"+spanExtra+" >>",
			"<< /Type /StructElem /S /P /P 7 0 R /K [10 0 R]"+pExtra+" >>",
		)
	}

	fires := func(t *testing.T, data []byte, ua bool) bool {
		t.Helper()
		d, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		var viols []error
		clause := "8.4.3"
		if ua {
			viols, err = ValidatePDFUA(context.Background(), d, models.PDFUA2)
		} else {
			clause = "6.2.11.7.3"
			viols, err = ValidatePDFA(context.Background(), d, models.PDFA2a)
		}
		if err != nil {
			t.Fatalf("validate: %v", err)
		}
		for _, e := range viols {
			if v, ok := e.(Violation); ok && v.Rule.Clause == clause {
				return true
			}
		}
		return false
	}

	tests := []struct {
		name      string
		spanExtra string
		pExtra    string
		want      bool
	}{
		{"ActualText on the parent only", "", " /ActualText (x)", true},
		{"ActualText on the MCID's own element", " /ActualText (x)", "", false},
		{"ActualText nowhere", "", "", true},
	}
	for _, tt := range tests {
		for _, ua := range []bool{true, false} {
			name := tt.name + " (PDF/A-2a)"
			if ua {
				name = tt.name + " (PDF/UA-2)"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				if got := fires(t, doc(tt.spanExtra, tt.pExtra), ua); got != tt.want {
					t.Errorf("fired = %v, want %v", got, tt.want)
				}
			})
		}
	}
}
