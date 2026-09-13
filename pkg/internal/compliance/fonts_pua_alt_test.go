package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestPUAActualText_AltCoversUnderUA2 pins that an /Alt covers a Private Use
// Area glyph under ISO 14289-2, 8.4.3 and not under ISO 19005-2, 6.2.11.7.3.
//
// veraPDF's 8.4.3 test reads "unicodePUA == false || actualTextPresent == true
// || altPresent == true", and altPresent looks up /Alt exactly where
// actualTextPresent looks up /ActualText: the enclosing marked-content
// sequences, then the structure element the glyph's MCID names
// (MarkedContentHelper.containsStringKey). The PDF/A-2 test has no Alt term.
// Measured against veraPDF: a /Span BDC carrying /Alt passes 8.4.3 and fails
// 6.2.11.7.3.
func TestPUAActualText_AltCoversUnderUA2(t *testing.T) {
	t.Parallel()

	const cmap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap\n" +
		"1 begincodespacerange <0000> <ffff> endcodespacerange\n" +
		"1 beginbfchar <0001> <E12D> endbfchar\n" +
		"endcmap CMapName currentdict /CMap defineresource pop end end"
	const glyph = "BT /F1 12 Tf 10 50 Td <0001> Tj ET"

	// doc shows the PUA glyph through body. The page's MCID 0 belongs to a
	// Span element that carries elemExtra.
	doc := func(body, elemExtra string) []byte {
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 7 0 R /MarkInfo << /Marked true >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /StructParents 0 "+
				"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
			"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H "+
				"/DescendantFonts [8 0 R] /ToUnicode 6 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cmap), cmap),
			"<< /Type /StructTreeRoot /K 10 0 R /ParentTree << /Nums [0 [10 0 R]] >> >>",
			"<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Test /DW 1000 "+
				"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor 9 0 R >>",
			"<< /Type /FontDescriptor /FontName /Test /Flags 4 /ItalicAngle 0 /Ascent 700 "+
				"/Descent -200 /CapHeight 700 /StemV 80 /FontBBox [0 0 1000 1000] >>",
			"<< /Type /StructElem /S /Span /P 7 0 R /Pg 3 0 R /K 0"+elemExtra+" >>",
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

	altBDC := "/Span << /Alt (xx) >> BDC " + glyph + " EMC"
	tagged := "/Span << /MCID 0 >> BDC " + glyph + " EMC"
	tests := []struct {
		name      string
		body      string
		elemExtra string
		ua        bool
		want      bool
	}{
		{"UA-2: no Alt or ActualText", tagged, "", true, true},
		{"UA-2: Alt on the marked content", altBDC, "", true, false},
		{"UA-2: Alt on the structure element", tagged, " /Alt (xx)", true, false},
		{"PDF/A-2a: Alt on the marked content does not cover", altBDC, "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fires(t, doc(tt.body, tt.elemExtra), tt.ua); got != tt.want {
				t.Errorf("fired = %v, want %v", got, tt.want)
			}
		})
	}
}
